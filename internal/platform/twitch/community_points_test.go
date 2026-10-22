package twitch

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWatchClaimsAvailableChannelPointsOnStartAndHeartbeat(t *testing.T) {
	var srv *httptest.Server
	contextQueries, claims := 0, 0
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/goldenstreamer" {
			_, _ = fmt.Fprintf(w, `"spade_url": %q`, srv.URL+"/spade")
			return
		}
		if r.Method == http.MethodPost && r.URL.Path != "/spade" {
			var request struct {
				OperationName string         `json:"operationName"`
				Variables     map[string]any `json:"variables"`
			}
			require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
			w.Header().Set("Content-Type", "application/json")
			switch request.OperationName {
			case "ChannelPointsContext":
				contextQueries++
				assert.Equal(t, "goldenstreamer", request.Variables["channelLogin"])
				_, _ = fmt.Fprint(w, `{"data":{"community":{"channel":{"id":"12345","self":{"communityPoints":{"availableClaim":{"id":"bonus-1"}}}}}}}`)
			case "ClaimCommunityPoints":
				claims++
				input := request.Variables["input"].(map[string]any)
				assert.Equal(t, "bonus-1", input["claimID"])
				assert.Equal(t, "12345", input["channelID"])
				_, _ = fmt.Fprint(w, `{"data":{"claimCommunityPoints":{"currentPoints":500}}}`)
			default:
				t.Errorf("unexpected operation %q", request.OperationName)
				http.Error(w, "unexpected operation", http.StatusBadRequest)
			}
			return
		}
		if r.URL.Path == "/spade" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	c.homeURL = srv.URL
	wt := &watch{c: c, cachedUserID: 42, spadeURLs: map[string]string{}}
	sess := platform.Session{AccessToken: "tok"}
	h, err := wt.start(context.Background(), sess, platform.Stream{Channel: "goldenstreamer", BroadcastID: "b1"})
	require.NoError(t, err)
	assert.Equal(t, "12345", h.ChannelID)
	require.NoError(t, wt.heartbeat(context.Background(), h))
	assert.Equal(t, 2, contextQueries)
	assert.Equal(t, 2, claims)
}

func TestClaimAvailableCommunityPointsSkipsAbsentBonus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"data":{"community":{"channel":{"id":"12345","self":{"communityPoints":{"availableClaim":null}}}}}}`)
	}))
	defer srv.Close()

	wt := &watch{c: newTestClient(srv.URL)}
	channelID, claimed, err := wt.claimAvailableCommunityPoints(context.Background(), "token", "goldenstreamer")
	require.NoError(t, err)
	assert.Equal(t, "12345", channelID)
	assert.False(t, claimed)
}

func TestClaimAvailableCommunityPointsRejectsMissingMutationResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			OperationName string `json:"operationName"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
		switch request.OperationName {
		case "ChannelPointsContext":
			_, _ = fmt.Fprint(w, `{"data":{"community":{"channel":{"id":"12345","self":{"communityPoints":{"availableClaim":{"id":"bonus-1"}}}}}}}`)
		case "ClaimCommunityPoints":
			_, _ = fmt.Fprint(w, `{"data":{"claimCommunityPoints":null},"errors":[{"message":"claim failed"}]}`)
		default:
			t.Errorf("unexpected operation %q", request.OperationName)
			http.Error(w, "unexpected operation", http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	wt := &watch{c: newTestClient(srv.URL)}
	channelID, claimed, err := wt.claimAvailableCommunityPoints(context.Background(), "token", "goldenstreamer")
	require.Error(t, err)
	assert.Equal(t, "12345", channelID)
	assert.False(t, claimed)
}
