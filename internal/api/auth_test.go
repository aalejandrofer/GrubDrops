package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

func (e *env) setup(pw string) {
	e.t.Helper()
	_, tok := e.session()
	resp, _ := e.do("POST", "/api/setup", tok, map[string]string{"password": pw, "confirm": pw})
	require.Equal(e.t, 204, resp.StatusCode)
}

func TestSession_SetupNeeded(t *testing.T) {
	e := newEnv(t)
	m, tok := e.session()
	assert.Equal(t, true, m["setup_needed"])
	assert.Equal(t, false, m["authed"])
	assert.NotEmpty(t, tok)
	assert.Equal(t, "test", m["version"])
	assert.Equal(t, false, m["oidc"].(map[string]any)["enabled"])
}

func TestSetup_CreatesAdminAndLogsIn(t *testing.T) {
	e := newEnv(t)
	e.setup("password123")
	m, tok := e.session()
	assert.Equal(t, true, m["authed"])
	assert.Equal(t, false, m["setup_needed"])

	resp, b := e.do("POST", "/api/setup", tok, map[string]string{"password": "password123", "confirm": "password123"})
	assert.Equal(t, 409, resp.StatusCode)
	assert.JSONEq(t, `{"error":"conflict"}`, string(b))
}

func TestSetup_Rejects(t *testing.T) {
	e := newEnv(t)
	_, tok := e.session()
	resp, _ := e.do("POST", "/api/setup", tok, map[string]string{"password": "password123", "confirm": "different123"})
	assert.Equal(t, 400, resp.StatusCode)
	resp, _ = e.do("POST", "/api/setup", tok, map[string]string{"password": "short", "confirm": "short"})
	assert.Equal(t, 400, resp.StatusCode)
}

func TestLogin_WrongPassword401(t *testing.T) {
	e := newEnv(t)
	e.setup("password123")
	_, tok := e.session()
	resp, _ := e.do("POST", "/api/logout", tok, nil)
	require.Equal(t, 204, resp.StatusCode)
	_, tok = e.session()
	resp, b := e.do("POST", "/api/login", tok, map[string]string{"password": "nope"})
	assert.Equal(t, 401, resp.StatusCode)
	assert.JSONEq(t, `{"error":"unauthorized"}`, string(b))
}

func TestLogin_OK(t *testing.T) {
	e := newEnv(t)
	e.setup("password123")
	_, tok := e.session()
	resp, _ := e.do("POST", "/api/logout", tok, nil)
	require.Equal(t, 204, resp.StatusCode)
	m, tok := e.session()
	require.Equal(t, false, m["authed"])
	resp, _ = e.do("POST", "/api/login", tok, map[string]string{"password": "password123"})
	assert.Equal(t, 204, resp.StatusCode)
	m, _ = e.session()
	assert.Equal(t, true, m["authed"])
	resp, _ = e.do("GET", "/api/overview", "", nil)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestRequireAdmin_JSON401(t *testing.T) {
	e := newEnv(t)
	resp, b := e.do("GET", "/api/overview", "", nil)
	assert.Equal(t, 401, resp.StatusCode)
	assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))
	assert.JSONEq(t, `{"error":"unauthorized"}`, string(b))
}

func TestCSRF_MissingHeader403(t *testing.T) {
	e := newEnv(t)
	e.setup("password123")
	resp, b := e.do("POST", "/api/logout", "", nil)
	assert.Equal(t, 403, resp.StatusCode)
	assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))
	assert.JSONEq(t, `{"error":"csrf"}`, string(b))
	m, _ := e.session()
	assert.Equal(t, true, m["authed"])
}

func TestAuthBypassLocal(t *testing.T) {
	t.Setenv("GRUB_AUTH_BYPASS_LOCAL", "1")
	e := newEnv(t)
	req := httptest.NewRequest("GET", "/api/overview", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	w := httptest.NewRecorder()
	e.s.handler().ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	req = httptest.NewRequest("GET", "/api/overview", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("X-Forwarded-For", "8.8.8.8")
	w = httptest.NewRecorder()
	e.s.handler().ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAuthBypassAll(t *testing.T) {
	t.Setenv("GRUB_AUTHBYPASS", "true")
	e := newEnv(t)
	resp, _ := e.do("GET", "/api/overview", "", nil)
	assert.Equal(t, 200, resp.StatusCode)
	m, _ := e.session()
	assert.Equal(t, true, m["authed"])
}

func TestCSRF_CrossSiteOrigin403(t *testing.T) {
	e := newEnv(t)
	_, tok := e.session()
	req, _ := http.NewRequest("POST", e.srv.URL+"/api/login", strings.NewReader(`{"password":"x"}`))
	req.Header.Set("X-CSRF-Token", tok)
	req.Header.Set("Origin", "https://evil.example")
	resp, err := e.cl.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, 403, resp.StatusCode)
	b, _ := io.ReadAll(resp.Body)
	assert.JSONEq(t, `{"error":"csrf"}`, string(b))
}

func TestOIDC_DisabledRedirectsHome(t *testing.T) {
	e := newEnv(t)
	cl := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for _, p := range []string{"/auth/oidc/login", "/auth/oidc/callback"} {
		resp, err := cl.Get(e.srv.URL + p)
		require.NoError(t, err)
		resp.Body.Close()
		assert.Equal(t, http.StatusSeeOther, resp.StatusCode, p)
		assert.Equal(t, "/", resp.Header.Get("Location"), p)
	}
}

func TestOIDC_CallbackStateMismatch(t *testing.T) {
	e := newEnv(t)
	p := fakeOIDC(t)
	e.s.d.OIDC = p
	cl := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, _ := http.NewRequest("GET", e.srv.URL+"/auth/oidc/callback?state=b&code=x", nil)
	req.AddCookie(&http.Cookie{Name: oidcStateCookie, Value: "a"})
	resp, err := cl.Do(req)
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusSeeOther, resp.StatusCode)
	assert.Equal(t, "/login?login_error=oidc", resp.Header.Get("Location"))
}

func TestSetup_AdminLookupErrorFailsClosed(t *testing.T) {
	e := newEnv(t)
	_, tok := e.session()
	e.s.d.Q = gen.New(adminErrDB{e.s.d.DB})
	resp, _ := e.do("POST", "/api/setup", tok, map[string]string{"password": "password123", "confirm": "password123"})
	assert.Equal(t, 500, resp.StatusCode)
}
