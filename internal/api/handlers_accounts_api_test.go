package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/store"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
	"github.com/aalejandrofer/grubdrops/internal/web"
)

// seedAccount opens a fresh DB, inserts one account row, and returns the
// *gen.Queries and the account ID. Mirrors the seeding pattern in
// handlers_accounts_toggle_test.go.
func seedAccount(t *testing.T, ctx context.Context, enabled bool) (*gen.Queries, string) {
	t.Helper()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	q := gen.New(db)

	const id = "acc_api_seed"
	enabledVal := int64(0)
	if enabled {
		enabledVal = 1
	}
	_, err = db.ExecContext(ctx,
		`INSERT INTO accounts (id, platform, display_name, status, fingerprint_json, enabled, created_at, updated_at)
		VALUES (?, 'twitch', 'Test', 'idle', '{}', ?, 1, 1)`, id, enabledVal)
	require.NoError(t, err)
	return q, id
}

func TestDoToggleEnabledFlipsFlag(t *testing.T) {
	ctx := context.Background()
	q, id := seedAccount(t, ctx, true)

	d := accountsDeps{q: q}
	enabled, err := d.doToggleEnabled(ctx, id)
	require.NoError(t, err)
	assert.False(t, enabled, "was enabled -> now disabled")

	acc, err := q.GetAccount(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, int64(0), acc.Enabled)
}

func TestAPIToggle_RequiresCSRF(t *testing.T) {
	t.Setenv("GRUB_AUTHBYPASS", "true")
	s, q := newTestSettings(t)
	h := NewRouter(Deps{Q: q, Session: scs.New(), SettingsStore: s, SecureCookies: false})
	// POST without an X-CSRF-Token → nosurf 403 JSON (csrf), proving the route
	// is CSRF-protected.
	req := httptest.NewRequest(http.MethodPost, "/api/accounts/x/toggle", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusForbidden, rec.Code)
	assert.Contains(t, rec.Body.String(), `"code":"csrf"`)
}

func TestAPIAccounts_SafeProjectionNoSecrets(t *testing.T) {
	t.Setenv("GRUB_AUTHBYPASS", "true")
	ctx := context.Background()
	q, id := seedAccount(t, ctx, true)
	s, _ := newTestSettings(t)
	h := NewRouter(Deps{Q: q, Session: scsNew(), SettingsStore: s, SecureCookies: false, Location: time.UTC})
	req := httptest.NewRequest(http.MethodGet, "/api/accounts", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "application/json")
	body := rec.Body.String()
	assert.Contains(t, body, id)               // the account is present
	assert.NotContains(t, body, "webhook_url") // NO secret webhook
	assert.NotContains(t, body, "WebhookUrl")
	assert.NotContains(t, body, "fingerprint_json")
	assert.NotContains(t, body, "FingerprintJson")
	assert.NotContains(t, body, "proxy_url")
}

func TestAPICheckAuth_RequireCSRF(t *testing.T) {
	t.Setenv("GRUB_AUTHBYPASS", "true")
	s, q := newTestSettings(t)
	h := NewRouter(Deps{Q: q, Session: scsNew(), SettingsStore: s, SecureCookies: false})
	req := httptest.NewRequest(http.MethodPost, "/api/accounts/check-auth", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusForbidden, rec.Code)
	assert.Contains(t, rec.Body.String(), `"code":"csrf"`)
}

func TestAccountsRoute_SuppressedWhenSPAOn(t *testing.T) {
	t.Setenv("GRUB_AUTHBYPASS", "true")
	s, q := newTestSettings(t)
	h := NewRouter(Deps{Q: q, Session: scsNew(), SettingsStore: s, SecureCookies: false, SPADashboard: true})
	for _, p := range []string{"/accounts", "/settings/accounts"} {
		req := httptest.NewRequest(http.MethodGet, p, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		require.Equalf(t, http.StatusOK, rec.Code, "path %s", p)
		assert.Containsf(t, rec.Body.String(), `id="app"`, "path %s", p)
	}
}

// seedAccountInDB inserts an account row into the DB backing q and returns the
// account ID. Used by tests that need a pre-seeded account without creating a
// fresh DB (i.e. when the caller already has *gen.Queries from newTestSettings).
func seedAccountInDB(t *testing.T, q *gen.Queries, enabled bool) string {
	t.Helper()
	ctx := context.Background()
	const id = "acc_detail_seed"
	enabledVal := int64(0)
	if enabled {
		enabledVal = 1
	}
	_, err := q.CreateAccount(ctx, gen.CreateAccountParams{
		ID:              id,
		Platform:        "twitch",
		DisplayName:     "DetailTest",
		Status:          "idle",
		FingerprintJson: "{}",
		Enabled:         enabledVal,
		CreatedAt:       1,
		UpdatedAt:       1,
	})
	require.NoError(t, err)
	return id
}

func TestAPIAccountDetail_SafeAndNoProxyFingerprint(t *testing.T) {
	t.Setenv("GRUB_AUTHBYPASS", "true")
	s, q := newTestSettings(t)
	id := seedAccountInDB(t, q, true)
	h := NewRouter(Deps{Q: q, Session: scsNew(), SettingsStore: s, SecureCookies: false, Location: time.UTC})
	req := httptest.NewRequest(http.MethodGet, "/api/accounts/"+id, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, `"DisplayName"`)
	assert.NotContains(t, body, "proxy_url")
	assert.NotContains(t, body, "fingerprint_json")
	assert.NotContains(t, body, "FingerprintJson")
}

func TestAPIAccountDetail_Unknown404(t *testing.T) {
	t.Setenv("GRUB_AUTHBYPASS", "true")
	s, q := newTestSettings(t)
	h := NewRouter(Deps{Q: q, Session: scsNew(), SettingsStore: s, SecureCookies: false, Location: time.UTC})
	req := httptest.NewRequest(http.MethodGet, "/api/accounts/nope", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestAPIUpdateDelete_RequireCSRF(t *testing.T) {
	t.Setenv("GRUB_AUTHBYPASS", "true")
	s, q := newTestSettings(t)
	h := NewRouter(Deps{Q: q, Session: scsNew(), SettingsStore: s, SecureCookies: false, Reload: func(c context.Context) error { return nil }})
	for _, p := range []string{"/api/accounts/x/update", "/api/accounts/x/delete"} {
		req := httptest.NewRequest(http.MethodPost, p, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		require.Equalf(t, http.StatusForbidden, rec.Code, "path %s", p)
		assert.Containsf(t, rec.Body.String(), `"code":"csrf"`, "path %s", p)
	}
}

func TestAccountNewStaysLegacyWhenSPAOn(t *testing.T) {
	t.Setenv("GRUB_AUTHBYPASS", "true")
	s, q := newTestSettings(t)
	id := seedAccountInDB(t, q, true)
	tmpl, err := web.Templates()
	require.NoError(t, err)
	h := NewRouter(Deps{Q: q, Session: scsNew(), SettingsStore: s, SecureCookies: false, SPADashboard: true, Templates: tmpl})
	// /accounts/{id} -> SPA shell
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/accounts/"+id, nil))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `id="app"`)
	// /accounts/new -> legacy HTML (chi static precedence), NOT the SPA shell
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/accounts/new", nil))
	require.Equal(t, http.StatusOK, rec2.Code)
	assert.NotContains(t, rec2.Body.String(), `id="app"`) // legacy new-account HTML form
}
