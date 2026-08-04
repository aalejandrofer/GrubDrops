package kick

import (
	"net/http"
	"testing"
)

// A rotated session_token must replace the stored one. This is the whole
// point: Kick reissuing a cookie mid-session was previously thrown away.
func TestMergeCookies_RotatedSessionTokenReplaces(t *testing.T) {
	ks := kickSession{Cookies: []cookie{
		{Name: "session_token", Value: "old"},
		{Name: "kick_session", Value: "ks1"},
	}}
	set := []*http.Cookie{{Name: "session_token", Value: "new"}}

	got, changed := mergeCookies(ks, set)
	if !changed {
		t.Fatal("a rotated session_token must report changed=true")
	}
	if v := cookieValue(got, "session_token"); v != "new" {
		t.Fatalf("session_token = %q, want new", v)
	}
	if v := cookieValue(got, "kick_session"); v != "ks1" {
		t.Fatalf("untouched cookie was lost: kick_session = %q, want ks1", v)
	}
}

// Analytics/consent cookies must never churn the stored session, or every
// request would look like a rotation and hammer the session store.
func TestMergeCookies_IgnoresIrrelevantNames(t *testing.T) {
	ks := kickSession{Cookies: []cookie{{Name: "session_token", Value: "old"}}}
	set := []*http.Cookie{
		{Name: "_ga", Value: "GA1.2.3"},
		{Name: "cf_clearance", Value: "abc"},
	}

	got, changed := mergeCookies(ks, set)
	if changed {
		t.Fatal("non-rotatable cookie names must not report a change")
	}
	if len(got.Cookies) != 1 {
		t.Fatalf("merge added unexpected cookies: %+v", got.Cookies)
	}
}

// Re-sending the SAME value is not a rotation.
func TestMergeCookies_SameValueIsNotAChange(t *testing.T) {
	ks := kickSession{Cookies: []cookie{{Name: "session_token", Value: "same"}}}
	set := []*http.Cookie{{Name: "session_token", Value: "same"}}

	if _, changed := mergeCookies(ks, set); changed {
		t.Fatal("an identical value must not report a change")
	}
}

// A rotatable cookie the session did not have yet is added.
func TestMergeCookies_AddsNewRotatableCookie(t *testing.T) {
	ks := kickSession{Cookies: []cookie{{Name: "session_token", Value: "tok"}}}
	set := []*http.Cookie{{Name: "XSRF-TOKEN", Value: "xsrf1"}}

	got, changed := mergeCookies(ks, set)
	if !changed {
		t.Fatal("a new rotatable cookie must report changed=true")
	}
	if v := cookieValue(got, "XSRF-TOKEN"); v != "xsrf1" {
		t.Fatalf("XSRF-TOKEN = %q, want xsrf1", v)
	}
}

// A rotated XSRF-TOKEN must also update the mirrored XSRFToken field, which
// cookieHeaderFor falls back to and encodeSession copies into Session.CSRF.
func TestMergeCookies_RotatedXSRFUpdatesMirrorField(t *testing.T) {
	ks := kickSession{
		Cookies:   []cookie{{Name: "XSRF-TOKEN", Value: "old"}},
		XSRFToken: "old",
	}
	set := []*http.Cookie{{Name: "XSRF-TOKEN", Value: "new"}}

	got, changed := mergeCookies(ks, set)
	if !changed {
		t.Fatal("expected changed=true")
	}
	if got.XSRFToken != "new" {
		t.Fatalf("XSRFToken mirror = %q, want new", got.XSRFToken)
	}
}

// An empty Set-Cookie value is a deletion signal from the server, not a
// rotation to empty — dropping a live token because of one would log the
// account out on the next request.
func TestMergeCookies_EmptyValueIsIgnored(t *testing.T) {
	ks := kickSession{Cookies: []cookie{{Name: "session_token", Value: "tok"}}}
	set := []*http.Cookie{{Name: "session_token", Value: ""}}

	got, changed := mergeCookies(ks, set)
	if changed {
		t.Fatal("an empty cookie value must not report a change")
	}
	if v := cookieValue(got, "session_token"); v != "tok" {
		t.Fatalf("session_token = %q, want the original tok", v)
	}
}

// No Set-Cookie at all leaves the session untouched.
func TestMergeCookies_NoSetCookieIsNoChange(t *testing.T) {
	ks := kickSession{Cookies: []cookie{{Name: "session_token", Value: "tok"}}}
	if _, changed := mergeCookies(ks, nil); changed {
		t.Fatal("nil Set-Cookie must not report a change")
	}
}

// cookieValue is a test helper: the value of the named cookie, or "".
func cookieValue(ks kickSession, name string) string {
	for _, c := range ks.Cookies {
		if c.Name == name {
			return c.Value
		}
	}
	return ""
}
