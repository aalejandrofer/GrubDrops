package kick

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// sessionFor builds a platform.Session carrying the given cookies.
func sessionFor(t *testing.T, accountID string, cookies ...cookie) platform.Session {
	t.Helper()
	s, err := encodeSession(kickSession{Cookies: cookies})
	if err != nil {
		t.Fatalf("encodeSession: %v", err)
	}
	s.AccountID = accountID
	return s
}

// A rotation must reach the session store exactly once, or the fresh cookie
// dies with the process and nothing is gained.
func TestCaptureCookies_PersistsOnChange(t *testing.T) {
	var gotAccount string
	var calls int
	b := &Backend{SessionPersister: func(accountID string, _ platform.Session) error {
		calls++
		gotAccount = accountID
		return nil
	}}

	sess := sessionFor(t, "acc_1", cookie{Name: "session_token", Value: "old"})
	b.captureCookies(sess, []*http.Cookie{{Name: "session_token", Value: "new"}})

	if calls != 1 {
		t.Fatalf("persister calls = %d, want 1", calls)
	}
	if gotAccount != "acc_1" {
		t.Fatalf("persisted under account %q, want acc_1", gotAccount)
	}
}

// No rotation means no write — otherwise every API call would hit the
// session store, which encrypts on every Put.
func TestCaptureCookies_NoWriteWithoutChange(t *testing.T) {
	var calls int
	b := &Backend{SessionPersister: func(string, platform.Session) error {
		calls++
		return nil
	}}

	sess := sessionFor(t, "acc_1", cookie{Name: "session_token", Value: "same"})
	b.captureCookies(sess, []*http.Cookie{{Name: "session_token", Value: "same"}})

	if calls != 0 {
		t.Fatalf("persister calls = %d, want 0", calls)
	}
}

// With no account id there is nothing to key the write on, so it must be
// skipped rather than written under an empty key.
func TestCaptureCookies_SkipsWithoutAccountID(t *testing.T) {
	var calls int
	b := &Backend{SessionPersister: func(string, platform.Session) error {
		calls++
		return nil
	}}

	sess := sessionFor(t, "", cookie{Name: "session_token", Value: "old"})
	b.captureCookies(sess, []*http.Cookie{{Name: "session_token", Value: "new"}})

	if calls != 0 {
		t.Fatalf("persister calls = %d, want 0 for an empty account id", calls)
	}
}

// A backend with no persister wired (tests, any non-production caller) must
// not panic.
func TestCaptureCookies_NilPersisterIsNoop(t *testing.T) {
	b := &Backend{}
	sess := sessionFor(t, "acc_1", cookie{Name: "session_token", Value: "old"})
	b.captureCookies(sess, []*http.Cookie{{Name: "session_token", Value: "new"}})
}

// RefreshSession must hand back the freshest captured cookie rather than
// returning its input unchanged, which is what it did before.
func TestRefreshSession_ReturnsCapturedCookie(t *testing.T) {
	b := &Backend{}
	sess := sessionFor(t, "acc_1", cookie{Name: "session_token", Value: "old"})
	b.captureCookies(sess, []*http.Cookie{{Name: "session_token", Value: "new"}})

	got, err := b.RefreshSession(nil, sess)
	if err != nil {
		t.Fatalf("RefreshSession: %v", err)
	}
	ks, err := decodeSession(got)
	if err != nil {
		t.Fatalf("decodeSession: %v", err)
	}
	if v := cookieValue(ks, "session_token"); v != "new" {
		t.Fatalf("RefreshSession returned session_token = %q, want new", v)
	}
}

// An account with nothing captured gets its input back untouched.
func TestRefreshSession_PassthroughWhenNothingCaptured(t *testing.T) {
	b := &Backend{}
	sess := sessionFor(t, "acc_1", cookie{Name: "session_token", Value: "tok"})

	got, err := b.RefreshSession(nil, sess)
	if err != nil {
		t.Fatalf("RefreshSession: %v", err)
	}
	ks, err := decodeSession(got)
	if err != nil {
		t.Fatalf("decodeSession: %v", err)
	}
	if v := cookieValue(ks, "session_token"); v != "tok" {
		t.Fatalf("session_token = %q, want the original tok", v)
	}
}

// TestCaptureCookies_PreservesExpiresAt guards against a decode-then-encode
// round trip through kickSession (which does not model ExpiresAt): a
// rotation must never make a live session look expired to the boot-time
// scheduler's refresh gate (cmd/miner/main.go: s.ExpiresAt.Before(now) with
// no RefreshToken replaces the account with an idling nopRunner).
func TestCaptureCookies_PreservesExpiresAt(t *testing.T) {
	expires := time.Now().Add(7 * 24 * time.Hour).Truncate(time.Second)
	blob := `{"cookies":[{"name":"session_token","value":"old"}],"xsrf_token":""}`
	sess := platform.Session{
		AccountID: "acc_1",
		Cookies:   map[string]string{"kick": blob},
		ExpiresAt: expires,
	}

	var persisted platform.Session
	b := &Backend{SessionPersister: func(_ string, s platform.Session) error {
		persisted = s
		return nil
	}}
	b.captureCookies(sess, []*http.Cookie{{Name: "session_token", Value: "new"}})

	if !persisted.ExpiresAt.Equal(expires) {
		t.Fatalf("persisted ExpiresAt = %v, want unchanged %v", persisted.ExpiresAt, expires)
	}
}

// TestCaptureCookies_PreservesUnrelatedBlobKeys guards against a
// decode-then-encode round trip through kickSession, which models only
// cookies/xsrf_token/user_agent: the login handler's stored blob also
// carries "channels" (internal/api/handlers_login_kick.go,
// kickSessionForStorage), and losing it on the first rotation leaves the
// account with nothing to watch.
func TestCaptureCookies_PreservesUnrelatedBlobKeys(t *testing.T) {
	blob := `{"cookies":[{"name":"session_token","value":"old"}],"xsrf_token":"","channels":["somechannel"]}`
	sess := platform.Session{
		AccountID: "acc_1",
		Cookies:   map[string]string{"kick": blob},
	}

	var persisted platform.Session
	b := &Backend{SessionPersister: func(_ string, s platform.Session) error {
		persisted = s
		return nil
	}}
	b.captureCookies(sess, []*http.Cookie{{Name: "session_token", Value: "new"}})

	var out struct {
		Channels []string `json:"channels"`
	}
	if err := json.Unmarshal([]byte(persisted.Cookies["kick"]), &out); err != nil {
		t.Fatalf("persisted blob did not decode: %v", err)
	}
	if len(out.Channels) != 1 || out.Channels[0] != "somechannel" {
		t.Fatalf("persisted channels = %v, want [somechannel]", out.Channels)
	}
}

// TestCaptureCookies_PersistedBlobHasRotatedValue confirms the cookie value
// actually written to the store (not just the in-memory merge result)
// carries the rotation.
func TestCaptureCookies_PersistedBlobHasRotatedValue(t *testing.T) {
	sess := sessionFor(t, "acc_1", cookie{Name: "session_token", Value: "old"})

	var persisted platform.Session
	b := &Backend{SessionPersister: func(_ string, s platform.Session) error {
		persisted = s
		return nil
	}}
	b.captureCookies(sess, []*http.Cookie{{Name: "session_token", Value: "new"}})

	ks, err := decodeSession(persisted)
	if err != nil {
		t.Fatalf("decodeSession: %v", err)
	}
	if v := cookieValue(ks, "session_token"); v != "new" {
		t.Fatalf("persisted session_token = %q, want new", v)
	}
}

// TestCaptureCookies_NewCookieHasDomainAndPath guards against installing a
// domainless cookie into the browser-watch sidecar: its CDP
// Network.setCookie call needs a domain and aborts the ENTIRE cookie
// install on the first error (internal/auth/browser/sidecar/kick.go
// InstallCookies), so one domainless cookie would silently break the whole
// IVS watch tab's auth, not just that cookie.
func TestCaptureCookies_NewCookieHasDomainAndPath(t *testing.T) {
	sess := sessionFor(t, "acc_1", cookie{Name: "session_token", Value: "tok", Domain: "kick.com", Path: "/"})

	var persisted platform.Session
	b := &Backend{SessionPersister: func(_ string, s platform.Session) error {
		persisted = s
		return nil
	}}
	b.captureCookies(sess, []*http.Cookie{{Name: "XSRF-TOKEN", Value: "xsrf1"}})

	ks, err := decodeSession(persisted)
	if err != nil {
		t.Fatalf("decodeSession: %v", err)
	}
	found := false
	for _, c := range ks.Cookies {
		if c.Name != "XSRF-TOKEN" {
			continue
		}
		found = true
		if c.Domain == "" || c.Path == "" {
			t.Fatalf("new cookie has empty Domain/Path: %+v", c)
		}
	}
	if !found {
		t.Fatal("XSRF-TOKEN cookie not found in persisted session")
	}
}
