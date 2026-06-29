package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/store"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

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
