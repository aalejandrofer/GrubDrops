package twitch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/platform"
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
		case "CurrentUser":
			_, _ = w.Write([]byte(`{"data":{"currentUser":{"login":"testuser"}}}`))
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
	// listActive now also surfaces EXPIRED / UPCOMING campaigns so the
	// /drops page can render past + upcoming tabs. ACTIVE remains the
	// first entry in the fixture.
	require.Len(t, camps, 2)
	require.Equal(t, "active", camps[0].Status)

	// After ListActiveCampaigns the fixture's allow.channels list is loaded
	// (fakestreamer + another). The mock server returns a live stream for
	// every OpGetStreamInfo call, so both allowed channels come back live.
	out, err := b.ListEligibleChannels(context.Background(), sess, camps[0])
	require.NoError(t, err)
	require.NotEmpty(t, out, "allow-list populated from fixture should produce eligible channels")
	assert.Equal(t, "fakestreamer", out[0].Channel)
}

// TestBackend_AllowedChannelCount exposes the allow-list cache so the
// dashboard's "channels" column has a number to render. Unknown
// campaign ids return zero so the row falls back to "no eligible
// channels yet" rather than panicking.
func TestBackend_AllowedChannelCount(t *testing.T) {
	b := New()
	assert.Equal(t, 0, b.AllowedChannelCount("missing"))

	b.setAllowedLogins("camp1", []string{"streamer1", "streamer2", "streamer3"})
	assert.Equal(t, 3, b.AllowedChannelCount("camp1"))
	assert.Equal(t, 0, b.AllowedChannelCount("camp2"), "unknown id stays at zero")
}

func TestBackend_ListActiveCampaignsPopulatesAllowList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req gqlRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		switch req.OperationName {
		case OpCampaigns.Name:
			_, _ = w.Write(loadFixture(t, "campaigns.json"))
		case "CurrentUser":
			_, _ = w.Write([]byte(`{"data":{"currentUser":{"login":"testuser"}}}`))
		case OpDropCampaignDetails.Name:
			_, _ = w.Write(loadFixture(t, "campaign_details.json"))
		case OpGetStreamInfo.Name:
			// streamlive.json returns "fakestreamer" live
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
	// listActive now returns ACTIVE + EXPIRED + UPCOMING. Allow-list
	// fetches only happen for the ACTIVE entry.
	require.Len(t, camps, 2)
	require.Equal(t, "active", camps[0].Status)
	assert.Contains(t, camps[0].AllowedChannels, "fakestreamer", "the allow-list rides on the campaign")
	assert.Equal(t, len(camps[0].AllowedChannels), camps[0].AllowedChannelCount)

	// After ListActiveCampaigns, the allow-list cache should be populated
	// from the fixture's allow.channels[].name field.
	out, err := b.ListEligibleChannels(context.Background(), sess, camps[0])
	require.NoError(t, err)
	require.NotEmpty(t, out, "allow-list cache should be populated after ListActiveCampaigns")
	assert.Equal(t, "fakestreamer", out[0].Channel)
}

func TestBackend_FetchAvatar(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req gqlRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		require.Equal(t, "CurrentUser", req.OperationName)
		_, _ = w.Write([]byte(`{"data":{"currentUser":{"id":"123","login":"grubber","displayName":"Grubber","profileImageURL":"https://static-cdn.jtvnw.net/jtv_user_pictures/abc-profile_image-300x300.png"}}}`))
	}))
	defer srv.Close()

	b := newForTest(srv.URL)
	url, err := b.FetchAvatar(context.Background(), platform.Session{AccessToken: "tok"})
	require.NoError(t, err)
	assert.Equal(t, "https://static-cdn.jtvnw.net/jtv_user_pictures/abc-profile_image-300x300.png", url)
}

func TestBackend_FetchAvatar_NullUser(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"currentUser":null}}`))
	}))
	defer srv.Close()
	b := newForTest(srv.URL)
	_, err := b.FetchAvatar(context.Background(), platform.Session{AccessToken: "tok"})
	require.Error(t, err)
}
