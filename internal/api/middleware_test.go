package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alexedwards/scs/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
}

func TestRequireAdminAPI_Unauthenticated401JSON(t *testing.T) {
	sm := scs.New()
	h := sm.LoadAndSave(RequireAdminAPI(sm)(okHandler()))
	req := httptest.NewRequest(http.MethodGet, "/api/dashboard", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "application/json")
	assert.Contains(t, rec.Body.String(), `"code":"unauthorized"`)
}

func TestRequireAdminAPI_AuthenticatedPasses(t *testing.T) {
	sm := scs.New()
	// Seed an authenticated session by running a request that sets the flag,
	// then replay its session cookie.
	seed := sm.LoadAndSave(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sm.Put(r.Context(), "admin_authed", true)
		w.WriteHeader(http.StatusOK)
	}))
	rec0 := httptest.NewRecorder()
	seed.ServeHTTP(rec0, httptest.NewRequest(http.MethodGet, "/seed", nil))
	cookie := rec0.Result().Cookies()[0]

	h := sm.LoadAndSave(RequireAdminAPI(sm)(okHandler()))
	req := httptest.NewRequest(http.MethodGet, "/api/dashboard", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "ok", rec.Body.String())
}

func TestRequireAdminAPI_BypassAll(t *testing.T) {
	t.Setenv("GRUB_AUTHBYPASS", "true")
	sm := scs.New()
	h := sm.LoadAndSave(RequireAdminAPI(sm)(okHandler()))
	req := httptest.NewRequest(http.MethodGet, "/api/dashboard", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
}
