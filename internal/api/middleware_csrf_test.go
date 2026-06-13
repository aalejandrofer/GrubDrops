package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/justinas/nosurf"
)

// formBody builds a properly URL-encoded form body carrying the CSRF token.
// nosurf masks the token with base64.StdEncoding, which can emit '+', '/' and
// '=' — these must be percent-encoded or the server reads a corrupted token.
func formBody(token string) *strings.Reader {
	return strings.NewReader(url.Values{"csrf_token": {token}}.Encode())
}

// echoToken is a trivial success handler that writes the request's CSRF token,
// so a test client can read it back from a GET and replay it on a POST.
func echoToken(w http.ResponseWriter, r *http.Request) {
	_, _ = io.WriteString(w, nosurf.Token(r))
}

// issueToken does a GET through the CSRF middleware and returns the masked
// token (from the body) plus the csrf_token cookie the server set.
func issueToken(t *testing.T, h http.Handler, scheme, host string) (token string, cookie *http.Cookie) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, scheme+"://"+host+"/login", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	res := rec.Result()
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	for _, c := range res.Cookies() {
		if c.Name == nosurf.CookieName {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatalf("no %s cookie issued on GET", nosurf.CookieName)
	}
	if string(body) == "" {
		t.Fatal("no token rendered on GET")
	}
	return string(body), cookie
}

// TestCSRF_PlainHTTPRoundTrip is the regression test for issue #15: on a
// plain-HTTP self-host (GRUB_SECURE_COOKIES=0, the default) a same-origin POST
// with the issued token must pass. Before the fix, nosurf hardcoded its
// self-origin scheme to https, so the http:// Origin/Referer never matched and
// every POST failed with "CSRF token invalid".
func TestCSRF_PlainHTTPRoundTrip(t *testing.T) {
	h := CSRF(false)(http.HandlerFunc(echoToken))

	token, cookie := issueToken(t, h, "http", "pi:8080")

	req := httptest.NewRequest(http.MethodPost, "http://pi:8080/login", formBody(token))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	// Browsers send Origin (and Referer) on same-site form POSTs; this is the
	// path nosurf falls back to when Sec-Fetch-Site is absent.
	req.Header.Set("Origin", "http://pi:8080")
	req.Header.Set("Referer", "http://pi:8080/login")
	req.AddCookie(cookie)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("plain-HTTP same-origin POST: got %d, want 200 (body=%q)", rec.Code, rec.Body.String())
	}
}

// TestCSRF_BlocksCrossOrigin proves the protection is NOT a no-op: a POST whose
// Origin is a different site is still rejected even with a valid token+cookie.
func TestCSRF_BlocksCrossOrigin(t *testing.T) {
	h := CSRF(false)(http.HandlerFunc(echoToken))
	token, cookie := issueToken(t, h, "http", "pi:8080")

	req := httptest.NewRequest(http.MethodPost, "http://pi:8080/login", formBody(token))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "http://evil.example.com")
	req.AddCookie(cookie)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-origin POST: got %d, want 403", rec.Code)
	}
}

// TestCSRF_BlocksMissingToken proves a same-origin POST with the cookie but no
// form/header token is still rejected (token comparison enforced).
func TestCSRF_BlocksMissingToken(t *testing.T) {
	h := CSRF(false)(http.HandlerFunc(echoToken))
	_, cookie := issueToken(t, h, "http", "pi:8080")

	req := httptest.NewRequest(http.MethodPost, "http://pi:8080/login", nil)
	req.Header.Set("Origin", "http://pi:8080")
	req.AddCookie(cookie)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("missing-token POST: got %d, want 403", rec.Code)
	}
}

// TestCSRF_SecureHTTPSRoundTrip covers the HTTPS deployment: with secure
// cookies on and the request arriving as https (here via X-Forwarded-Proto
// from a TLS-terminating proxy), a same-origin https POST must pass.
func TestCSRF_SecureHTTPSRoundTrip(t *testing.T) {
	h := CSRF(true)(http.HandlerFunc(echoToken))

	// GET to issue the token, simulating proxy-forwarded HTTPS.
	getReq := httptest.NewRequest(http.MethodGet, "http://drops.example.com/login", nil)
	getReq.Header.Set("X-Forwarded-Proto", "https")
	getRec := httptest.NewRecorder()
	h.ServeHTTP(getRec, getReq)
	var cookie *http.Cookie
	for _, c := range getRec.Result().Cookies() {
		if c.Name == nosurf.CookieName {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatal("no csrf cookie issued")
	}
	token := getRec.Body.String()

	req := httptest.NewRequest(http.MethodPost, "http://drops.example.com/login", formBody(token))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("Origin", "https://drops.example.com")
	req.AddCookie(cookie)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("HTTPS same-origin POST: got %d, want 200 (body=%q)", rec.Code, rec.Body.String())
	}
}

// TestRequestIsHTTPS pins the scheme-detection logic the same-origin check
// depends on.
func TestRequestIsHTTPS(t *testing.T) {
	tests := []struct {
		name    string
		secure  bool
		proto   string // X-Forwarded-Proto
		wantTLS bool
	}{
		{"plain http default", false, "", false},
		{"plain http ignores spoofed xfp when insecure", false, "https", false},
		{"secure honors xfp https", true, "https", true},
		{"secure honors xfp http", true, "http", false},
		{"secure no xfp assumes https", true, "", true},
		{"secure honors chained xfp", true, "https, http", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "http://h/login", nil)
			if tc.proto != "" {
				req.Header.Set("X-Forwarded-Proto", tc.proto)
			}
			if got := requestIsHTTPS(req, tc.secure); got != tc.wantTLS {
				t.Fatalf("requestIsHTTPS=%v want %v", got, tc.wantTLS)
			}
		})
	}
}
