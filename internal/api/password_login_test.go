package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/store"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

// withSettings gives the env a settings store on its own DB.
func (e *env) withSettings() *store.Settings {
	e.t.Helper()
	st := store.NewSettings(gen.New(e.s.d.DB))
	e.s.d.Settings = st
	return st
}

func (e *env) logout() string {
	e.t.Helper()
	_, tok := e.session()
	resp, _ := e.do("POST", "/api/logout", tok, nil)
	require.Equal(e.t, 204, resp.StatusCode)
	_, tok = e.session()
	return tok
}

func TestPasswordLogin_OnByDefault(t *testing.T) {
	e := newEnv(t)
	e.withSettings()
	e.s.d.OIDC = fakeOIDC(t)
	m, _ := e.session()
	assert.Equal(t, true, m["password_login"])
	assert.Equal(t, true, m["oidc"].(map[string]any)["enabled"])
}

func TestPasswordLogin_DisabledWithSSORefuses(t *testing.T) {
	e := newEnv(t)
	st := e.withSettings()
	e.s.d.OIDC = fakeOIDC(t)
	require.NoError(t, st.SetPasswordLoginDisabled(context.Background(), true))

	// First-run setup still works with the setting on.
	e.setup("password123")
	tok := e.logout()

	m, _ := e.session()
	assert.Equal(t, false, m["password_login"])
	resp, b := e.do("POST", "/api/login", tok, map[string]string{"password": "password123"})
	assert.Equal(t, 403, resp.StatusCode)
	assert.JSONEq(t, `{"error":"password_login_disabled"}`, string(b))
	m, _ = e.session()
	assert.Equal(t, false, m["authed"])
}

func TestPasswordLogin_SettingIgnoredWithoutSSO(t *testing.T) {
	e := newEnv(t)
	st := e.withSettings()
	require.NoError(t, st.SetPasswordLoginDisabled(context.Background(), true))
	e.setup("password123")
	tok := e.logout()

	// SSO is not configured, so the password still works.
	m, _ := e.session()
	assert.Equal(t, true, m["password_login"])
	resp, _ := e.do("POST", "/api/login", tok, map[string]string{"password": "password123"})
	assert.Equal(t, 204, resp.StatusCode)
	m, _ = e.session()
	assert.Equal(t, true, m["authed"])
}

func newPasswordSettingsEnv(t *testing.T, sso bool) (do func(method string, body any) (int, []byte), st *store.Settings) {
	t.Helper()
	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "t.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	st = store.NewSettings(gen.New(db))
	s := &server{d: Deps{Settings: st}}
	if sso {
		s.d.OIDC = fakeOIDC(t)
	}
	r := chi.NewRouter()
	s.routesSettings(r)
	return func(method string, body any) (int, []byte) {
		var rd *bytes.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			rd = bytes.NewReader(b)
		} else {
			rd = bytes.NewReader(nil)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(method, "/settings", rd))
		return w.Code, w.Body.Bytes()
	}, st
}

func TestSettings_PasswordLoginNeedsSSO(t *testing.T) {
	do, st := newPasswordSettingsEnv(t, false)
	code, b := do("GET", nil)
	require.Equal(t, 200, code)
	var got settingsBody
	require.NoError(t, json.Unmarshal(b, &got))
	assert.False(t, got.SSO.Enabled)
	assert.False(t, got.PasswordLoginDisabled)

	code, b = do("PUT", map[string]any{"password_login_disabled": true})
	assert.Equal(t, 409, code)
	assert.JSONEq(t, `{"error":"sso_not_configured"}`, string(b))
	off, err := st.PasswordLoginDisabled(context.Background())
	require.NoError(t, err)
	assert.False(t, off, "a rejected PUT saves nothing")

	// Turning it back on works without SSO, for the host-side recovery.
	require.NoError(t, st.SetPasswordLoginDisabled(context.Background(), true))
	code, _ = do("PUT", map[string]any{"password_login_disabled": false})
	assert.Equal(t, 200, code)
	off, err = st.PasswordLoginDisabled(context.Background())
	require.NoError(t, err)
	assert.False(t, off)
}

func TestSettings_PasswordLoginWithSSO(t *testing.T) {
	do, st := newPasswordSettingsEnv(t, true)
	code, b := do("PUT", map[string]any{"password_login_disabled": true})
	require.Equal(t, 200, code, string(b))
	var got settingsBody
	require.NoError(t, json.Unmarshal(b, &got))
	assert.True(t, got.SSO.Enabled)
	assert.True(t, got.PasswordLoginDisabled)
	off, err := st.PasswordLoginDisabled(context.Background())
	require.NoError(t, err)
	assert.True(t, off)

	// The GET body, sso included, can be sent back as is.
	code, b = do("GET", nil)
	require.Equal(t, 200, code)
	code, _ = do("PUT", json.RawMessage(b))
	assert.Equal(t, 200, code)
}
