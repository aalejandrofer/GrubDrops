package api

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/alexedwards/scs/v2"
	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"github.com/aalejandrofer/grubdrops/internal/auth/oidc"
)

// oidcTestEnv bundles all shared infrastructure for OIDC handler tests.
type oidcTestEnv struct {
	db     *sql.DB
	sm     *scs.SessionManager
	od     *oidcDeps
	appMux *http.ServeMux
	app    *httptest.Server
}

// newOIDCTestEnv creates a fresh in-memory DB, session manager, and app mux.
// The caller is responsible for setting od.p (the OIDC provider) and calling
// app = httptest.NewServer(sm.LoadAndSave(appMux)) after wiring the provider.
func newOIDCTestEnv(t *testing.T) *oidcTestEnv {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	_, err = db.Exec(`CREATE TABLE kv (key TEXT PRIMARY KEY, value BLOB)`)
	require.NoError(t, err)

	sm := scs.New()
	sm.Store = NewKVSessionStore(db)

	od := &oidcDeps{hs: oidc.NewHandshakeStore(db), sm: sm}

	appMux := http.NewServeMux()
	appMux.HandleFunc("/auth/oidc/login", func(w http.ResponseWriter, r *http.Request) { od.loginRedirect(w, r) })
	appMux.HandleFunc("/auth/oidc/callback", func(w http.ResponseWriter, r *http.Request) { od.callback(w, r) })
	appMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if sm.GetBool(r.Context(), "admin_authed") {
			_, _ = w.Write([]byte("authed"))
			return
		}
		_, _ = w.Write([]byte("anon"))
	})

	return &oidcTestEnv{db: db, sm: sm, od: od, appMux: appMux}
}

// newFakeIdP builds a minimal fake IdP server that signs tokens with key.
// idpURL is a pointer that will be filled after the server starts.
func newFakeIdP(t *testing.T, key *rsa.PrivateKey, idpURL *string, capturedNonce *string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer": *idpURL, "authorization_endpoint": *idpURL + "/authorize",
			"token_endpoint": *idpURL + "/token", "jwks_uri": *idpURL + "/jwks",
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{
			{Key: key.Public(), KeyID: "k", Algorithm: "RS256", Use: "sig"},
		}})
	})
	mux.HandleFunc("/authorize", func(w http.ResponseWriter, r *http.Request) {
		if capturedNonce != nil {
			*capturedNonce = r.URL.Query().Get("nonce")
		}
		redirect := r.URL.Query().Get("redirect_uri") + "?state=" +
			url.QueryEscape(r.URL.Query().Get("state")) + "&code=authcode"
		http.Redirect(w, r, redirect, http.StatusSeeOther)
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		nonce := ""
		if capturedNonce != nil {
			nonce = *capturedNonce
		}
		signer, _ := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key},
			(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "k"))
		raw, _ := jwt.Signed(signer).Claims(map[string]any{
			"iss": *idpURL, "sub": "sub1", "aud": "client", "email": "admin@example.com",
			"exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(),
			"nonce": nonce,
		}).Serialize()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "a", "token_type": "Bearer", "id_token": raw,
		})
	})
	return httptest.NewServer(mux)
}

func TestOIDCCallback_HappyPath(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	var idpURL string
	var capturedNonce string

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer": idpURL, "authorization_endpoint": idpURL + "/authorize",
			"token_endpoint": idpURL + "/token", "jwks_uri": idpURL + "/jwks",
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{
			{Key: key.Public(), KeyID: "k", Algorithm: "RS256", Use: "sig"},
		}})
	})
	mux.HandleFunc("/authorize", func(w http.ResponseWriter, r *http.Request) {
		capturedNonce = r.URL.Query().Get("nonce")
		redirect := r.URL.Query().Get("redirect_uri") + "?state=" +
			url.QueryEscape(r.URL.Query().Get("state")) + "&code=authcode"
		http.Redirect(w, r, redirect, http.StatusSeeOther)
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		signer, _ := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key},
			(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "k"))
		raw, _ := jwt.Signed(signer).Claims(map[string]any{
			"iss": idpURL, "sub": "sub1", "aud": "client", "email": "admin@example.com",
			"exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(),
			"nonce": capturedNonce,
		}).Serialize()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "a", "token_type": "Bearer", "id_token": raw,
		})
	})
	idp := httptest.NewServer(mux)
	defer idp.Close()
	idpURL = idp.URL

	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE kv (key TEXT PRIMARY KEY, value BLOB)`)
	require.NoError(t, err)

	sm := scs.New()
	sm.Store = NewKVSessionStore(db)

	var od oidcDeps
	appMux := http.NewServeMux()
	appMux.HandleFunc("/auth/oidc/login", func(w http.ResponseWriter, r *http.Request) { od.loginRedirect(w, r) })
	appMux.HandleFunc("/auth/oidc/callback", func(w http.ResponseWriter, r *http.Request) { od.callback(w, r) })
	appMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if sm.GetBool(r.Context(), "admin_authed") {
			_, _ = w.Write([]byte("authed"))
			return
		}
		_, _ = w.Write([]byte("anon"))
	})
	app := httptest.NewServer(sm.LoadAndSave(appMux))
	defer app.Close()

	prov, err := oidc.New(context.Background(), oidc.Config{
		Issuer: idpURL, ClientID: "client", ClientSecret: "secret",
		RedirectURL: app.URL + "/auth/oidc/callback",
	})
	require.NoError(t, err)
	od = oidcDeps{p: prov, hs: oidc.NewHandshakeStore(db), sm: sm}

	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	client := &http.Client{Jar: jar}

	resp, err := client.Get(app.URL + "/auth/oidc/login")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, "authed", string(body))
}

func TestOIDCCallback_StateCookieMismatch(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	var idpURL string
	idp := newFakeIdP(t, key, &idpURL, nil)
	defer idp.Close()
	idpURL = idp.URL

	env := newOIDCTestEnv(t)

	prov, err := oidc.New(context.Background(), oidc.Config{
		Issuer: idpURL, ClientID: "client", ClientSecret: "secret",
		RedirectURL: "http://localhost/auth/oidc/callback",
	})
	require.NoError(t, err)
	env.od.p = prov
	env.od.secure = false

	app := httptest.NewServer(env.sm.LoadAndSave(env.appMux))
	defer app.Close()

	// Build a callback request with state=X but no matching cookie (or wrong value).
	// We send the request without a cookie jar so no grub_oidc_state cookie is present.
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse // stop at first redirect
		},
	}
	resp, err := client.Get(app.URL + "/auth/oidc/callback?state=somestate&code=authcode")
	require.NoError(t, err)
	defer resp.Body.Close()

	// Should redirect to /login (303).
	require.Equal(t, http.StatusSeeOther, resp.StatusCode)
	require.Equal(t, "/login", resp.Header.Get("Location"))

	// Confirm no admin_authed was set by hitting / in a fresh session.
	resp2, err := client.Get(app.URL + "/")
	require.NoError(t, err)
	defer resp2.Body.Close()
	body, err := io.ReadAll(resp2.Body)
	require.NoError(t, err)
	require.Equal(t, "anon", string(body))
}

func TestOIDCCallback_AuthorizeRejected(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	var idpURL string
	var capturedNonce string

	env := newOIDCTestEnv(t)
	idp := newFakeIdP(t, key, &idpURL, &capturedNonce)
	defer idp.Close()
	idpURL = idp.URL

	// Wire a real app server first so we have the redirect URL.
	app := httptest.NewServer(env.sm.LoadAndSave(env.appMux))
	defer app.Close()

	// AllowedEmails does NOT include the token's email (admin@example.com).
	prov, err := oidc.New(context.Background(), oidc.Config{
		Issuer: idpURL, ClientID: "client", ClientSecret: "secret",
		RedirectURL:   app.URL + "/auth/oidc/callback",
		AllowedEmails: []string{"someone-else@example.com"},
	})
	require.NoError(t, err)
	env.od.p = prov
	env.od.secure = false

	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	client := &http.Client{Jar: jar}

	// Drive the full login flow; it should reach Authorize and be rejected.
	resp, err := client.Get(app.URL + "/auth/oidc/login")
	require.NoError(t, err)
	defer resp.Body.Close()

	// Final response should be "anon" (account not allowed, redirected to /login,
	// then the / handler renders anon for an unauthenticated session).
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NotEqual(t, "authed", string(body))

	// Explicitly confirm admin_authed was never set.
	resp2, err := client.Get(app.URL + "/")
	require.NoError(t, err)
	defer resp2.Body.Close()
	body2, err := io.ReadAll(resp2.Body)
	require.NoError(t, err)
	require.Equal(t, "anon", string(body2))
}
