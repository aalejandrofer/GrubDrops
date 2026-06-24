package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAPIDropsMutations_RequireCSRF(t *testing.T) {
	t.Setenv("GRUB_AUTHBYPASS", "true")
	s, q := newTestSettings(t)
	h := NewRouter(Deps{Q: q, Session: scsNew(), SettingsStore: s, SecureCookies: false,
		Reload: func(_ context.Context) error { return nil }})
	for _, p := range []string{
		"/api/drops/whitelist/add", "/api/drops/whitelist/channel",
		"/api/drops/whitelist/channel/remove", "/api/drops/link",
	} {
		req := httptest.NewRequest(http.MethodPost, p, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		require.Equalf(t, http.StatusForbidden, rec.Code, "path %s", p)
		assert.Containsf(t, rec.Body.String(), `"code":"csrf"`, "path %s", p)
	}
}

func TestDropsPageJSONOmitsCSRFToken(t *testing.T) {
	t.Setenv("GRUB_AUTHBYPASS", "true")
	s, q := newTestSettings(t)
	h := NewRouter(Deps{Q: q, Session: scsNew(), SettingsStore: s, SecureCookies: false, Location: time.UTC})
	req := httptest.NewRequest(http.MethodGet, "/api/drops?tab=current", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.NotContains(t, rec.Body.String(), `"CSRFToken"`)
}
