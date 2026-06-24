package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAPIGeneral_RequireCSRF(t *testing.T) {
	t.Setenv("GRUB_AUTHBYPASS", "true")
	s, q := newTestSettings(t)
	h := NewRouter(Deps{Q: q, Session: scsNew(), SettingsStore: s, SecureCookies: false})
	req := httptest.NewRequest(http.MethodPost, "/api/settings/general", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusForbidden, rec.Code)
	assert.Contains(t, rec.Body.String(), `"code":"csrf"`)
}

func TestDoSaveGeneral_PersistsAndDetectsIntervalChange(t *testing.T) {
	s, _ := newTestSettings(t)
	d := &settingsDeps{s: s}
	changed, err := d.doSaveGeneral(context.Background(), 7, "debug", 90, 5)
	require.NoError(t, err)
	assert.True(t, changed) // tick/disc differ from defaults
	got, _ := s.TickIntervalSec(context.Background())
	assert.Equal(t, 90, got)
	lvl, _ := s.LogLevel(context.Background())
	assert.Equal(t, "debug", lvl)
}

func TestSettingsRoute_SuppressedWhenSPAOn(t *testing.T) {
	t.Setenv("GRUB_AUTHBYPASS", "true")
	s, q := newTestSettings(t)
	h := NewRouter(Deps{Q: q, Session: scsNew(), SettingsStore: s, SecureCookies: false, SPADashboard: true})
	req := httptest.NewRequest(http.MethodGet, "/settings", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `id="app"`)
}

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

func TestAPISettingsTabs_RequireCSRF(t *testing.T) {
	t.Setenv("GRUB_AUTHBYPASS", "true")
	s, q := newTestSettings(t)
	h := NewRouter(Deps{Q: q, Session: scsNew(), SettingsStore: s, SecureCookies: false})
	for _, p := range []string{"/api/settings/notifications", "/api/settings/experimental", "/api/settings/proxy", "/api/settings/notify-test", "/api/settings/proxy/test"} {
		req := httptest.NewRequest(http.MethodPost, p, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		require.Equalf(t, http.StatusForbidden, rec.Code, "path %s", p)
		assert.Containsf(t, rec.Body.String(), `"code":"csrf"`, "path %s", p)
	}
}

func TestDoSaveProxy_Persists(t *testing.T) {
	s, _ := newTestSettings(t)
	d := &settingsDeps{s: s}
	require.NoError(t, d.doSaveProxy(context.Background(), true, "socks5://127.0.0.1:1080"))
	url, _ := s.ProxyURL(context.Background())
	assert.Equal(t, "socks5://127.0.0.1:1080", url)
	en, _ := s.ProxyEnabled(context.Background())
	assert.True(t, en)
}

func TestExperimentalTabRoute_SuppressedWhenSPAOn(t *testing.T) {
	t.Setenv("GRUB_AUTHBYPASS", "true")
	s, q := newTestSettings(t)
	h := NewRouter(Deps{Q: q, Session: scsNew(), SettingsStore: s, SecureCookies: false, SPADashboard: true})
	req := httptest.NewRequest(http.MethodGet, "/settings/experimental", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `id="app"`)
}

func TestAPISecurityHealth_RequireCSRF(t *testing.T) {
	t.Setenv("GRUB_AUTHBYPASS", "true")
	s, q := newTestSettings(t)
	h := NewRouter(Deps{Q: q, Session: scsNew(), SettingsStore: s, SecureCookies: false, RunCanary: func(c context.Context) error { return nil }})
	for _, p := range []string{"/api/settings/password", "/api/settings/canary", "/api/settings/canary/run"} {
		req := httptest.NewRequest(http.MethodPost, p, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		require.Equalf(t, http.StatusForbidden, rec.Code, "path %s", p)
		assert.Containsf(t, rec.Body.String(), `"code":"csrf"`, "path %s", p)
	}
}

func TestDoSaveCanary_Persists(t *testing.T) {
	s, _ := newTestSettings(t)
	d := &settingsDeps{s: s}
	require.NoError(t, d.doSaveCanary(context.Background(), "alveussanctuary", "xqc", 7200))
	tw, _ := s.CanaryTwitchChannel(context.Background())
	assert.Equal(t, "alveussanctuary", tw)
	iv, _ := s.CanaryIntervalSec(context.Background())
	assert.Equal(t, 7200, iv)
}

// TestAPICanaryRun_DetachesAndReturnsImmediately verifies that apiCanaryRun
// returns 200 {ok:true} without blocking on the probe (the runner is invoked
// asynchronously in a background goroutine).
func TestAPICanaryRun_DetachesAndReturnsImmediately(t *testing.T) {
	invoked := make(chan struct{}, 1)
	d := &settingsDeps{
		runCanary: func(ctx context.Context) error {
			invoked <- struct{}{}
			return nil
		},
	}
	req := httptest.NewRequest(http.MethodPost, "/api/settings/canary/run", nil)
	rec := httptest.NewRecorder()
	d.apiCanaryRun(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"ok":true`)
	// Runner must be called asynchronously within a short window.
	select {
	case <-invoked:
	case <-time.After(2 * time.Second):
		t.Fatal("canary runner was not invoked within 2s")
	}
}

// TestAPICanaryRun_NilRunner returns ok:false without panicking.
func TestAPICanaryRun_NilRunner(t *testing.T) {
	d := &settingsDeps{}
	req := httptest.NewRequest(http.MethodPost, "/api/settings/canary/run", nil)
	rec := httptest.NewRecorder()
	d.apiCanaryRun(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"ok":false`)
	assert.Contains(t, rec.Body.String(), "canary runner unavailable")
}

func TestSecurityHealthRoutes_SuppressedWhenSPAOn(t *testing.T) {
	t.Setenv("GRUB_AUTHBYPASS", "true")
	s, q := newTestSettings(t)
	h := NewRouter(Deps{Q: q, Session: scsNew(), SettingsStore: s, SecureCookies: false, SPADashboard: true})
	for _, p := range []string{"/settings/security", "/settings/health"} {
		req := httptest.NewRequest(http.MethodGet, p, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		require.Equalf(t, http.StatusOK, rec.Code, "path %s", p)
		assert.Containsf(t, rec.Body.String(), `id="app"`, "path %s", p)
	}
}
