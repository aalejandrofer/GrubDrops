package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/auth/oidc"
	"github.com/aalejandrofer/grubdrops/internal/store"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

// ssoProvider is fakeOIDC with an allowlist, served under an issuer path.
func ssoProvider(t *testing.T, emails, groups []string) *oidc.Provider {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/application/o/grub/.well-known/openid-configuration" {
			http.NotFound(w, r)
			return
		}
		iss := srv.URL + "/application/o/grub"
		writeJSON(w, 200, map[string]any{
			"issuer":                 iss,
			"authorization_endpoint": iss + "/auth",
			"token_endpoint":         iss + "/token",
			"jwks_uri":               iss + "/jwks",
		})
	}))
	t.Cleanup(srv.Close)
	p, err := oidc.New(context.Background(), oidc.Config{
		Issuer: srv.URL + "/application/o/grub", ClientID: "client-id-value", ClientSecret: "client-secret-value",
		RedirectURL: "http://localhost/auth/oidc/callback", ProviderName: "authentik",
		AllowedEmails: emails, AllowedGroups: groups,
	})
	require.NoError(t, err)
	require.True(t, p.Enabled())
	return p
}

func getSettingsRaw(t *testing.T, p *oidc.Provider) []byte {
	t.Helper()
	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "t.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	s := &server{d: Deps{Settings: store.NewSettings(gen.New(db))}}
	if p != nil {
		s.d.OIDC = p
	}
	r := chi.NewRouter()
	s.routesSettings(r)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/settings", nil))
	require.Equal(t, 200, w.Code)
	return w.Body.Bytes()
}

func TestSettings_SSOSummary(t *testing.T) {
	p := ssoProvider(t, []string{"a@example.com", "b@example.com"}, []string{"grubdrops-users"})
	b := getSettingsRaw(t, p)

	var got settingsBody
	require.NoError(t, json.Unmarshal(b, &got))
	assert.True(t, got.SSO.Enabled)
	assert.Equal(t, "authentik", got.SSO.Name)
	assert.Equal(t, "127.0.0.1", got.SSO.IssuerHost, "host only: no scheme, port or path")
	assert.Equal(t, 2, got.SSO.AllowedEmails)
	assert.Equal(t, 1, got.SSO.AllowedGroups)

	// Nothing secret or personal leaves the server.
	for _, leak := range []string{"client-id-value", "client-secret-value", "a@example.com", "b@example.com", "grubdrops-users", "/application/o/grub"} {
		assert.NotContains(t, string(b), leak)
	}
}

func TestSettings_SSOSummaryOff(t *testing.T) {
	var got settingsBody
	require.NoError(t, json.Unmarshal(getSettingsRaw(t, nil), &got))
	assert.Equal(t, settingsSSO{}, got.SSO)
}
