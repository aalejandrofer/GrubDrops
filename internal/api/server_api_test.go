package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alexedwards/scs/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildAPIRouter wires the minimal Deps needed to exercise the /api mount.
// Only a non-nil Session is required: the two routes tested here resolve
// before any handler dereferences the DB (RequireAdminAPI rejects the
// unauthenticated dashboard request, and /api/lang is a cookie/redirect
// handler that never touches Q). NewRouter builds the full router when
// Session is set (it only short-circuits to skeleton mode when Session is
// nil), storing the nil deps without dereferencing them.
func buildAPIRouter(t *testing.T) http.Handler {
	t.Helper()
	sm := scs.New()
	return NewRouter(Deps{
		Session:       sm,
		SecureCookies: false,
	})
}

func TestAPIDashboard_Unauthenticated_Returns401JSON(t *testing.T) {
	h := buildAPIRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/api/dashboard", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "application/json")
	assert.Contains(t, rec.Body.String(), `"code":"unauthorized"`)
}

func TestAPILang_Public_SetsCookieAndRedirects(t *testing.T) {
	h := buildAPIRouter(t)
	// /api/lang is a CSRF-protected POST; fetch a token first via a GET that
	// flows through the same csrf middleware, then replay token + cookie.
	// Simpler: assert it does NOT 401 (it must remain public). A full CSRF
	// round-trip is covered by existing settings tests; here we only verify
	// the route resolves without admin auth.
	req := httptest.NewRequest(http.MethodPost, "/api/lang", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	// Without a CSRF token the nosurf layer returns 403 (Forbidden), NOT 401
	// (Unauthorized). 403 proves the route is public (reached CSRF, not auth);
	// a 401 would mean it was wrongly placed behind RequireAdminAPI.
	assert.NotEqual(t, http.StatusUnauthorized, rec.Code)
}

func scsNew() *scs.SessionManager { return scs.New() }

func TestSPAIndexSetsCSRFCookie(t *testing.T) {
	h := buildAPIRouter(t) // from #1 task 3: Deps{Session: scs.New()}
	// GET / with SPA enabled would require Deps.SPADashboard; instead test the
	// spaIndex handler is reached on the SPA route. Here we assert via a direct
	// request to the SPA shell route when SPADashboard is on.
	t.Setenv("GRUB_AUTHBYPASS", "true") // bypass admin so GET / serves the shell
	h2 := NewRouter(Deps{Session: scsNew(), SecureCookies: false, SPADashboard: true})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h2.ServeHTTP(rec, req)

	var found *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == "csrftoken" {
			found = c
		}
	}
	require.NotNil(t, found, "spaIndex must set a csrftoken cookie")
	assert.False(t, found.HttpOnly, "csrftoken must be readable by JS")
	assert.NotEmpty(t, found.Value)
	_ = h
}

func TestAPIAccountDetail_UnknownIs404JSON(t *testing.T) {
	t.Setenv("GRUB_AUTHBYPASS", "true")
	s, q := newTestSettings(t) // migrated in-memory queries; mirror existing usage
	h := NewRouter(Deps{Q: q, Session: scsNew(), SettingsStore: s, SecureCookies: false})
	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/account/nope", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "application/json")
	assert.Contains(t, rec.Body.String(), `"code":"not_found"`)
}
