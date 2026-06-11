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

	"github.com/aalejandrofer/grubdrops/internal/store"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

// ageKey is a throwaway age identity for encrypting stored sessions in tests.
const ageKey = "AGE-SECRET-KEY-1DZCAXYWJM6M42NSX5GR4QWZZ2JXEYKJ9ZKWYFYSNU997775JJ6XSY85FK9"

// newKickIngestDeps spins up a migrated sqlite store + a kick account and
// returns deps wired for the no-auth helper-ingest handler.
func newKickIngestDeps(t *testing.T, accID string) (*loginKickDeps, *store.SessionStore) {
	t.Helper()
	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	q := gen.New(db)
	if accID != "" {
		now := time.Now().Unix()
		if _, err := q.CreateAccount(context.Background(), gen.CreateAccountParams{
			ID: accID, Platform: "kick", DisplayName: "TTik3r",
			Status: "idle", FingerprintJson: "{}", Enabled: 1,
			CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("seed account: %v", err)
		}
	}
	cr, err := store.NewCryptor(ageKey)
	if err != nil {
		t.Fatalf("cryptor: %v", err)
	}
	ss := store.NewSessionStore(db, q, cr)
	return &loginKickDeps{q: q, sessions: ss}, ss
}

func postKickIngest(t *testing.T, d *loginKickDeps, id string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	r.Post("/helper/accounts/{id}/kick", d.helperIngest)
	req := httptest.NewRequest(http.MethodPost, "/helper/accounts/"+id+"/kick", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestHelperIngest_PersistsKickSessionNoAuthNoChannel(t *testing.T) {
	const id = "acc_0123456789abcdef01234567"
	d, ss := newKickIngestDeps(t, id)

	// No channel field at all — channels are optional now.
	form := url.Values{
		"kick_session": {"sess-value"},
		"xsrf_token":   {"xsrf-value"},
	}
	rec := postKickIngest(t, d, id, form)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (body %q)", rec.Code, rec.Body.String())
	}
	if _, ok, err := ss.Get(context.Background(), id); err != nil || !ok {
		t.Fatalf("session not persisted: ok=%v err=%v", ok, err)
	}
}

func TestHelperIngest_UnknownAccountIs404(t *testing.T) {
	d, _ := newKickIngestDeps(t, "acc_realaccount0000000000000000")
	form := url.Values{"kick_session": {"x"}, "xsrf_token": {"y"}}
	rec := postKickIngest(t, d, "acc_doesnotexist0000000000000000", form)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("want 404 for unknown account, got %d", rec.Code)
	}
}
