package twitch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// pointsReq is one decoded gql request seen by pointsServer.
type pointsReq struct {
	Op        string
	Hash      string
	Variables map[string]any
	Header    http.Header
}

// pointsServer answers each op with the scripted body and records requests.
func pointsServer(t *testing.T, bodies map[string]string) (*httptest.Server, func() []pointsReq) {
	t.Helper()
	var mu sync.Mutex
	var seen []pointsReq
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			OperationName string         `json:"operationName"`
			Variables     map[string]any `json:"variables"`
			Extensions    struct {
				PersistedQuery struct {
					Sha256Hash string `json:"sha256Hash"`
				} `json:"persistedQuery"`
			} `json:"extensions"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		mu.Lock()
		seen = append(seen, pointsReq{req.OperationName, req.Extensions.PersistedQuery.Sha256Hash, req.Variables, r.Header.Clone()})
		mu.Unlock()
		body, ok := bodies[req.OperationName]
		if !ok {
			t.Errorf("unexpected op %q", req.OperationName)
			http.Error(w, "unexpected", http.StatusBadRequest)
			return
		}
		_, _ = fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv, func() []pointsReq {
		mu.Lock()
		defer mu.Unlock()
		return append([]pointsReq(nil), seen...)
	}
}

func TestClaimChannelPoints_ClaimsAvailableBonus(t *testing.T) {
	srv, seen := pointsServer(t, map[string]string{
		"ChannelPointsContext": `{"data":{"community":{"channel":{"id":"12345","self":{"communityPoints":{"balance":450,"availableClaim":{"id":"bonus-1"}}}}}}}`,
		"ClaimCommunityPoints": `{"data":{"claimCommunityPoints":{"claim":{"id":"bonus-1"},"currentPoints":500,"error":null}}}`,
	})
	b := newForTest(srv.URL)
	got, err := b.ClaimChannelPoints(context.Background(), platform.Session{AccessToken: "tok"}, "goldenstreamer")
	require.NoError(t, err)
	assert.Equal(t, platform.PointsClaim{Claimed: true, Gained: 50, Balance: 500}, got)

	reqs := seen()
	require.Len(t, reqs, 2)
	assert.Equal(t, "ChannelPointsContext", reqs[0].Op)
	assert.Equal(t, OpChannelPointsContext.Hash, reqs[0].Hash)
	assert.Equal(t, map[string]any{"channelLogin": "goldenstreamer"}, reqs[0].Variables)
	assert.Equal(t, "ClaimCommunityPoints", reqs[1].Op)
	assert.Equal(t, OpClaimCommunityPoints.Hash, reqs[1].Hash)
	assert.Equal(t, map[string]any{"input": map[string]any{"channelID": "12345", "claimID": "bonus-1"}}, reqs[1].Variables)
	for _, r := range reqs {
		assert.Equal(t, "OAuth tok", r.Header.Get("Authorization"))
		assert.Empty(t, r.Header.Values("Client-Integrity"), "never send Client-Integrity")
	}
}

func TestClaimChannelPoints_NoBonusSendsOneQuery(t *testing.T) {
	srv, seen := pointsServer(t, map[string]string{
		"ChannelPointsContext": `{"data":{"community":{"channel":{"id":"12345","self":{"communityPoints":{"balance":450,"availableClaim":null}}}}}}`,
	})
	b := newForTest(srv.URL)
	got, err := b.ClaimChannelPoints(context.Background(), platform.Session{AccessToken: "tok"}, "goldenstreamer")
	require.NoError(t, err)
	assert.Equal(t, platform.PointsClaim{Balance: 450}, got)
	assert.Len(t, seen(), 1)
}

func TestClaimChannelPoints_RefusedClaimErrors(t *testing.T) {
	srv, _ := pointsServer(t, map[string]string{
		"ChannelPointsContext": `{"data":{"community":{"channel":{"id":"12345","self":{"communityPoints":{"availableClaim":{"id":"bonus-1"}}}}}}}`,
		"ClaimCommunityPoints": `{"data":{"claimCommunityPoints":{"claim":null,"currentPoints":null,"error":{"code":"NOT_FOUND"}}}}`,
	})
	b := newForTest(srv.URL)
	got, err := b.ClaimChannelPoints(context.Background(), platform.Session{AccessToken: "tok"}, "goldenstreamer")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "NOT_FOUND")
	assert.False(t, got.Claimed)
}

func TestClaimChannelPoints_MissingResultErrors(t *testing.T) {
	srv, _ := pointsServer(t, map[string]string{
		"ChannelPointsContext": `{"data":{"community":{"channel":{"id":"12345","self":{"communityPoints":{"availableClaim":{"id":"bonus-1"}}}}}}}`,
		"ClaimCommunityPoints": `{"data":{"claimCommunityPoints":null}}`,
	})
	b := newForTest(srv.URL)
	_, err := b.ClaimChannelPoints(context.Background(), platform.Session{AccessToken: "tok"}, "goldenstreamer")
	require.Error(t, err)
}

func TestClaimChannelPoints_UnknownChannelErrors(t *testing.T) {
	srv, _ := pointsServer(t, map[string]string{
		"ChannelPointsContext": `{"data":{"community":null}}`,
	})
	b := newForTest(srv.URL)
	_, err := b.ClaimChannelPoints(context.Background(), platform.Session{AccessToken: "tok"}, "nobody")
	require.Error(t, err)
}

// A rotated hash surfaces as ErrPersistedQueryNotFound on every op, not as a
// generic gql error.
func TestGQL_PersistedQueryNotFoundSentinel(t *testing.T) {
	srv, _ := pointsServer(t, map[string]string{
		"ChannelPointsContext": `{"errors":[{"message":"PersistedQueryNotFound"}]}`,
		"Inventory":            `{"errors":[{"message":"PersistedQueryNotFound"}],"data":null}`,
	})
	b := newForTest(srv.URL)
	_, err := b.ClaimChannelPoints(context.Background(), platform.Session{AccessToken: "tok"}, "goldenstreamer")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrPersistedQueryNotFound), "err=%v", err)

	err = b.c.gql(context.Background(), "tok", OpInventory, nil, nil)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrPersistedQueryNotFound), "err=%v", err)
	assert.Contains(t, err.Error(), "Inventory")

	// Any other application error stays generic.
	srv2, _ := pointsServer(t, map[string]string{"Inventory": `{"errors":[{"message":"service timeout"}]}`})
	b2 := newForTest(srv2.URL)
	err = b2.c.gql(context.Background(), "tok", OpInventory, nil, nil)
	require.Error(t, err)
	assert.False(t, errors.Is(err, ErrPersistedQueryNotFound))
}

func TestChannelPointsBalance_ReadsWithoutClaiming(t *testing.T) {
	srv, seen := pointsServer(t, map[string]string{
		"ChannelPointsContext": `{"data":{"community":{"channel":{"id":"12345","self":{"communityPoints":{"balance":450,"availableClaim":{"id":"bonus-1"}}}}}}}`,
	})
	b := newForTest(srv.URL)
	got, err := b.ChannelPointsBalance(context.Background(), platform.Session{AccessToken: "tok"}, "goldenstreamer")
	require.NoError(t, err)
	assert.Equal(t, 450, got)
	reqs := seen()
	require.Len(t, reqs, 1, "a balance read never claims")
	assert.Equal(t, "ChannelPointsContext", reqs[0].Op)
	assert.Equal(t, map[string]any{"channelLogin": "goldenstreamer"}, reqs[0].Variables)
	assert.Empty(t, reqs[0].Header.Values("Client-Integrity"), "never send Client-Integrity")
}

func TestChannelPointsBalance_NoPointsOnChannel(t *testing.T) {
	srv, _ := pointsServer(t, map[string]string{
		"ChannelPointsContext": `{"data":{"community":{"channel":{"id":"12345","self":{"communityPoints":null}}}}}`,
	})
	b := newForTest(srv.URL)
	_, err := b.ChannelPointsBalance(context.Background(), platform.Session{AccessToken: "tok"}, "goldenstreamer")
	assert.ErrorIs(t, err, platform.ErrNoPointsBalance)
}

func TestChannelPointsBalance_UnknownChannelErrors(t *testing.T) {
	srv, _ := pointsServer(t, map[string]string{
		"ChannelPointsContext": `{"data":{"community":null}}`,
	})
	b := newForTest(srv.URL)
	_, err := b.ChannelPointsBalance(context.Background(), platform.Session{AccessToken: "tok"}, "nobody")
	require.Error(t, err)
	assert.NotErrorIs(t, err, platform.ErrNoPointsBalance)
}
