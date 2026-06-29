package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/store"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

// TestItems_DecodesEncodedSynthID proves the items handler URL-decodes the chi
// path param so a scrape-synth campaign id with spaces/pipes (which chi hands
// back percent-encoded) still matches the decoded id stored in the campaigns
// table — i.e. Builder Cape / Tubbo's WatchTime load instead of "Couldn't load".
func TestItems_DecodesEncodedSynthID(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	q := gen.New(db)

	const campID = "Minecraft|Builder Cape Sat, May 30"
	now := time.Now().Unix()
	require.NoError(t, q.UpsertCampaign(ctx, gen.UpsertCampaignParams{
		ID: campID, Platform: "twitch", Game: "Minecraft", Name: "Builder Cape",
		StartsAt: now - 60, EndsAt: now + 3600, Status: "active",
		RawJson: "{}", DiscoveredAt: now, Kind: "drop", AccountLinked: 1,
	}))
	require.NoError(t, q.UpsertBenefit(ctx, gen.UpsertBenefitParams{
		ID: campID + "_default", CampaignID: campID, Name: "Builder Cape", RequiredMinutes: 5,
	}))

	d := &dropsDeps{q: q, t: testRenderer(t), loc: time.UTC}
	// Simulate chi handing back the percent-encoded segment.
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", url.PathEscape(campID))
	req := httptest.NewRequest(http.MethodGet, "/drops/campaigns/x/items", nil).
		WithContext(context.WithValue(context.Background(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	d.items(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "Builder Cape", "item should render, not a load error")
	require.NotContains(t, rec.Body.String(), "load items for this campaign")
}

// TestRenderCampaignItems_UnknownID_Renders200LoadError proves the items panel
// never 404s on an unknown/malformed id (which left HTMX showing a permanent
// "Loading…"). It must return 200 with the load-error message instead.
func TestRenderCampaignItems_UnknownID_Renders200LoadError(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	q := gen.New(db)

	d := &dropsDeps{q: q, t: testRenderer(t), loc: time.UTC}
	req := httptest.NewRequest(http.MethodGet, "/drops/campaigns/no-such-id/items", nil)
	rec := httptest.NewRecorder()
	d.renderCampaignItems(rec, req, "no-such-id")

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "load items for this campaign")
}

// TestDropsTable_EscapesCampaignIDInItemsURL proves a malformed scrape-synth
// campaign id (spaces, "|") is percent-encoded in the items hx-get URL so the
// request can route, while a normal uuid id is left intact.
func TestDropsTable_EscapesCampaignIDInItemsURL(t *testing.T) {
	page := dropsPage{
		Tab: tabCurrent,
		Rows: []dropsRow{
			{CampaignID: "Minecraft|Tubbo's WatchTime Sun", Platform: "twitch", When: "x"},
			{CampaignID: "a37ae57b-2268-4472-a8b3", Platform: "twitch", When: "y"},
		},
	}
	out := renderDropsTable(t, page)
	if !strings.Contains(out, "/drops/campaigns/Minecraft%7CTubbo%27s%20WatchTime%20Sun/items") {
		t.Errorf("synth id not path-escaped in hx-get:\n%s", out)
	}
	if !strings.Contains(out, "/drops/campaigns/a37ae57b-2268-4472-a8b3/items") {
		t.Errorf("uuid id should be unchanged")
	}
}
