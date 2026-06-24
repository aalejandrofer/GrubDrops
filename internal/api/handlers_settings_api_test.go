package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAPISettings_ReturnsView(t *testing.T) {
	t.Setenv("GRUB_AUTHBYPASS", "true")
	s, q := newTestSettings(t)
	h := NewRouter(Deps{Q: q, Session: scsNew(), SettingsStore: s, SecureCookies: false})
	req := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "application/json")
	assert.Contains(t, rec.Body.String(), `"PriorityMode"`)
	assert.Contains(t, rec.Body.String(), `"GlobalGames"`)
}

func TestAPIPriorityMutations_RequireCSRF(t *testing.T) {
	t.Setenv("GRUB_AUTHBYPASS", "true")
	s, q := newTestSettings(t)
	h := NewRouter(Deps{Q: q, Session: scsNew(), SettingsStore: s, SecureCookies: false,
		Reload: func(c context.Context) error { return nil }})
	for _, p := range []string{"/api/settings/global-games", "/api/settings/global-games/add", "/api/settings/priority-mode"} {
		req := httptest.NewRequest(http.MethodPost, p, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		require.Equalf(t, http.StatusForbidden, rec.Code, "path %s", p)
		assert.Containsf(t, rec.Body.String(), `"code":"csrf"`, "path %s", p)
	}
}

func TestPriorityRoute_SuppressedWhenSPAOn(t *testing.T) {
	t.Setenv("GRUB_AUTHBYPASS", "true")
	s, q := newTestSettings(t)
	h := NewRouter(Deps{Q: q, Session: scsNew(), SettingsStore: s, SecureCookies: false, SPADashboard: true})
	req := httptest.NewRequest(http.MethodGet, "/priority", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `id="app"`)
}

func TestAPIGlobalGamesOrder_MalformedBodyIs400(t *testing.T) {
	d := &settingsDeps{}
	req := httptest.NewRequest(http.MethodPost, "/api/settings/global-games", strings.NewReader("not json"))
	rec := httptest.NewRecorder()
	d.apiGlobalGamesOrder(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), `"code":"bad_request"`)
}
