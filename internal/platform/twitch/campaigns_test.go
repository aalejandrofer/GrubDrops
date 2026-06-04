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

	"github.com/chano-fernandez/rust-drops-miner/internal/platform"
)

func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	return b
}

func TestCampaigns_ListActive_FiltersInactive(t *testing.T) {
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
	camps, err := d.listActive(context.Background(), platform.Session{AccessToken: "tok"})
	require.NoError(t, err)
	require.Len(t, camps, 1) // EXPIRED is filtered
	assert.Equal(t, "camp1", camps[0].ID)
	assert.Equal(t, "Rust", camps[0].Game)
	require.Len(t, camps[0].Benefits, 1)
	assert.Equal(t, "Wolf Helmet", camps[0].Benefits[0].Name)
	assert.Equal(t, 60, camps[0].Benefits[0].RequiredMinutes)
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
