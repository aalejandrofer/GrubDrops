package twitch

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type pointsRequest struct {
	OperationName string         `json:"operationName"`
	Variables     map[string]any `json:"variables"`
	Extensions    struct {
		PersistedQuery struct {
			Hash string `json:"sha256Hash"`
		} `json:"persistedQuery"`
	} `json:"extensions"`
}

func pointsServer(t *testing.T, bonus bool, requests *[]pointsRequest) (*client, *int) {
	t.Helper()
	claims := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		var req pointsRequest
		require.NoError(t, json.Unmarshal(body, &req))
		if requests != nil {
			*requests = append(*requests, req)
		}
		w.Header().Set("Content-Type", "application/json")
		switch req.OperationName {
		case "ChannelPointsContext":
			claim := `null`
			if bonus {
				claim = `{"id":"bonus-1"}`
			}
			_, _ = io.WriteString(w, `{"data":{"community":{"channel":{"id":"channel-id-1","self":{"communityPoints":{"balance":123,"availableClaim":`+claim+`}}}}}}`)
		case "ClaimCommunityPoints":
			claims++
			_, _ = io.WriteString(w, `{"data":{"claimCommunityPoints":{}}}`)
		case "JoinRaid":
			_, _ = io.WriteString(w, `{"data":{"joinRaid":{}}}`)
		default:
			_, _ = io.WriteString(w, `{"data":{}}`)
		}
	}))
	t.Cleanup(srv.Close)
	return newTestClient(srv.URL), &claims
}

func TestChannelPointsBalanceIsReadOnly(t *testing.T) {
	var requests []pointsRequest
	c, claims := pointsServer(t, true, &requests)
	balance, err := channelPointsBalance(context.Background(), c, platform.Session{AccessToken: "token"}, "channel-login")
	require.NoError(t, err)
	assert.Equal(t, 123, balance)
	assert.Zero(t, *claims, "balance reads must never claim bonuses")
	require.Len(t, requests, 1)
	assert.Equal(t, "ChannelPointsContext", requests[0].OperationName)
	assert.Equal(t, OpChannelPointsContext.Hash, requests[0].Extensions.PersistedQuery.Hash)
	assert.Equal(t, "channel-login", requests[0].Variables["channelLogin"])
}

func TestWatchClaimsOnlyAvailableBonusOnStartAndHeartbeat(t *testing.T) {
	for _, tc := range []struct {
		name  string
		bonus bool
		want  int
	}{{"absent", false, 0}, {"present", true, 2}} {
		t.Run(tc.name, func(t *testing.T) {
			var requests []pointsRequest
			c, claims := pointsServer(t, tc.bonus, &requests)
			wt := &watch{c: c, cachedUserID: 123, spadeURLs: map[string]string{}, playlistURLs: map[string]playlistEntry{}}
			h, err := wt.start(context.Background(), platform.Session{AccessToken: "token"}, platform.Stream{Channel: "source", ChannelID: "42"})
			require.NoError(t, err)
			assert.Equal(t, map[bool]int{false: 0, true: 1}[tc.bonus], *claims)
			_ = wt.heartbeat(context.Background(), h) // Playback itself is intentionally unavailable in this fixture.
			assert.Equal(t, tc.want, *claims)
			wantNames := []string{"ChannelPointsContext", "ChannelPointsContext"}
			if tc.bonus {
				wantNames = []string{"ChannelPointsContext", "ClaimCommunityPoints", "ChannelPointsContext", "ClaimCommunityPoints"}
			}
			var pointRequests []pointsRequest
			for _, req := range requests {
				if req.OperationName == "ChannelPointsContext" || req.OperationName == "ClaimCommunityPoints" {
					pointRequests = append(pointRequests, req)
				}
			}
			require.Len(t, pointRequests, len(wantNames))
			for i, name := range wantNames {
				assert.Equal(t, name, pointRequests[i].OperationName)
				wantHash := OpChannelPointsContext.Hash
				if name == "ClaimCommunityPoints" {
					wantHash = OpClaimCommunityPoints.Hash
				}
				assert.Equal(t, wantHash, pointRequests[i].Extensions.PersistedQuery.Hash)
			}
			if tc.bonus {
				input, ok := pointRequests[1].Variables["input"].(map[string]any)
				require.True(t, ok)
				assert.Equal(t, "bonus-1", input["claimID"])
				assert.Equal(t, "channel-id-1", input["channelID"])
			}
		})
	}
}

func TestJoinRaidUsesPersistedOperationAndRaidID(t *testing.T) {
	var requests []pointsRequest
	c, _ := pointsServer(t, false, &requests)
	require.NoError(t, joinRaid(context.Background(), c, platform.Session{AccessToken: "token"}, "raid-9"))
	require.Len(t, requests, 1)
	assert.Equal(t, "JoinRaid", requests[0].OperationName)
	assert.Equal(t, OpJoinRaid.Hash, requests[0].Extensions.PersistedQuery.Hash)
	input, ok := requests[0].Variables["input"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "raid-9", input["raidID"])
}
