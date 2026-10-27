package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/auth/oidc"
	"github.com/aalejandrofer/grubdrops/internal/store"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

type env struct {
	t   *testing.T
	srv *httptest.Server
	cl  *http.Client
	s   *server
}

func newEnv(t *testing.T) *env {
	t.Helper()
	return newEnvSPA(t, fstest.MapFS{"index.html": {Data: []byte("<html>spa</html>")}})
}

func newEnvSPA(t *testing.T, spa fs.FS) *env {
	t.Helper()
	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "t.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	sm := scs.New()
	sm.Store = NewKVSessionStore(db)
	sm.Lifetime = time.Hour
	s := newServer(Deps{
		DB: db, Q: gen.New(db), Session: sm, Version: "test",
		Hub: NewHub(),
		SPA: spa,
	})
	s.protected.Get("/overview", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 200, map[string]string{"ok": "1"}) })
	s.routesEvents(s.protected)
	srv := httptest.NewServer(s.handler())
	t.Cleanup(srv.Close)
	jar, _ := cookiejar.New(nil)
	return &env{t: t, srv: srv, cl: &http.Client{Jar: jar}, s: s}
}

func (e *env) do(method, path, csrf string, body any) (*http.Response, []byte) {
	e.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, e.srv.URL+path, rd)
	require.NoError(e.t, err)
	if csrf != "" {
		req.Header.Set("X-CSRF-Token", csrf)
	}
	resp, err := e.cl.Do(req)
	require.NoError(e.t, err)
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, b
}

func (e *env) session() (map[string]any, string) {
	e.t.Helper()
	resp, b := e.do("GET", "/api/session", "", nil)
	require.Equal(e.t, 200, resp.StatusCode)
	var m map[string]any
	require.NoError(e.t, json.Unmarshal(b, &m))
	tok, _ := m["csrf_token"].(string)
	return m, tok
}

func TestSPA_Fallback(t *testing.T) {
	e := newEnv(t)
	resp, b := e.do("GET", "/games", "", nil)
	assert.Equal(t, 200, resp.StatusCode)
	assert.Contains(t, string(b), "spa")
	resp, b = e.do("GET", "/", "", nil)
	assert.Equal(t, 200, resp.StatusCode)
	assert.Contains(t, string(b), "spa")

	resp, b = e.do("GET", "/api/nope", "", nil)
	assert.Equal(t, 404, resp.StatusCode)
	assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))
	assert.JSONEq(t, `{"error":"not_found"}`, string(b))

	resp, _ = e.do("GET", "/healthz", "", nil)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestReadJSON_TooLarge(t *testing.T) {
	big := `{"password":"` + strings.Repeat("a", 2<<20) + `"}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/x", strings.NewReader(big))
	var v struct {
		Password string `json:"password"`
	}
	assert.False(t, readJSON(w, r, &v))
	assert.Equal(t, 400, w.Code)
	assert.JSONEq(t, `{"error":"bad_request"}`, w.Body.String())

	w = httptest.NewRecorder()
	r = httptest.NewRequest("POST", "/x", strings.NewReader(`{"nope":1}`))
	assert.False(t, readJSON(w, r, &v))
	assert.Equal(t, 400, w.Code)
}

// fakeOIDC returns an enabled provider backed by a discovery-only IdP.
func fakeOIDC(t *testing.T) *oidc.Provider {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/openid-configuration" {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, 200, map[string]any{
			"issuer":                 srv.URL,
			"authorization_endpoint": srv.URL + "/auth",
			"token_endpoint":         srv.URL + "/token",
			"jwks_uri":               srv.URL + "/jwks",
		})
	}))
	t.Cleanup(srv.Close)
	p, err := oidc.New(context.Background(), oidc.Config{
		Issuer: srv.URL, ClientID: "id", ClientSecret: "secret", RedirectURL: "http://localhost/auth/oidc/callback",
	})
	require.NoError(t, err)
	require.True(t, p.Enabled())
	return p
}

// adminErrDB makes only the AdminExists query fail.
type adminErrDB struct{ *sql.DB }

func (d adminErrDB) QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row {
	if strings.Contains(q, "FROM admin WHERE id = 1)") {
		return d.DB.QueryRowContext(ctx, "SELECT x FROM no_such_table")
	}
	return d.DB.QueryRowContext(ctx, q, args...)
}

func TestSPA_NotBuilt(t *testing.T) {
	e := newEnvSPA(t, fstest.MapFS{".gitkeep": {}})
	for _, p := range []string{"/", "/games"} {
		resp, b := e.do("GET", p, "", nil)
		assert.Equal(t, 503, resp.StatusCode, p)
		assert.Equal(t, "text/html; charset=utf-8", resp.Header.Get("Content-Type"))
		assert.Equal(t, "no-store", resp.Header.Get("Cache-Control"))
		assert.Contains(t, string(b), "UI not built, run <code>npm --prefix web run build</code>")
	}
	resp, b := e.do("GET", "/api/nope", "", nil)
	assert.Equal(t, 404, resp.StatusCode)
	assert.JSONEq(t, `{"error":"not_found"}`, string(b))
}

func TestSPA_CacheHeaders(t *testing.T) {
	e := newEnvSPA(t, fstest.MapFS{
		"index.html":                         {Data: []byte("<html>spa</html>")},
		"_app/immutable/entry/app.abc123.js": {Data: []byte("export {}")},
	})
	for _, p := range []string{"/", "/inventory"} {
		resp, _ := e.do("GET", p, "", nil)
		assert.Equal(t, 200, resp.StatusCode, p)
		assert.Equal(t, "no-cache", resp.Header.Get("Cache-Control"), p)
	}
	resp, _ := e.do("GET", "/_app/immutable/entry/app.abc123.js", "", nil)
	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, "public, max-age=31536000, immutable", resp.Header.Get("Cache-Control"))
	assert.True(t, strings.HasPrefix(resp.Header.Get("Content-Type"), "text/javascript"), resp.Header.Get("Content-Type"))
}

func TestRewardsRedirectsToInventory(t *testing.T) {
	e := newEnvSPA(t, fstest.MapFS{"index.html": {Data: []byte("<html>spa</html>")}})
	cl := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := cl.Get(e.srv.URL + "/rewards")
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusMovedPermanently, resp.StatusCode)
	assert.Equal(t, "/inventory", resp.Header.Get("Location"))
}

func TestSPA_MissingAssetIs404(t *testing.T) {
	e := newEnv(t)
	resp, b := e.do("GET", "/_app/immutable/chunks/gone.js", "", nil)
	assert.Equal(t, 404, resp.StatusCode)
	assert.NotContains(t, string(b), "spa")
}
