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

func TestBackend_SatisfiesInterface(t *testing.T) {
	var _ platform.Backend = (*Backend)(nil)
}

func TestBackend_NameTwitch(t *testing.T) {
	b := New()
	assert.Equal(t, "twitch", b.Name())
}

func TestBackend_ListActiveThenEligibleChannels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req gqlRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		switch req.OperationName {
		case OpCampaigns.Name:
			_, _ = w.Write(loadFixture(t, "campaigns.json"))
		case OpDropCampaignDetails.Name:
			_, _ = w.Write(loadFixture(t, "campaign_details.json"))
		case OpGetStreamInfo.Name:
			_, _ = w.Write(loadFixture(t, "streamlive.json"))
		default:
			t.Fatalf("unexpected op %q", req.OperationName)
		}
	}))
	defer srv.Close()

	b := newForTest(srv.URL)
	sess := platform.Session{AccessToken: "tok"}

	camps, err := b.ListActiveCampaigns(context.Background(), sess)
	require.NoError(t, err)
	require.Len(t, camps, 1)

	// Empty allow-list => no streams.
	out, err := b.ListEligibleChannels(context.Background(), sess, camps[0])
	require.NoError(t, err)
	assert.Empty(t, out)

	// Once we cache a streamer, it gets returned.
	b.setAllowedLogins(camps[0].ID, []string{"fakestreamer"})
	out, err = b.ListEligibleChannels(context.Background(), sess, camps[0])
	require.NoError(t, err)
	require.Len(t, out, 1)
	assert.Equal(t, "fakestreamer", out[0].Channel)
}
