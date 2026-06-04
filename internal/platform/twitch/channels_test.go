package twitch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chano-fernandez/rust-drops-miner/internal/platform"
)

func TestChannels_ListEligible_LiveOnly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req gqlRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		assert.Equal(t, OpGetStreamInfo.Name, req.OperationName)
		_, _ = w.Write(loadFixture(t, "streamlive.json"))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	ch := &channels{c: c}
	camp := platform.Campaign{ID: "camp1", Platform: "twitch"}
	out, err := ch.listEligible(context.Background(), platform.Session{AccessToken: "tok"}, camp, []string{"fakestreamer"})
	require.NoError(t, err)
	require.Len(t, out, 1)
	assert.Equal(t, "fakestreamer", out[0].Channel)
	assert.Equal(t, 9001, out[0].ViewerCount)
	assert.True(t, out[0].DropsEnabled)
}

func TestChannels_ListEligible_SkipsOffline(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"user":{"login":"offline","stream":null}}}`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	ch := &channels{c: c}
	camp := platform.Campaign{ID: "camp1", Platform: "twitch"}
	out, err := ch.listEligible(context.Background(), platform.Session{AccessToken: "tok"}, camp, []string{"offline"})
	require.NoError(t, err)
	assert.Empty(t, out)
}

func TestChannels_ListEligible_EmptyAllowList(t *testing.T) {
	c := newTestClient("http://invalid-not-called")
	ch := &channels{c: c}
	camp := platform.Campaign{ID: "camp1", Platform: "twitch"}
	out, err := ch.listEligible(context.Background(), platform.Session{AccessToken: "tok"}, camp, nil)
	require.NoError(t, err)
	assert.Empty(t, out)
}
