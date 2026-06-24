package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/justinas/nosurf"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/auth"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

// seedAdmin inserts an admin row with the given plain-text password (hashed).
func seedAdmin(t *testing.T, q *gen.Queries, password string) {
	t.Helper()
	hash, err := auth.HashPassword(password)
	require.NoError(t, err)
	err = q.UpsertAdmin(context.Background(), gen.UpsertAdminParams{
		PasswordHash: hash,
		CreatedAt:    1,
	})
	require.NoError(t, err)
}

// getCSRFToken does a GET /api/auth/info through the router and returns the
// masked CSRF token (from the csrftoken cookie) plus all cookies for replay.
func getCSRFToken(t *testing.T, h http.Handler) (token string, cookies []*http.Cookie) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/auth/info", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, "GET /api/auth/info must succeed")
	cookies = rec.Result().Cookies()
	for _, c := range cookies {
		if c.Name == "csrftoken" {
			token = c.Value
		}
	}
	require.NotEmpty(t, token, "csrftoken cookie must be set by GET /api/auth/info")
	return token, cookies
}

// apiLoginPOST builds and sends a POST /api/login request with CSRF token + cookies.
func apiLoginPOST(t *testing.T, h http.Handler, password string, tok string, cookies []*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	body := strings.NewReader(`{"password":"` + password + `"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/login", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", tok)
	// nosurf same-origin check requires matching Origin or Referer.
	req.Header.Set("Origin", "http://example.com")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestAPIAuthInfo_Public(t *testing.T) {
	s, q := newTestSettings(t)
	h := NewRouter(Deps{Q: q, Session: scsNew(), SettingsStore: s, SecureCookies: false})
	// public GET, no auth, no CSRF needed for GET
	req := httptest.NewRequest(http.MethodGet, "/api/auth/info", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"oidc_enabled"`)
}

func TestAPIAuthInfo_OIDCFields(t *testing.T) {
	s, q := newTestSettings(t)
	h := NewRouter(Deps{Q: q, Session: scsNew(), SettingsStore: s, SecureCookies: false})
	req := httptest.NewRequest(http.MethodGet, "/api/auth/info", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, `"oidc_enabled"`)
	assert.Contains(t, body, `"oidc_provider"`)
}

func TestAPIAuthInfo_SetsCSRFCookie(t *testing.T) {
	s, q := newTestSettings(t)
	h := NewRouter(Deps{Q: q, Session: scsNew(), SettingsStore: s, SecureCookies: false})
	req := httptest.NewRequest(http.MethodGet, "/api/auth/info", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	var found *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == "csrftoken" {
			found = c
		}
	}
	require.NotNil(t, found, "GET /api/auth/info must set csrftoken cookie")
	assert.False(t, found.HttpOnly, "csrftoken must be JS-readable")
	assert.NotEmpty(t, found.Value)
}

func TestAPILogin_NoCSRFToken_Returns403(t *testing.T) {
	s, q := newTestSettings(t)
	seedAdmin(t, q, "correct-horse")
	h := NewRouter(Deps{Q: q, Session: scsNew(), SettingsStore: s, SecureCookies: false})
	// POST without X-CSRF-Token -> nosurf 403, proving the route exists and is
	// CSRF-protected (not 401, which would mean it's behind RequireAdminAPI).
	req := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(`{"password":"correct-horse"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusForbidden, rec.Code)
	assert.Contains(t, rec.Body.String(), `"code":"csrf"`)
}

func TestAPILogin_WrongPassword(t *testing.T) {
	s, q := newTestSettings(t)
	seedAdmin(t, q, "correct-horse")
	h := NewRouter(Deps{Q: q, Session: scsNew(), SettingsStore: s, SecureCookies: false})

	tok, cookies := getCSRFToken(t, h)
	rec := apiLoginPOST(t, h, "nope", tok, cookies)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), `"code":"wrong_password"`)
}

func TestAPILogin_CorrectPassword(t *testing.T) {
	s, q := newTestSettings(t)
	seedAdmin(t, q, "correct-horse")
	h := NewRouter(Deps{Q: q, Session: scsNew(), SettingsStore: s, SecureCookies: false})

	tok, cookies := getCSRFToken(t, h)
	rec := apiLoginPOST(t, h, "correct-horse", tok, cookies)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"ok":true`)
}

func TestAPILogin_NoAdmin_Returns400(t *testing.T) {
	s, q := newTestSettings(t)
	// No admin seeded.
	h := NewRouter(Deps{Q: q, Session: scsNew(), SettingsStore: s, SecureCookies: false})

	tok, cookies := getCSRFToken(t, h)
	rec := apiLoginPOST(t, h, "anything", tok, cookies)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), `"code":"admin_not_configured"`)
}

// TestAPILogin_IsPublicNotBehindRequireAdminAPI proves the /api/login route is
// accessible pre-auth. Without AUTHBYPASS, a 403 (CSRF) means it reached the
// route (public); a 401 would mean RequireAdminAPI blocked it first.
func TestAPILogin_IsPublicNotBehindRequireAdminAPI(t *testing.T) {
	s, q := newTestSettings(t)
	h := NewRouter(Deps{Q: q, Session: scsNew(), SettingsStore: s, SecureCookies: false})
	req := httptest.NewRequest(http.MethodPost, "/api/login", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	// 403 (CSRF) not 401 (RequireAdminAPI): proves route is PUBLIC.
	assert.NotEqual(t, http.StatusUnauthorized, rec.Code)
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

// Compile-time proof that nosurf is imported (via CookieName reference).
var _ = nosurf.CookieName

func TestAPISetup_CreatesAdminWhenNone(t *testing.T) {
	s, q := newTestSettings(t) // fresh DB, no admin
	sm := scsNew()
	h := NewRouter(Deps{Q: q, Session: sm, SettingsStore: s, SecureCookies: false})
	// Get CSRF token via GET /api/auth/info (sets csrftoken cookie).
	tok, cookies := getCSRFToken(t, h)
	req := httptest.NewRequest(http.MethodPost, "/api/setup", strings.NewReader(`{"password":"longenough","confirm":"longenough"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", tok)
	req.Header.Set("Origin", "http://example.com")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"ok":true`)
	exists, _ := q.AdminExists(context.Background())
	assert.True(t, exists)
}

func TestAPISetup_409WhenAdminExists(t *testing.T) {
	s, q := newTestSettings(t)
	seedAdmin(t, q, "already-here")
	sm := scsNew()
	h := NewRouter(Deps{Q: q, Session: sm, SettingsStore: s, SecureCookies: false})
	tok, cookies := getCSRFToken(t, h)
	req := httptest.NewRequest(http.MethodPost, "/api/setup", strings.NewReader(`{"password":"longenough","confirm":"longenough"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", tok)
	req.Header.Set("Origin", "http://example.com")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), `"code":"admin_configured"`)
}

func TestAPISetup_RequireCSRF(t *testing.T) {
	s, q := newTestSettings(t)
	h := NewRouter(Deps{Q: q, Session: scsNew(), SettingsStore: s, SecureCookies: false})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/setup", nil))
	require.Equal(t, http.StatusForbidden, rec.Code)
	assert.Contains(t, rec.Body.String(), `"code":"csrf"`)
}

func TestAPIAuthInfo_IncludesAdminExists(t *testing.T) {
	s, q := newTestSettings(t)
	h := NewRouter(Deps{Q: q, Session: scsNew(), SettingsStore: s, SecureCookies: false})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/auth/info", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"admin_exists"`)
}
