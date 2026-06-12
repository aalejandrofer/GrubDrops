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

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"

	"github.com/aalejandrofer/grubdrops/internal/store"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

// ageKey is a throwaway age identity for encrypting stored sessions in tests.
const ageKey = "AGE-SECRET-KEY-1DZCAXYWJM6M42NSX5GR4QWZZ2JXEYKJ9ZKWYFYSNU997775JJ6XSY85FK9"

// newKickLoginDeps spins up a migrated sqlite store + a kick account and
// returns deps wired for the Kick login handlers.
func newKickLoginDeps(t *testing.T, accID string) (*loginKickDeps, *store.SessionStore) {
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

func TestLoginKickPost_CookiesTxtPersistsSession(t *testing.T) {
	const id = "acc_0123456789abcdef01234567"
	d, ss := newKickLoginDeps(t, id)
	d.sm = scs.New() // post() writes a flash on success

	r := chi.NewRouter()
	r.Post("/accounts/{id}/login", d.post)
	h := d.sm.LoadAndSave(r)

	form := url.Values{"cookies_txt": {cookiesTxtOK}}
	req := httptest.NewRequest(http.MethodPost, "/accounts/"+id+"/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("want 303 redirect, got %d (body %q)", rec.Code, rec.Body.String())
	}
	if _, ok, err := ss.Get(context.Background(), id); err != nil || !ok {
		t.Fatalf("session not persisted: ok=%v err=%v", ok, err)
	}
}

// parseKickChannels must accept the various separator styles operators
// paste into the form. The web template advertises "comma/space-separated";
// both styles must round-trip.
func TestParseKickChannels(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"empty", "", nil},
		{"single", "alice", []string{"alice"}},
		{"csv", "alice,bob,carol", []string{"alice", "bob", "carol"}},
		{"spaces", "alice bob carol", []string{"alice", "bob", "carol"}},
		{"mixed", "alice, bob; carol\tdave", []string{"alice", "bob", "carol", "dave"}},
		{"dedupe", "Alice,alice,ALICE,bob", []string{"Alice", "bob"}},
		{"trim", "  alice  ,  bob  ", []string{"alice", "bob"}},
		{"empty parts", ",,alice,,,bob,,", []string{"alice", "bob"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, parseKickChannels(tc.in))
		})
	}
}
