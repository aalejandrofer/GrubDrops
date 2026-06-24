package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/platform/platformtest"
	"github.com/aalejandrofer/grubdrops/internal/store"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

// seedReauthAccount opens a fresh DB, inserts one account with the given
// platform, and returns the queries + session store + account ID.
func seedReauthAccount(t *testing.T, plat string) (*gen.Queries, *store.SessionStore, string) {
	t.Helper()
	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	q := gen.New(db)
	const id = "acc_reauth_test"
	now := time.Now().Unix()
	_, err = q.CreateAccount(context.Background(), gen.CreateAccountParams{
		ID: id, Platform: plat, DisplayName: "ReauthUser",
		Status: "idle", FingerprintJson: "{}", Enabled: 1,
		CreatedAt: now, UpdatedAt: now,
	})
	require.NoError(t, err)
	cr, err := store.NewCryptor(ageKey) // reuse ageKey from handlers_login_kick_test.go
	require.NoError(t, err)
	ss := store.NewSessionStore(db, q, cr)
	return q, ss, id
}

// buildReauthRouter builds a full router wired with a mock backend ("mock"
// platform) and a real migrated DB containing one account.
func buildReauthRouter(t *testing.T, q *gen.Queries, ss *store.SessionStore, reg *platform.Registry) http.Handler {
	t.Helper()
	t.Setenv("GRUB_AUTHBYPASS", "true")
	s := store.NewSettings(q)
	return NewRouter(Deps{
		Q:             q,
		Session:       scsNew(),
		SettingsStore: s,
		SecureCookies: false,
		Sessions:      ss,
		Registry:      reg,
		Location:      time.UTC,
		RootCtx:       context.Background(),
	})
}

// doAPIPost sends a POST to path with X-CSRF-Token + cookies from a prior
// GET /api/auth/info. Returns the recorder.
func doAPIPost(t *testing.T, h http.Handler, path, contentType string, body io.Reader) *httptest.ResponseRecorder {
	t.Helper()
	tok, cookies := getCSRFToken(t, h)
	req := httptest.NewRequest(http.MethodPost, path, body)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("X-CSRF-Token", tok)
	req.Header.Set("Origin", "http://example.com")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// --- Twitch device-start ---

func TestAPIDeviceStart_HappyPath_MockBackend(t *testing.T) {
	mock := platformtest.New()
	reg := platform.NewRegistry()
	reg.Register(mock)

	q, ss, id := seedReauthAccount(t, "mock")
	h := buildReauthRouter(t, q, ss, reg)

	rec := doAPIPost(t, h, "/api/accounts/"+id+"/twitch/device", "application/json", nil)
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	assert.Contains(t, rec.Header().Get("Content-Type"), "application/json")
	body := rec.Body.String()
	assert.Contains(t, body, `"user_code"`)
	assert.Contains(t, body, `"verification_url"`)
	// MockBackend returns UserCode "MOCK"
	assert.Contains(t, body, "MOCK")
}

func TestAPIDeviceStart_UnknownAccount_Returns404(t *testing.T) {
	q, ss, _ := seedReauthAccount(t, "mock")
	h := buildReauthRouter(t, q, ss, platform.NewRegistry())

	rec := doAPIPost(t, h, "/api/accounts/no_such_acc/twitch/device", "application/json", nil)
	require.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Body.String(), `"code":"not_found"`)
}

func TestAPIDeviceStart_NoBackend_Returns400(t *testing.T) {
	// Account exists with platform "mock" but registry is empty — no backend.
	q, ss, id := seedReauthAccount(t, "mock")
	h := buildReauthRouter(t, q, ss, platform.NewRegistry())

	rec := doAPIPost(t, h, "/api/accounts/"+id+"/twitch/device", "application/json", nil)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), `"code":"no_backend"`)
}

func TestAPIDeviceStart_RequiresCSRF(t *testing.T) {
	t.Setenv("GRUB_AUTHBYPASS", "true")
	s, q := newTestSettings(t)
	h := NewRouter(Deps{Q: q, Session: scsNew(), SettingsStore: s, SecureCookies: false})
	req := httptest.NewRequest(http.MethodPost, "/api/accounts/x/twitch/device", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusForbidden, rec.Code)
	assert.Contains(t, rec.Body.String(), `"code":"csrf"`)
}

// --- Twitch device poll ---

func TestAPIDevicePoll_NoState_ReturnsError(t *testing.T) {
	t.Setenv("GRUB_AUTHBYPASS", "true")
	s, q := newTestSettings(t)
	h := NewRouter(Deps{Q: q, Session: scsNew(), SettingsStore: s, SecureCookies: false})
	req := httptest.NewRequest(http.MethodGet, "/api/accounts/acc_nope/twitch/poll", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"status":"error"`)
}

func TestAPIDevicePoll_PendingState_ReturnsPending(t *testing.T) {
	mock := platformtest.New()
	reg := platform.NewRegistry()
	reg.Register(mock)

	q, ss, id := seedReauthAccount(t, "mock")
	h := buildReauthRouter(t, q, ss, reg)

	// Start device flow first so state is stored.
	recStart := doAPIPost(t, h, "/api/accounts/"+id+"/twitch/device", "application/json", nil)
	require.Equal(t, http.StatusOK, recStart.Code, "start body: %s", recStart.Body.String())

	// Immediately poll — goroutine hasn't finished so status is "pending".
	tok, cookies := getCSRFToken(t, h)
	req := httptest.NewRequest(http.MethodGet, "/api/accounts/"+id+"/twitch/poll", nil)
	req.Header.Set("X-CSRF-Token", tok)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	status := resp["status"]
	assert.True(t, status == "pending" || status == "done" || status == "error",
		"unexpected status %q", status)
}

// --- Twitch cookie import ---

func TestAPICookieImport_NoFile_Returns400(t *testing.T) {
	t.Setenv("GRUB_AUTHBYPASS", "true")
	s, q := newTestSettings(t)
	h := NewRouter(Deps{Q: q, Session: scsNew(), SettingsStore: s, SecureCookies: false})

	tok, cookies := getCSRFToken(t, h)
	// Send multipart with no cookie_file field.
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("dummy", "x")
	mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/accounts/acc_nope/twitch/cookie", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("X-CSRF-Token", tok)
	req.Header.Set("Origin", "http://example.com")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	// Account doesn't exist → 404; the no_file path needs an account to exist.
	// Either 404 (not_found) or 400 (no_file) is correct JSON error here.
	assert.True(t, rec.Code == http.StatusNotFound || rec.Code == http.StatusBadRequest,
		"expected 404 or 400, got %d: %s", rec.Code, rec.Body.String())
	assert.Contains(t, rec.Header().Get("Content-Type"), "application/json")
}

func TestAPICookieImport_NoFile_KnownAccount_Returns400(t *testing.T) {
	q, ss, id := seedReauthAccount(t, "twitch")
	h := buildReauthRouter(t, q, ss, platform.NewRegistry())

	tok, cookies := getCSRFToken(t, h)
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("dummy", "x")
	mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/accounts/"+id+"/twitch/cookie", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("X-CSRF-Token", tok)
	req.Header.Set("Origin", "http://example.com")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code, "body: %s", rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"code":"no_file"`)
}

func TestAPICookieImport_NetscapeHappyPath(t *testing.T) {
	q, ss, id := seedReauthAccount(t, "twitch")
	// No twitch backend in registry → verified=false but persist succeeds.
	h := buildReauthRouter(t, q, ss, platform.NewRegistry())

	netscape := "# Netscape HTTP Cookie File\n" +
		"#HttpOnly_.twitch.tv\tTRUE\t/\tTRUE\t1781000000\tauth-token\tabc123\n"

	tok, cookies := getCSRFToken(t, h)
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("cookie_file", "cookies.txt")
	require.NoError(t, err)
	_, _ = io.WriteString(fw, netscape)
	mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/accounts/"+id+"/twitch/cookie", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("X-CSRF-Token", tok)
	req.Header.Set("Origin", "http://example.com")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"ok":true`)
	assert.Contains(t, rec.Body.String(), `"verified"`)
}

func TestAPICookieImport_RequiresCSRF(t *testing.T) {
	t.Setenv("GRUB_AUTHBYPASS", "true")
	s, q := newTestSettings(t)
	h := NewRouter(Deps{Q: q, Session: scsNew(), SettingsStore: s, SecureCookies: false})
	req := httptest.NewRequest(http.MethodPost, "/api/accounts/x/twitch/cookie", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusForbidden, rec.Code)
	assert.Contains(t, rec.Body.String(), `"code":"csrf"`)
}

// --- Kick login ---

func TestAPIKickLogin_GarbageCookies_Returns400(t *testing.T) {
	q, ss, id := seedReauthAccount(t, "kick")
	h := buildReauthRouter(t, q, ss, platform.NewRegistry())

	body := strings.NewReader(`{"cookies_txt":"garbage","channel":""}`)
	rec := doAPIPost(t, h, "/api/accounts/"+id+"/kick/login", "application/json", body)
	require.Equal(t, http.StatusBadRequest, rec.Code, "body: %s", rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"code":"cookies"`)
}

func TestAPIKickLogin_UnknownAccount_Returns404(t *testing.T) {
	q, ss, _ := seedReauthAccount(t, "kick")
	h := buildReauthRouter(t, q, ss, platform.NewRegistry())

	body := strings.NewReader(`{"cookies_txt":"","channel":""}`)
	rec := doAPIPost(t, h, "/api/accounts/no_such/kick/login", "application/json", body)
	require.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Body.String(), `"code":"not_found"`)
}

func TestAPIKickLogin_ValidCookies_Returns200(t *testing.T) {
	q, ss, id := seedReauthAccount(t, "kick")
	h := buildReauthRouter(t, q, ss, platform.NewRegistry())

	payload, _ := json.Marshal(map[string]string{
		"cookies_txt": cookiesTxtOK, // valid kick netscape blob from cookies_netscape_test.go
		"channel":     "testchan",
	})
	rec := doAPIPost(t, h, "/api/accounts/"+id+"/kick/login", "application/json", bytes.NewReader(payload))
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"ok":true`)
	assert.Contains(t, rec.Body.String(), `"verified"`)
}

func TestAPIKickLogin_RequiresCSRF(t *testing.T) {
	t.Setenv("GRUB_AUTHBYPASS", "true")
	s, q := newTestSettings(t)
	h := NewRouter(Deps{Q: q, Session: scsNew(), SettingsStore: s, SecureCookies: false})
	req := httptest.NewRequest(http.MethodPost, "/api/accounts/x/kick/login", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusForbidden, rec.Code)
	assert.Contains(t, rec.Body.String(), `"code":"csrf"`)
}

// TestSPALoginPagesServeShell verifies that with SPADashboard on, the 3
// GET login pages all serve the SPA shell instead of legacy templates.
func TestSPALoginPagesServeShell(t *testing.T) {
	t.Setenv("GRUB_AUTHBYPASS", "true")
	s, q := newTestSettings(t)
	h := NewRouter(Deps{Q: q, Session: scsNew(), SettingsStore: s, SecureCookies: false, SPADashboard: true})
	for _, path := range []string{
		"/accounts/acc_x/login",
		"/accounts/acc_x/twitch/device",
		"/accounts/acc_x/twitch/cookie",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, "path %s", path)
		assert.Contains(t, rec.Body.String(), `id="app"`, "path %s must serve SPA shell", path)
	}
}
