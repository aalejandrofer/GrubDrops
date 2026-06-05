package twitch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/rust-drops-miner/internal/platform"
)

func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	return b
}

// listActive must return ALL campaigns (active, expired, upcoming) so the
// /drops page can show past + upcoming tabs. Only ACTIVE campaigns get
// their benefits fetched — EXPIRED / UPCOMING are emitted with empty
// benefit slices to save bandwidth.
func TestCampaigns_ListActive_ReturnsAllStatuses(t *testing.T) {
	var detailCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req gqlRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		switch req.OperationName {
		case OpCampaigns.Name:
			_, _ = w.Write(loadFixture(t, "campaigns.json"))
		case OpDropCampaignDetails.Name:
			detailCalls++
			_, _ = w.Write(loadFixture(t, "campaign_details.json"))
		default:
			t.Fatalf("unexpected op %q", req.OperationName)
		}
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	d := &discovery{c: c}
	camps, err := d.listActive(context.Background(), platform.Session{AccessToken: "tok"})
	require.NoError(t, err)
	require.Len(t, camps, 2) // ACTIVE + EXPIRED both retained

	// Find each by ID
	byID := map[string]platform.Campaign{}
	for _, c := range camps {
		byID[c.ID] = c
	}

	active := byID["camp1"]
	assert.Equal(t, "active", active.Status)
	assert.Equal(t, "Rust", active.Game)
	require.Len(t, active.Benefits, 1)
	assert.Equal(t, "Wolf Helmet", active.Benefits[0].Name)
	assert.Equal(t, 60, active.Benefits[0].RequiredMinutes)

	expired := byID["camp2"]
	assert.Equal(t, "expired", expired.Status)
	assert.Empty(t, expired.Benefits, "EXPIRED campaigns must not fetch details")

	assert.Equal(t, 1, detailCalls, "only ACTIVE campaigns issue detail fetches")
}

// When Session.GameFilter rejects every active campaign's game,
// listActive must NOT issue any OpDropCampaignDetails fetch — the
// whitelist short-circuits before per-campaign roundtrips. This is the
// guardrail that keeps bandwidth bounded by the whitelist size, not by
// the total number of active campaigns Twitch is currently running.
func TestCampaigns_ListActive_GameFilterShortCircuits(t *testing.T) {
	var detailCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req gqlRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		switch req.OperationName {
		case OpCampaigns.Name:
			_, _ = w.Write(loadFixture(t, "campaigns.json"))
		case OpDropCampaignDetails.Name:
			detailCalls++
			_, _ = w.Write(loadFixture(t, "campaign_details.json"))
		default:
			t.Fatalf("unexpected op %q", req.OperationName)
		}
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	d := &discovery{c: c}
	sess := platform.Session{
		AccessToken: "tok",
		GameFilter:  func(game string) bool { return false }, // reject everything
	}
	camps, err := d.listActive(context.Background(), sess)
	require.NoError(t, err)
	assert.Empty(t, camps)
	assert.Zero(t, detailCalls, "GameFilter must skip per-campaign detail fetches")
}

// When GameFilter allows the campaign's game, listActive should
// continue fetching details and return the matched campaign. Mirrors
// the existing positive path but with the filter explicitly enabled.
func TestCampaigns_ListActive_GameFilterAllowsMatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req gqlRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		switch req.OperationName {
		case OpCampaigns.Name:
			_, _ = w.Write(loadFixture(t, "campaigns.json"))
		case OpDropCampaignDetails.Name:
			_, _ = w.Write(loadFixture(t, "campaign_details.json"))
		default:
			t.Fatalf("unexpected op %q", req.OperationName)
		}
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	d := &discovery{c: c}
	sess := platform.Session{
		AccessToken: "tok",
		GameFilter:  func(game string) bool { return game == "Rust" },
	}
	camps, err := d.listActive(context.Background(), sess)
	require.NoError(t, err)
	// All Rust campaigns (active + expired) pass the whitelist; only the
	// ACTIVE one gets its benefits fetched.
	require.Len(t, camps, 2)
	for _, c := range camps {
		assert.Equal(t, "Rust", c.Game)
	}
}

func TestCampaigns_Inventory_ParsesProgress(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(loadFixture(t, "inventory.json"))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	d := &discovery{c: c}
	pr, err := d.inventory(context.Background(), platform.Session{AccessToken: "tok"})
	require.NoError(t, err)
	require.Len(t, pr, 1)
	assert.Equal(t, "drop1", pr[0].BenefitID)
	assert.Equal(t, 30, pr[0].MinutesWatched)
	assert.False(t, pr[0].Claimed)
}
