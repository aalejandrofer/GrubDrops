package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"github.com/aalejandrofer/grubdrops/internal/store"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

// TestToggleEnabled_FlipsAndReloads proves the per-account enable/disable
// button flips accounts.enabled and triggers a targeted watcher reload, so a
// disable actually stops that account mining (and an enable starts it) without
// touching the rest of the roster.
func TestToggleEnabled_FlipsAndReloads(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	q := gen.New(db)

	const id = "acc_toggle"
	if _, err := db.ExecContext(ctx, `INSERT INTO accounts (id, platform, display_name, status, fingerprint_json, enabled, created_at, updated_at)
		VALUES (?, 'twitch', 'Test', 'idle', '{}', 1, 1, 1)`, id); err != nil {
		t.Fatalf("seed: %v", err)
	}

	sm := scs.New()
	var reloaded []string
	d := accountsDeps{q: q, db: db, sm: sm, rootCtx: ctx,
		reloadAccount: func(_ context.Context, accID string) { reloaded = append(reloaded, accID) }}

	call := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/accounts/"+id+"/toggle", nil)
		req.Header.Set("HX-Request", "true")
		req, _ = loadSession(t, sm, req)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", id)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		rec := httptest.NewRecorder()
		d.toggleEnabled(rec, req)
		return rec
	}

	// enabled 1 -> 0
	rec := call()
	if rec.Code != http.StatusNoContent || rec.Header().Get("HX-Redirect") == "" {
		t.Fatalf("HX toggle: want 204 + HX-Redirect, got %d redirect=%q", rec.Code, rec.Header().Get("HX-Redirect"))
	}
	if acc, _ := q.GetAccount(ctx, id); acc.Enabled != 0 {
		t.Fatalf("after first toggle want enabled=0, got %d", acc.Enabled)
	}
	// disabled 0 -> 1
	call()
	if acc, _ := q.GetAccount(ctx, id); acc.Enabled != 1 {
		t.Fatalf("after second toggle want enabled=1, got %d", acc.Enabled)
	}
	if len(reloaded) != 2 || reloaded[0] != id {
		t.Fatalf("want 2 targeted reloads of %s, got %v", id, reloaded)
	}
}
