package api

import (
	"database/sql"
	"net/http"
	"net/http/cookiejar"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func adminEnv(t *testing.T) *env {
	t.Helper()
	e := newEnv(t)
	e.s.routesAdmin(e.s.protected)
	return e
}

func TestChangePassword(t *testing.T) {
	e := adminEnv(t)
	loginAdmin(t, e)
	_, tok := e.session()

	resp, b := e.do("POST", "/api/settings/password", tok, map[string]string{"current": "wrongwrong", "new": "newpass123"})
	assert.Equal(t, 400, resp.StatusCode)
	assert.JSONEq(t, `{"error":"wrong_password"}`, string(b))

	resp, b = e.do("POST", "/api/settings/password", tok, map[string]string{"current": "hunter2hunter2", "new": "short"})
	assert.Equal(t, 400, resp.StatusCode)
	assert.JSONEq(t, `{"error":"password_short"}`, string(b))

	resp, b = e.do("POST", "/api/settings/password", tok, map[string]string{"current": "hunter2hunter2", "new": "newpass123"})
	require.Equal(t, 204, resp.StatusCode, string(b))

	// Still signed in after the change, and only the new password logs in.
	resp, _ = e.do("GET", "/api/overview", "", nil)
	assert.Equal(t, 200, resp.StatusCode)
	_, tok = e.session()
	resp, _ = e.do("POST", "/api/login", tok, map[string]string{"password": "hunter2hunter2"})
	assert.Equal(t, 401, resp.StatusCode)
	_, tok = e.session()
	resp, _ = e.do("POST", "/api/login", tok, map[string]string{"password": "newpass123"})
	assert.Equal(t, 204, resp.StatusCode)
}

// A password change signs out every other browser; the one that made it stays in.
func TestChangePassword_EndsOtherSessions(t *testing.T) {
	e := adminEnv(t)
	loginAdmin(t, e)
	jar, _ := cookiejar.New(nil)
	other := &env{t: t, srv: e.srv, cl: &http.Client{Jar: jar}, s: e.s}
	_, otok := other.session()
	resp, _ := other.do("POST", "/api/login", otok, map[string]string{"password": "hunter2hunter2"})
	require.Equal(t, 204, resp.StatusCode)
	resp, _ = other.do("GET", "/api/overview", "", nil)
	require.Equal(t, 200, resp.StatusCode)

	_, tok := e.session()
	resp, b := e.do("POST", "/api/settings/password", tok, map[string]string{"current": "hunter2hunter2", "new": "newpass123"})
	require.Equal(t, 204, resp.StatusCode, string(b))

	resp, _ = other.do("GET", "/api/overview", "", nil)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, "other session must end")
	resp, _ = e.do("GET", "/api/overview", "", nil)
	assert.Equal(t, 200, resp.StatusCode, "changing session stays in")
}

func TestChangePassword_RequiresAdmin(t *testing.T) {
	e := adminEnv(t)
	_, tok := e.session()
	resp, _ := e.do("POST", "/api/settings/password", tok, map[string]string{"current": "x", "new": "newpass123"})
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestSnapshot(t *testing.T) {
	e := adminEnv(t)
	loginAdmin(t, e)
	_, tok := e.session()

	resp, _ := e.do("POST", "/api/settings/snapshot", "", nil)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode, "CSRF applies")

	resp, b := e.do("POST", "/api/settings/snapshot", tok, nil)
	require.Equal(t, 200, resp.StatusCode, string(b))
	assert.Equal(t, "application/x-sqlite3", resp.Header.Get("Content-Type"))
	assert.Regexp(t, `^attachment; filename="grubdrops-\d{8}-\d{4}\.db"$`, resp.Header.Get("Content-Disposition"))
	assert.Equal(t, "SQLite format 3\x00", string(b[:16]))

	// The copy is a working database holding the admin row.
	path := t.TempDir() + "/snap.db"
	require.NoError(t, os.WriteFile(path, b, 0o600))
	db, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	defer db.Close()
	var n int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM admin").Scan(&n))
	assert.Equal(t, 1, n)
}

func TestQuoteSQLiteString(t *testing.T) {
	assert.Equal(t, `'/tmp/a''b.db'`, quoteSQLiteString("/tmp/a'b.db"))
}
