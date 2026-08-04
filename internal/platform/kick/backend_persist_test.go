package kick

import (
	"net/http"
	"testing"

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
