package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alexedwards/scs/v2"

	"github.com/aalejandrofer/grubdrops/internal/store"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

// brokenSettings returns a settings store whose underlying DB is already
// closed, so every write (Set*) fails. Used to prove the save handlers
// surface a write failure instead of falsely reporting success.
func brokenSettings(t *testing.T) (*store.Settings, *gen.Queries) {
	t.Helper()
	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "broken.db"))
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	q := gen.New(db)
	s := store.NewSettings(q)
	_ = db.Close() // every subsequent Set* now errors
	return s, q
}

// loadSession attaches a fresh scs session to the request so the handler's
// d.sm.Put(flash) calls work without the LoadAndSave middleware.
func loadSession(t *testing.T, sm *scs.SessionManager, req *http.Request) (*http.Request, context.Context) {
	t.Helper()
	ctx, err := sm.Load(req.Context(), "")
	if err != nil {
		t.Fatalf("session load: %v", err)
	}
	return req.WithContext(ctx), ctx
}

func formPost(target, body string) *http.Request {
	req := httptest.NewRequest("POST", target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}

// TestPostGeneral_FailedWriteNotReportedAsSuccess proves the General-tab save
// does not show "settings saved" (303 redirect + success flash) when the
// underlying DB write fails — the silent-save-failure bug.
func TestPostGeneral_FailedWriteNotReportedAsSuccess(t *testing.T) {
	s, q := brokenSettings(t)
	sm := scs.New()
	d := &settingsDeps{s: s, q: q, sm: sm}

	req, ctx := loadSession(t, sm, formPost("/settings", "log_level=info"))
	rec := httptest.NewRecorder()
	d.postGeneral(rec, req)

	if rec.Code == http.StatusSeeOther {
		t.Fatalf("failed save reported as success (303 redirect)")
	}
	if flash := strings.ToLower(sm.GetString(ctx, "flash")); strings.Contains(flash, "saved") {
		t.Fatalf("failed save left a success flash: %q", flash)
	}
}

func TestPostPriorityMode_FailedWriteNotReportedAsSuccess(t *testing.T) {
	s, q := brokenSettings(t)
	sm := scs.New()
	d := &settingsDeps{s: s, q: q, sm: sm}

	req, ctx := loadSession(t, sm, formPost("/settings/priority-mode", "priority_mode=ordered"))
	rec := httptest.NewRecorder()
	d.postPriorityMode(rec, req)

	if rec.Code == http.StatusSeeOther {
		t.Fatalf("failed save reported as success (303 redirect)")
	}
	if flash := strings.ToLower(sm.GetString(ctx, "flash")); strings.Contains(flash, "saved") {
		t.Fatalf("failed save left a success flash: %q", flash)
	}
}

func TestPostExperimental_FailedWriteNotReportedAsSuccess(t *testing.T) {
	s, q := brokenSettings(t)
	sm := scs.New()
	d := &settingsDeps{s: s, q: q, sm: sm}

	req, ctx := loadSession(t, sm, formPost("/settings/experimental", "kick_watch_mode=ws"))
	rec := httptest.NewRecorder()
	d.postExperimental(rec, req)

	if rec.Code == http.StatusSeeOther {
		t.Fatalf("failed save reported as success (303 redirect)")
	}
	if flash := strings.ToLower(sm.GetString(ctx, "flash")); strings.Contains(flash, "saved") {
		t.Fatalf("failed save left a success flash: %q", flash)
	}
}
