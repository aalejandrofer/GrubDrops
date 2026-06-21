package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"github.com/aalejandrofer/grubdrops/internal/store"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

// TestAccountChannels_AddAndRemove proves addChannel inserts a lowercased
// channel row and removeChannel deletes it.
func TestAccountChannels_AddAndRemove(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	q := gen.New(db)

	const id = "acc-1"
	if _, err := db.ExecContext(ctx,
		`INSERT INTO accounts (id, platform, display_name, status, fingerprint_json, enabled, created_at, updated_at)
		VALUES (?, 'twitch', 'Test', 'idle', '{}', 1, 1, 1)`, id); err != nil {
		t.Fatalf("seed: %v", err)
	}

	sm := scs.New()
	d := accountsDeps{q: q, db: db, sm: sm, rootCtx: ctx}

	makeReq := func(path, channel string, handler func(http.ResponseWriter, *http.Request)) *httptest.ResponseRecorder {
		form := url.Values{"channel": {channel}}
		req := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req, _ = loadSession(t, sm, req)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", id)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		rec := httptest.NewRecorder()
		handler(rec, req)
		return rec
	}

	// ACT add: POST with "channel=XQC" (uppercase — must be lowercased).
	rec := makeReq("/accounts/"+id+"/channels/add", "XQC", d.addChannel)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("add: want 303, got %d", rec.Code)
	}

	// ASSERT: ListAccountChannels returns exactly ["xqc"].
	rows, err := q.ListAccountChannels(ctx, id)
	if err != nil {
		t.Fatalf("list after add: %v", err)
	}
	if len(rows) != 1 || rows[0].Channel != "xqc" {
		t.Fatalf("after add: want [xqc], got %v", rows)
	}

	// ACT remove: POST with "channel=xqc".
	rec = makeReq("/accounts/"+id+"/channels/remove", "xqc", d.removeChannel)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("remove: want 303, got %d", rec.Code)
	}

	// ASSERT: list is now empty.
	rows, err = q.ListAccountChannels(ctx, id)
	if err != nil {
		t.Fatalf("list after remove: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("after remove: want empty, got %v", rows)
	}
}
