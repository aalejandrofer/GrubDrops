# Plan 2: Web GUI + Discord Notifier

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the bare `/healthz` HTTP surface with a usable HTMX-driven admin GUI (first-run setup, login, dashboard, account CRUD) and swap `NoopNotifier` for a real `DiscordWebhookNotifier`. By the end of this plan a fresh `docker compose up` lets the user open `http://127.0.0.1:8080`, set an admin password, log in, see the fake account on a live-polling dashboard, toggle it, and watch Discord receive `state` / `progress` / `claim` embeds.

**Architecture:** Server-rendered Go `html/template` pages embedded with `embed.FS`. HTMX polls partials every 2s for the dashboard. Session cookie via `alexedwards/scs/v2` backed by sqlite. CSRF via `justinas/nosurf` wrapped around mutating endpoints. Account state still loaded at daemon boot — toggling enabled flags in the GUI marks the row dirty; an explicit "Apply changes" action stops and re-starts watchers in-process. No browser sidecar, no real Twitch/Kick backend yet — that's Plan 3 / Plan 4.

**Tech Stack:** Go 1.26, `chi` (already a dep), `html/template`, `alexedwards/scs/v2` for sessions, `justinas/nosurf` for CSRF, `golang.org/x/crypto/bcrypt` for password hashing, `embed.FS` for templates + static assets, vanilla HTMX 2.0 CDN.

**Out of scope for Plan 2:**
- Real Twitch / Kick adapters (Plan 3 / Plan 4)
- WebSocket / SSE live event stream (HTMX polling is enough; can add in Plan 2.5)
- Per-account Discord webhook (global only in Plan 2; per-account in a later plan)
- Browser sidecar (Plan 4)
- Drop history / campaigns / logs pages (Plan 5)
- Production deploy compose (Plan 6)

---

## File Map

New files:

| File | Responsibility |
|---|---|
| `internal/store/migrations/0003_admin_kv.sql` | Indexes + constraints already present; no schema changes — keep file empty Up/Down if no schema delta needed. Drop if not needed. |
| `internal/auth/password.go` | bcrypt hash + verify |
| `internal/auth/password_test.go` | round-trip + reject-wrong |
| `internal/api/session.go` | scs.SessionManager constructor backed by sqlite `kv` table (custom store) |
| `internal/api/middleware.go` | RequireAdmin, CSRF protect wrapper, no-cache helpers |
| `internal/api/handlers_setup.go` | GET/POST /setup |
| `internal/api/handlers_auth.go` | GET/POST /login, POST /logout |
| `internal/api/handlers_dashboard.go` | GET / + GET /dashboard/cards (partial) |
| `internal/api/handlers_accounts.go` | GET/POST/DELETE on /accounts and /accounts/:id |
| `internal/api/render.go` | Helper: render template by name with shared layout |
| `internal/web/templates/_layout.html` | Base layout (html/head/body, HTMX script, nav) |
| `internal/web/templates/_nav.html` | Nav partial reused on every page |
| `internal/web/templates/setup.html` | First-run admin password form |
| `internal/web/templates/login.html` | Login form |
| `internal/web/templates/dashboard.html` | Account cards container |
| `internal/web/templates/dashboard_cards.html` | Partial polled by HTMX every 2s |
| `internal/web/templates/accounts_list.html` | Account table + "new" button |
| `internal/web/templates/accounts_new.html` | Pick platform + login form (fake-only in Plan 2) |
| `internal/web/templates/accounts_detail.html` | Edit display name + enable toggle + delete |
| `internal/web/embed.go` | `//go:embed templates` and parsed `*template.Template` factory |
| `internal/notify/discord.go` | DiscordWebhookNotifier impl |
| `internal/notify/discord_test.go` | httptest fake server validates payload shape |
| `internal/scheduler/control.go` | `Stop(ctx)` + `Reload(ctx, accounts)` to allow GUI-triggered rebuild |
| `internal/scheduler/state.go` | Per-account state snapshot exposed for dashboard |
| `internal/store/queries/admin.sql` | sqlc queries for admin password rows |
| `internal/store/queries/accounts_extras.sql` | DeleteAccount, UpdateAccountDisplayName |

Modified files:

| File | Change |
|---|---|
| `cmd/miner/main.go` | Wire session manager + handlers + Discord notifier (env-gated). Expose scheduler/store/cryptor via `api.Deps`. |
| `internal/api/server.go` | Expand `Deps` struct; mount real routes; preserve `/healthz` for liveness. |
| `internal/config/config.go` | Add `DiscordWebhookURL` env var; remove unused `SeedFakeAccount`. |
| `internal/store/queries/accounts.sql` | Add the two queries listed above (kept in a separate file to limit churn). |
| `internal/notify/notify.go` | Keep interface; no change. |
| `internal/scheduler/scheduler.go` | Refactor to expose state + Stop/Reload. |

---

## Task 1: Add dependencies and base test

**Files:**
- Modify: `go.mod`, `go.sum`

- [ ] **Step 1: Add deps**

```bash
go get github.com/alexedwards/scs/v2@v2.8.0
go get github.com/alexedwards/scs/sqlite3store@v0.0.0-20240316134038-7e11d57e8885
go get github.com/justinas/nosurf@v1.1.1
go get golang.org/x/crypto/bcrypt@latest
```

> The `scs/sqlite3store` import path uses `mattn/go-sqlite3` driver under the hood. We can't use that here because we're on `modernc.org/sqlite`. So instead of the canned sqlite store we'll roll a small one against our `kv` table. **Do NOT install `sqlite3store`** — remove that line from the `go get`. Final dep list is scs/v2, nosurf, bcrypt.

Revised:

```bash
go get github.com/alexedwards/scs/v2@v2.8.0
go get github.com/justinas/nosurf@v1.1.1
go get golang.org/x/crypto/bcrypt@latest
go mod tidy
```

- [ ] **Step 2: Confirm build still green**

```bash
go build ./...
go test ./...
```

Both clean.

- [ ] **Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "$(cat <<'EOF'
chore(deps): add scs, nosurf, bcrypt for web GUI auth

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: Password hash/verify helper

**Files:**
- Create: `internal/auth/password.go`
- Test: `internal/auth/password_test.go`

- [ ] **Step 1: Write failing test**

```go
// internal/auth/password_test.go
package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHashVerifyRoundTrip(t *testing.T) {
	h, err := HashPassword("hunter2")
	require.NoError(t, err)
	assert.NotEmpty(t, h)

	require.NoError(t, VerifyPassword(h, "hunter2"))
}

func TestVerifyRejectsWrongPassword(t *testing.T) {
	h, err := HashPassword("hunter2")
	require.NoError(t, err)
	require.Error(t, VerifyPassword(h, "wrong"))
}

func TestHashEmptyRejected(t *testing.T) {
	_, err := HashPassword("")
	require.Error(t, err)
}
```

- [ ] **Step 2: Implement**

```go
// internal/auth/password.go
package auth

import (
	"errors"

	"golang.org/x/crypto/bcrypt"
)

var ErrEmptyPassword = errors.New("password must not be empty")

func HashPassword(plain string) (string, error) {
	if plain == "" {
		return "", ErrEmptyPassword
	}
	b, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func VerifyPassword(hash, plain string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain))
}
```

- [ ] **Step 3: Run tests**

```bash
go test ./internal/auth/... -v
```

Expected: 3 PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/auth/
git commit -m "$(cat <<'EOF'
feat(auth): bcrypt password hash + verify helpers

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: sqlc query additions (admin + account extras)

**Files:**
- Create: `internal/store/queries/admin.sql`
- Create: `internal/store/queries/accounts_extras.sql`
- Re-run: `sqlc generate`

- [ ] **Step 1: Admin queries**

```sql
-- internal/store/queries/admin.sql

-- name: GetAdmin :one
SELECT * FROM admin WHERE id = 1;

-- name: UpsertAdmin :exec
INSERT INTO admin (id, password_hash, created_at)
VALUES (1, ?, ?)
ON CONFLICT(id) DO UPDATE SET password_hash = excluded.password_hash;

-- name: AdminExists :one
SELECT EXISTS(SELECT 1 FROM admin WHERE id = 1);
```

- [ ] **Step 2: Account extras**

```sql
-- internal/store/queries/accounts_extras.sql

-- name: UpdateAccountDisplayName :exec
UPDATE accounts SET display_name = ?, updated_at = ? WHERE id = ?;

-- name: DeleteAccount :exec
DELETE FROM accounts WHERE id = ?;

-- name: GetAccountByPlatformLogin :one
SELECT * FROM accounts WHERE platform = ? AND login = ?;
```

- [ ] **Step 3: Regenerate sqlc and verify build**

```bash
sqlc generate
go build ./...
go test ./...
```

Expected: clean.

- [ ] **Step 4: Commit**

```bash
git add internal/store/queries/ internal/store/gen/
git commit -m "$(cat <<'EOF'
feat(store): sqlc queries for admin password + account extras

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 4: SCS session store backed by the `kv` table

The session library doesn't ship a `modernc.org/sqlite` store. We'll write a tiny implementation against our existing `kv` table. SCS's `Store` interface needs `Find`, `Commit`, `Delete`, `All`.

**Files:**
- Create: `internal/api/session.go`
- Test: `internal/api/session_test.go`

- [ ] **Step 1: Write failing test**

```go
// internal/api/session_test.go
package api

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/rust-drops-miner/internal/store"
)

func TestKVStore_CommitFindDelete(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.db")
	db, err := store.Open(context.Background(), path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	s := NewKVSessionStore(db)

	require.NoError(t, s.Commit("tok1", []byte("payload"), time.Now().Add(time.Hour)))

	b, exists, err := s.Find("tok1")
	require.NoError(t, err)
	require.True(t, exists)
	assert.Equal(t, []byte("payload"), b)

	require.NoError(t, s.Delete("tok1"))

	_, exists, err = s.Find("tok1")
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestKVStore_ExpiredFindReturnsFalse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.db")
	db, err := store.Open(context.Background(), path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	s := NewKVSessionStore(db)
	require.NoError(t, s.Commit("tok2", []byte("p"), time.Now().Add(-1*time.Minute)))

	_, exists, err := s.Find("tok2")
	require.NoError(t, err)
	assert.False(t, exists)
}
```

- [ ] **Step 2: Implement the store**

```go
// internal/api/session.go
package api

import (
	"database/sql"
	"encoding/binary"
	"errors"
	"time"

	"github.com/alexedwards/scs/v2"
)

// kvSessionStore stores session blobs in the existing kv table.
// Keys are prefixed with "session:" to namespace from other kv uses.
// Value layout: [8 bytes big-endian unix nano expiry][N bytes session payload].
type kvSessionStore struct {
	db *sql.DB
}

func NewKVSessionStore(db *sql.DB) scs.Store {
	return &kvSessionStore{db: db}
}

const sessionPrefix = "session:"

func (s *kvSessionStore) Find(token string) ([]byte, bool, error) {
	var raw []byte
	err := s.db.QueryRow(`SELECT value FROM kv WHERE key = ?`, sessionPrefix+token).Scan(&raw)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, false, nil
	case err != nil:
		return nil, false, err
	}
	if len(raw) < 8 {
		return nil, false, nil
	}
	exp := time.Unix(0, int64(binary.BigEndian.Uint64(raw[:8])))
	if time.Now().After(exp) {
		_, _ = s.db.Exec(`DELETE FROM kv WHERE key = ?`, sessionPrefix+token)
		return nil, false, nil
	}
	return raw[8:], true, nil
}

func (s *kvSessionStore) Commit(token string, b []byte, expiry time.Time) error {
	buf := make([]byte, 8+len(b))
	binary.BigEndian.PutUint64(buf[:8], uint64(expiry.UnixNano()))
	copy(buf[8:], b)
	_, err := s.db.Exec(
		`INSERT INTO kv (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		sessionPrefix+token, buf,
	)
	return err
}

func (s *kvSessionStore) Delete(token string) error {
	_, err := s.db.Exec(`DELETE FROM kv WHERE key = ?`, sessionPrefix+token)
	return err
}

func (s *kvSessionStore) All() (map[string][]byte, error) {
	rows, err := s.db.Query(`SELECT key, value FROM kv WHERE key LIKE ?`, sessionPrefix+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]byte{}
	for rows.Next() {
		var k string
		var v []byte
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		if len(v) < 8 {
			continue
		}
		out[k[len(sessionPrefix):]] = v[8:]
	}
	return out, rows.Err()
}
```

- [ ] **Step 3: Verify**

```bash
go test ./internal/api/... -v
```

Expected: 2 new PASS + existing `TestHealthz` PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/api/session.go internal/api/session_test.go
git commit -m "$(cat <<'EOF'
feat(api): scs session store backed by sqlite kv table

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 5: Templates + embed.FS + render helper

**Files:**
- Create: `internal/web/embed.go`
- Create: `internal/web/templates/_layout.html`
- Create: `internal/web/templates/_nav.html`
- Create: `internal/api/render.go`

- [ ] **Step 1: Embed templates**

```go
// internal/web/embed.go
package web

import (
	"embed"
	"html/template"
	"io/fs"
)

//go:embed templates/*.html
var templatesFS embed.FS

// Templates returns a *template.Template with every page parsed against
// the shared layout. Each page is registered by its filename.
func Templates() (*template.Template, error) {
	t := template.New("").Funcs(template.FuncMap{
		"safe": func(s string) template.HTML { return template.HTML(s) },
	})
	matches, err := fs.Glob(templatesFS, "templates/*.html")
	if err != nil {
		return nil, err
	}
	for _, m := range matches {
		raw, err := templatesFS.ReadFile(m)
		if err != nil {
			return nil, err
		}
		if _, err := t.New(m).Parse(string(raw)); err != nil {
			return nil, err
		}
	}
	return t, nil
}
```

- [ ] **Step 2: Base layout**

```html
<!-- internal/web/templates/_layout.html -->
{{define "layout"}}
<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{block "title" .}}Rust Drops Miner{{end}}</title>
<script src="https://unpkg.com/htmx.org@2.0.2"></script>
<style>
  :root { color-scheme: light dark; font-family: system-ui, sans-serif; }
  body { max-width: 960px; margin: 2rem auto; padding: 0 1rem; line-height: 1.4; }
  nav { display: flex; gap: 1rem; padding-bottom: 1rem; border-bottom: 1px solid #ccc; margin-bottom: 1rem; }
  nav a { text-decoration: none; }
  .card { border: 1px solid #ccc; border-radius: 6px; padding: 1rem; margin-bottom: 0.75rem; }
  .row { display: flex; gap: 1rem; align-items: center; flex-wrap: wrap; }
  .badge { padding: 0.15rem 0.5rem; border-radius: 4px; background: #eee; font-size: 0.85rem; }
  form { display: flex; flex-direction: column; gap: 0.5rem; max-width: 320px; }
  input, select, button { padding: 0.4rem; font: inherit; }
  .err { color: #c00; }
  .ok { color: #060; }
</style>
</head>
<body>
{{template "nav" .}}
<main>
{{block "content" .}}{{end}}
</main>
</body>
</html>
{{end}}
```

- [ ] **Step 3: Nav partial**

```html
<!-- internal/web/templates/_nav.html -->
{{define "nav"}}
{{if .AuthedAdmin}}
<nav>
  <a href="/">Dashboard</a>
  <a href="/accounts">Accounts</a>
  <form method="post" action="/logout" style="margin-left:auto;display:inline">
    <input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
    <button type="submit">Logout</button>
  </form>
</nav>
{{else}}
<nav><strong>rust-drops-miner</strong></nav>
{{end}}
{{end}}
```

- [ ] **Step 4: Render helper**

```go
// internal/api/render.go
package api

import (
	"bytes"
	"html/template"
	"net/http"
)

type templateData struct {
	AuthedAdmin bool
	CSRFToken   string
	Page        any
	Flash       string
}

func render(w http.ResponseWriter, t *template.Template, name string, data templateData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, name, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_, _ = w.Write(buf.Bytes())
}

func renderPartial(w http.ResponseWriter, t *template.Template, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, name, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_, _ = w.Write(buf.Bytes())
}
```

- [ ] **Step 5: Build (no tests yet — exercised in later tasks)**

```bash
go build ./...
```

Clean.

- [ ] **Step 6: Commit**

```bash
git add internal/web/ internal/api/render.go
git commit -m "$(cat <<'EOF'
feat(web): embed templates, base layout, render helpers

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 6: Middleware (RequireAdmin, CSRF wrapper)

**Files:**
- Create: `internal/api/middleware.go`

- [ ] **Step 1: Write the middleware module**

```go
// internal/api/middleware.go
package api

import (
	"context"
	"net/http"

	"github.com/alexedwards/scs/v2"
	"github.com/justinas/nosurf"
)

type ctxKey int

const (
	ctxAdminAuthed ctxKey = iota
)

// RequireAdmin redirects unauthenticated users to /login. Allows /setup,
// /login, /logout, /healthz, and /static/* unconditionally — those handlers
// short-circuit before this runs.
func RequireAdmin(sm *scs.SessionManager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authed := sm.GetBool(r.Context(), "admin_authed")
			if !authed {
				http.Redirect(w, r, "/login", http.StatusSeeOther)
				return
			}
			ctx := context.WithValue(r.Context(), ctxAdminAuthed, true)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// CSRF wraps mutating endpoints. Get-only handlers do not need it but
// nosurf gracefully passes them through.
func CSRF(next http.Handler) http.Handler {
	h := nosurf.New(next)
	h.SetFailureHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "CSRF token invalid", http.StatusForbidden)
	}))
	return h
}

func csrfToken(r *http.Request) string {
	return nosurf.Token(r)
}

func isAdminAuthed(r *http.Request) bool {
	v, _ := r.Context().Value(ctxAdminAuthed).(bool)
	return v
}
```

- [ ] **Step 2: Build**

```bash
go build ./...
```

Clean.

- [ ] **Step 3: Commit**

```bash
git add internal/api/middleware.go
git commit -m "$(cat <<'EOF'
feat(api): RequireAdmin + CSRF middleware

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 7: Setup handler (first-run admin password)

**Files:**
- Create: `internal/api/handlers_setup.go`
- Create: `internal/web/templates/setup.html`

- [ ] **Step 1: Setup template**

```html
<!-- internal/web/templates/setup.html -->
{{define "setup.html"}}
{{template "layout" .}}
{{define "title"}}Setup · Rust Drops Miner{{end}}
{{define "content"}}
<h1>First-run setup</h1>
<p>Create the admin password. This is the only credential to manage this miner instance.</p>
{{if .Flash}}<p class="err">{{.Flash}}</p>{{end}}
<form method="post" action="/setup">
  <input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
  <label>Password<input type="password" name="password" minlength="8" required></label>
  <label>Confirm<input type="password" name="confirm" minlength="8" required></label>
  <button type="submit">Create admin</button>
</form>
{{end}}
{{end}}
```

- [ ] **Step 2: Handler**

```go
// internal/api/handlers_setup.go
package api

import (
	"context"
	"html/template"
	"net/http"
	"time"

	"github.com/alexedwards/scs/v2"

	"github.com/aalejandrofer/rust-drops-miner/internal/auth"
	"github.com/aalejandrofer/rust-drops-miner/internal/store/gen"
)

type setupDeps struct {
	q   *gen.Queries
	t   *template.Template
	sm  *scs.SessionManager
}

func (d setupDeps) get(w http.ResponseWriter, r *http.Request) {
	exists, err := d.q.AdminExists(r.Context())
	if err == nil && exists != 0 {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	render(w, d.t, "setup.html", templateData{
		AuthedAdmin: false,
		CSRFToken:   csrfToken(r),
	})
}

func (d setupDeps) post(w http.ResponseWriter, r *http.Request) {
	exists, err := d.q.AdminExists(r.Context())
	if err == nil && exists != 0 {
		http.Error(w, "admin already configured", http.StatusConflict)
		return
	}
	pw := r.FormValue("password")
	confirm := r.FormValue("confirm")
	if pw != confirm {
		render(w, d.t, "setup.html", templateData{
			AuthedAdmin: false,
			CSRFToken:   csrfToken(r),
			Flash:       "passwords do not match",
		})
		return
	}
	hash, err := auth.HashPassword(pw)
	if err != nil {
		render(w, d.t, "setup.html", templateData{
			AuthedAdmin: false,
			CSRFToken:   csrfToken(r),
			Flash:       err.Error(),
		})
		return
	}
	if err := d.q.UpsertAdmin(r.Context(), gen.UpsertAdminParams{
		PasswordHash: hash,
		CreatedAt:    time.Now().Unix(),
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Log the admin straight in.
	d.sm.Put(r.Context(), "admin_authed", true)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// Helper used by the public setup-needed redirect on the dashboard handler.
func adminConfigured(ctx context.Context, q *gen.Queries) bool {
	exists, err := q.AdminExists(ctx)
	return err == nil && exists != 0
}
```

- [ ] **Step 3: Build**

```bash
go build ./...
```

Clean. Wiring happens in Task 13.

- [ ] **Step 4: Commit**

```bash
git add internal/api/handlers_setup.go internal/web/templates/setup.html
git commit -m "$(cat <<'EOF'
feat(api): /setup first-run wizard for admin password

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 8: Auth handlers (login + logout)

**Files:**
- Create: `internal/api/handlers_auth.go`
- Create: `internal/web/templates/login.html`

- [ ] **Step 1: Login template**

```html
<!-- internal/web/templates/login.html -->
{{define "login.html"}}
{{template "layout" .}}
{{define "title"}}Login · Rust Drops Miner{{end}}
{{define "content"}}
<h1>Login</h1>
{{if .Flash}}<p class="err">{{.Flash}}</p>{{end}}
<form method="post" action="/login">
  <input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
  <label>Password<input type="password" name="password" required autofocus></label>
  <button type="submit">Sign in</button>
</form>
{{end}}
{{end}}
```

- [ ] **Step 2: Handlers**

```go
// internal/api/handlers_auth.go
package api

import (
	"html/template"
	"net/http"

	"github.com/alexedwards/scs/v2"

	"github.com/aalejandrofer/rust-drops-miner/internal/auth"
	"github.com/aalejandrofer/rust-drops-miner/internal/store/gen"
)

type authDeps struct {
	q  *gen.Queries
	t  *template.Template
	sm *scs.SessionManager
}

func (d authDeps) loginGet(w http.ResponseWriter, r *http.Request) {
	if !adminConfigured(r.Context(), d.q) {
		http.Redirect(w, r, "/setup", http.StatusSeeOther)
		return
	}
	if d.sm.GetBool(r.Context(), "admin_authed") {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	render(w, d.t, "login.html", templateData{CSRFToken: csrfToken(r)})
}

func (d authDeps) loginPost(w http.ResponseWriter, r *http.Request) {
	admin, err := d.q.GetAdmin(r.Context())
	if err != nil {
		render(w, d.t, "login.html", templateData{CSRFToken: csrfToken(r), Flash: "admin not configured"})
		return
	}
	if err := auth.VerifyPassword(admin.PasswordHash, r.FormValue("password")); err != nil {
		render(w, d.t, "login.html", templateData{CSRFToken: csrfToken(r), Flash: "wrong password"})
		return
	}
	if err := d.sm.RenewToken(r.Context()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	d.sm.Put(r.Context(), "admin_authed", true)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (d authDeps) logoutPost(w http.ResponseWriter, r *http.Request) {
	_ = d.sm.Destroy(r.Context())
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
```

- [ ] **Step 3: Build**

```bash
go build ./...
```

Clean.

- [ ] **Step 4: Commit**

```bash
git add internal/api/handlers_auth.go internal/web/templates/login.html
git commit -m "$(cat <<'EOF'
feat(api): /login and /logout handlers

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 9: Scheduler state exposure + Reload

The dashboard needs to read per-account state. The toggle action needs to stop and restart the scheduler with the new set of enabled accounts.

**Files:**
- Create: `internal/scheduler/state.go`
- Create: `internal/scheduler/control.go`
- Modify: `internal/scheduler/scheduler.go`
- Modify: `internal/watcher/watcher.go` (expose `AccountID`)
- Test: `internal/scheduler/control_test.go`

- [ ] **Step 1: Expose state from watcher**

In `internal/watcher/watcher.go`, add an accessor below `State()`:

```go
func (w *Watcher) AccountID() string { return w.cfg.AccountID }
```

- [ ] **Step 2: Scheduler state snapshot**

```go
// internal/scheduler/state.go
package scheduler

import "github.com/aalejandrofer/rust-drops-miner/internal/watcher"

type AccountState struct {
	AccountID string
	State     string
}

func (s *Scheduler) Snapshot() []AccountState {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]AccountState, 0, len(s.entries))
	for _, e := range s.entries {
		w, ok := e.runner.(*watcher.Watcher)
		if !ok {
			out = append(out, AccountState{AccountID: e.id, State: "unknown"})
			continue
		}
		out = append(out, AccountState{AccountID: e.id, State: w.State().String()})
	}
	return out
}
```

- [ ] **Step 3: Scheduler control**

Replace the supervise pattern with a context-per-run so we can cancel + restart:

```go
// internal/scheduler/control.go
package scheduler

import (
	"context"
	"sync"
)

// runState lets Stop cancel an in-flight Start without dropping wg semantics.
type runState struct {
	cancel context.CancelFunc
	wg     *sync.WaitGroup
}

func (s *Scheduler) startInternal(parent context.Context) *runState {
	ctx, cancel := context.WithCancel(parent)
	wg := &sync.WaitGroup{}

	s.mu.Lock()
	entries := append([]entry(nil), s.entries...)
	s.mu.Unlock()

	for _, e := range entries {
		wg.Add(1)
		go func(e entry) {
			defer wg.Done()
			s.supervise(ctx, e)
		}(e)
	}
	return &runState{cancel: cancel, wg: wg}
}

func (s *Scheduler) Stop(_ context.Context) {
	s.runMu.Lock()
	r := s.current
	s.current = nil
	s.runMu.Unlock()
	if r == nil {
		return
	}
	r.cancel()
	r.wg.Wait()
}

// Reload swaps the entry set and restarts. Caller passes a fresh parent
// context that controls overall lifetime.
func (s *Scheduler) Reload(parent context.Context, builders []EntryBuilder) error {
	s.Stop(parent)

	s.mu.Lock()
	s.entries = nil
	s.mu.Unlock()
	for _, b := range builders {
		s.AddEntry(b())
	}
	return s.Start(parent)
}

// EntryBuilder constructs a fresh runner for a given account. Used by main
// to rebuild watchers after enabled-flags change.
type EntryBuilder func() Entry

type Entry = entry

// AddEntry registers a pre-built entry (used by Reload).
func (s *Scheduler) AddEntry(e Entry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = append(s.entries, e)
}
```

- [ ] **Step 4: Refactor scheduler.go**

Replace the existing `Scheduler` struct + `Start` + `supervise` to use `runState`:

```go
// internal/scheduler/scheduler.go (replace existing implementation)
package scheduler

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/aalejandrofer/rust-drops-miner/internal/notify"
	"github.com/aalejandrofer/rust-drops-miner/internal/watcher"
)

type runner interface {
	Run(ctx context.Context) error
}

type entry struct {
	id     string
	runner runner
}

type Options struct {
	Notifier notify.Notifier
}

type Scheduler struct {
	opts Options

	mu      sync.Mutex
	entries []entry

	runMu   sync.Mutex
	current *runState
}

func New(opts Options) *Scheduler { return &Scheduler{opts: opts} }

func (s *Scheduler) Add(id string, w *watcher.Watcher) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = append(s.entries, entry{id: id, runner: w})
}

func (s *Scheduler) Start(ctx context.Context) error {
	s.runMu.Lock()
	defer s.runMu.Unlock()
	if s.current != nil {
		return errors.New("scheduler already running")
	}
	s.current = s.startInternal(ctx)
	return nil
}

func (s *Scheduler) Wait() {
	s.runMu.Lock()
	r := s.current
	s.runMu.Unlock()
	if r == nil {
		return
	}
	r.wg.Wait()
}

func (s *Scheduler) supervise(ctx context.Context, e entry) {
	defer func() {
		if r := recover(); r != nil {
			if s.opts.Notifier != nil {
				_ = s.opts.Notifier.Notify(ctx, notify.EventError, map[string]any{
					"account": e.id, "panic": fmt.Sprint(r),
				})
			}
		}
	}()
	if err := e.runner.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		if s.opts.Notifier != nil {
			_ = s.opts.Notifier.Notify(ctx, notify.EventError, map[string]any{
				"account": e.id, "error": err.Error(),
			})
		}
	}
}
```

- [ ] **Step 5: Test for Reload**

```go
// internal/scheduler/control_test.go
package scheduler

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/rust-drops-miner/internal/platform"
	"github.com/aalejandrofer/rust-drops-miner/internal/platform/fake"
	"github.com/aalejandrofer/rust-drops-miner/internal/watcher"
)

type counterNotifier struct{ claims atomic.Int64 }

func (c *counterNotifier) Notify(_ context.Context, ev string, _ map[string]any) error {
	if ev == "claim" {
		c.claims.Add(1)
	}
	return nil
}

func TestScheduler_StopThenReloadAddsAccount(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	notif := &counterNotifier{}
	s := New(Options{Notifier: notif})

	build := func(id string) EntryBuilder {
		return func() Entry {
			w := watcher.New(watcher.Config{
				AccountID:    id,
				Backend:      fake.New(fake.WithFastTime()),
				Session:      platform.Session{AccessToken: "x"},
				Notifier:     notif,
				TickInterval: 5 * time.Millisecond,
			})
			return Entry{id: id, runner: w}
		}
	}

	s.AddEntry(build("acc1")())
	require.NoError(t, s.Start(ctx))
	s.Wait()
	assert.Equal(t, int64(1), notif.claims.Load())

	// Reload with two accounts; each should claim.
	require.NoError(t, s.Reload(ctx, []EntryBuilder{build("acc2"), build("acc3")}))
	s.Wait()
	assert.Equal(t, int64(3), notif.claims.Load())
	_ = fmt.Sprintf
}
```

- [ ] **Step 6: Run**

```bash
go test -race ./internal/scheduler/... ./internal/watcher/... -v
```

Both should pass. The earlier `TestScheduler_RunsMultipleAccountsConcurrently` still works because `Start` happily runs the entries added via `Add`.

- [ ] **Step 7: Commit**

```bash
git add internal/scheduler/ internal/watcher/watcher.go
git commit -m "$(cat <<'EOF'
feat(scheduler): Stop/Reload + state snapshot for GUI

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 10: Dashboard handler + templates

**Files:**
- Create: `internal/api/handlers_dashboard.go`
- Create: `internal/web/templates/dashboard.html`
- Create: `internal/web/templates/dashboard_cards.html`

- [ ] **Step 1: Templates**

```html
<!-- internal/web/templates/dashboard.html -->
{{define "dashboard.html"}}
{{template "layout" .}}
{{define "title"}}Dashboard · Rust Drops Miner{{end}}
{{define "content"}}
<h1>Dashboard</h1>
<div id="cards" hx-get="/dashboard/cards" hx-trigger="every 2s" hx-swap="innerHTML">
  {{template "dashboard_cards" .Page}}
</div>
{{end}}
{{end}}
```

```html
<!-- internal/web/templates/dashboard_cards.html -->
{{define "dashboard_cards"}}
{{if not .}}<p>No accounts configured yet. <a href="/accounts/new">Add one</a>.</p>{{end}}
{{range .}}
<div class="card">
  <div class="row">
    <strong>{{.DisplayName}}</strong>
    <span class="badge">{{.Platform}}</span>
    <span class="badge">{{.State}}</span>
    {{if not .Enabled}}<span class="badge">disabled</span>{{end}}
    <a href="/accounts/{{.ID}}" style="margin-left:auto">manage</a>
  </div>
</div>
{{end}}
{{end}}
```

- [ ] **Step 2: Handler**

```go
// internal/api/handlers_dashboard.go
package api

import (
	"html/template"
	"net/http"

	"github.com/aalejandrofer/rust-drops-miner/internal/scheduler"
	"github.com/aalejandrofer/rust-drops-miner/internal/store/gen"
)

type dashboardDeps struct {
	q   *gen.Queries
	t   *template.Template
	sch *scheduler.Scheduler
}

type dashCard struct {
	ID, Platform, DisplayName, State string
	Enabled                          bool
}

func (d dashboardDeps) collect(r *http.Request) []dashCard {
	accs, err := d.q.ListEnabledAccounts(r.Context())
	if err != nil {
		return nil
	}
	stateByID := map[string]string{}
	for _, s := range d.sch.Snapshot() {
		stateByID[s.AccountID] = s.State
	}
	cards := make([]dashCard, 0, len(accs))
	for _, a := range accs {
		st, ok := stateByID[a.ID]
		if !ok {
			st = "stopped"
		}
		cards = append(cards, dashCard{
			ID: a.ID, Platform: a.Platform, DisplayName: a.DisplayName,
			State: st, Enabled: a.Enabled == 1,
		})
	}
	return cards
}

func (d dashboardDeps) page(w http.ResponseWriter, r *http.Request) {
	render(w, d.t, "dashboard.html", templateData{
		AuthedAdmin: true, CSRFToken: csrfToken(r),
		Page: d.collect(r),
	})
}

func (d dashboardDeps) cards(w http.ResponseWriter, r *http.Request) {
	renderPartial(w, d.t, "dashboard_cards", d.collect(r))
}
```

- [ ] **Step 3: Build**

```bash
go build ./...
```

Clean.

- [ ] **Step 4: Commit**

```bash
git add internal/api/handlers_dashboard.go internal/web/templates/dashboard.html internal/web/templates/dashboard_cards.html
git commit -m "$(cat <<'EOF'
feat(api): dashboard with HTMX-polled account cards

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 11: Accounts CRUD handlers + templates

For Plan 2 only the `fake` platform is available. Picking Twitch/Kick will land in Plan 3/4. We allow the user to:
- List accounts
- Add a new fake account (just enter a login string; no real auth)
- Rename the display name
- Toggle enabled
- Delete

After any mutation, return them to /accounts with a flash message reminding to click "Apply changes" on the dashboard to restart the scheduler.

**Files:**
- Create: `internal/api/handlers_accounts.go`
- Create: `internal/web/templates/accounts_list.html`
- Create: `internal/web/templates/accounts_new.html`
- Create: `internal/web/templates/accounts_detail.html`

- [ ] **Step 1: Templates**

```html
<!-- internal/web/templates/accounts_list.html -->
{{define "accounts_list.html"}}
{{template "layout" .}}
{{define "title"}}Accounts · Rust Drops Miner{{end}}
{{define "content"}}
<h1>Accounts</h1>
<p><a href="/accounts/new">+ Add account</a> · <form method="post" action="/accounts/apply" style="display:inline">
  <input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
  <button type="submit">Apply changes (reload watchers)</button>
</form></p>
{{if .Flash}}<p class="ok">{{.Flash}}</p>{{end}}
<table>
  <thead><tr><th>Display</th><th>Platform</th><th>Login</th><th>Enabled</th><th></th></tr></thead>
  <tbody>
  {{range .Page}}
  <tr>
    <td>{{.DisplayName}}</td>
    <td>{{.Platform}}</td>
    <td>{{.Login}}</td>
    <td>{{if eq .Enabled 1}}yes{{else}}no{{end}}</td>
    <td><a href="/accounts/{{.ID}}">edit</a></td>
  </tr>
  {{end}}
  </tbody>
</table>
{{end}}
{{end}}
```

```html
<!-- internal/web/templates/accounts_new.html -->
{{define "accounts_new.html"}}
{{template "layout" .}}
{{define "title"}}New account · Rust Drops Miner{{end}}
{{define "content"}}
<h1>New account</h1>
{{if .Flash}}<p class="err">{{.Flash}}</p>{{end}}
<form method="post" action="/accounts/new">
  <input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
  <label>Platform
    <select name="platform">
      <option value="fake">fake (development)</option>
    </select>
  </label>
  <label>Login (handle)<input type="text" name="login" required></label>
  <label>Display name<input type="text" name="display_name"></label>
  <button type="submit">Create</button>
</form>
<p><a href="/accounts">cancel</a></p>
{{end}}
{{end}}
```

```html
<!-- internal/web/templates/accounts_detail.html -->
{{define "accounts_detail.html"}}
{{template "layout" .}}
{{define "title"}}Account · Rust Drops Miner{{end}}
{{define "content"}}
{{with .Page}}
<h1>{{.DisplayName}}</h1>
<p>Platform: <span class="badge">{{.Platform}}</span> Login: <code>{{.Login}}</code></p>
<form method="post" action="/accounts/{{.ID}}/update">
  <input type="hidden" name="csrf_token" value="{{$.CSRFToken}}">
  <label>Display name<input type="text" name="display_name" value="{{.DisplayName}}"></label>
  <label><input type="checkbox" name="enabled" value="1" {{if eq .Enabled 1}}checked{{end}}> Enabled</label>
  <button type="submit">Save</button>
</form>
<form method="post" action="/accounts/{{.ID}}/delete" onsubmit="return confirm('Delete this account permanently?');" style="margin-top:1rem">
  <input type="hidden" name="csrf_token" value="{{$.CSRFToken}}">
  <button type="submit">Delete</button>
</form>
{{end}}
<p><a href="/accounts">back</a></p>
{{end}}
{{end}}
```

- [ ] **Step 2: Handlers**

```go
// internal/api/handlers_accounts.go
package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"html/template"
	"net/http"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"github.com/aalejandrofer/rust-drops-miner/internal/store/gen"
)

type accountsDeps struct {
	q  *gen.Queries
	t  *template.Template
	sm *scs.SessionManager
}

func (d accountsDeps) list(w http.ResponseWriter, r *http.Request) {
	rows, err := d.q.ListEnabledAccounts(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	flash := d.sm.PopString(r.Context(), "flash")
	render(w, d.t, "accounts_list.html", templateData{
		AuthedAdmin: true, CSRFToken: csrfToken(r),
		Page: rows, Flash: flash,
	})
}

func (d accountsDeps) newGet(w http.ResponseWriter, r *http.Request) {
	render(w, d.t, "accounts_new.html", templateData{
		AuthedAdmin: true, CSRFToken: csrfToken(r),
	})
}

func (d accountsDeps) newPost(w http.ResponseWriter, r *http.Request) {
	platform := r.FormValue("platform")
	login := r.FormValue("login")
	display := r.FormValue("display_name")
	if platform == "" || login == "" {
		render(w, d.t, "accounts_new.html", templateData{
			AuthedAdmin: true, CSRFToken: csrfToken(r),
			Flash: "platform and login required",
		})
		return
	}
	if display == "" {
		display = login
	}
	id := genID()
	now := time.Now().Unix()
	if _, err := d.q.CreateAccount(r.Context(), gen.CreateAccountParams{
		ID: id, Platform: platform, Login: login, DisplayName: display,
		Status: "idle", FingerprintJson: "{}", Enabled: 1,
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		render(w, d.t, "accounts_new.html", templateData{
			AuthedAdmin: true, CSRFToken: csrfToken(r),
			Flash: err.Error(),
		})
		return
	}
	d.sm.Put(r.Context(), "flash", "account added — click Apply changes to start mining")
	http.Redirect(w, r, "/accounts", http.StatusSeeOther)
}

func (d accountsDeps) detail(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	row, err := d.q.GetAccount(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	render(w, d.t, "accounts_detail.html", templateData{
		AuthedAdmin: true, CSRFToken: csrfToken(r),
		Page: row,
	})
}

func (d accountsDeps) update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	display := r.FormValue("display_name")
	enabled := int64(0)
	if r.FormValue("enabled") == "1" {
		enabled = 1
	}
	now := time.Now().Unix()
	if display != "" {
		if err := d.q.UpdateAccountDisplayName(r.Context(), gen.UpdateAccountDisplayNameParams{
			DisplayName: display, UpdatedAt: now, ID: id,
		}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	if err := d.q.SetAccountEnabled(r.Context(), gen.SetAccountEnabledParams{
		Enabled: enabled, UpdatedAt: now, ID: id,
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	d.sm.Put(r.Context(), "flash", "saved — click Apply changes to reload watchers")
	http.Redirect(w, r, "/accounts", http.StatusSeeOther)
}

func (d accountsDeps) delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := d.q.DeleteAccount(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	d.sm.Put(r.Context(), "flash", "deleted — click Apply changes to reload watchers")
	http.Redirect(w, r, "/accounts", http.StatusSeeOther)
}

func genID() string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return "acc_" + hex.EncodeToString(b[:])
}

// Apply is mounted on POST /accounts/apply by the main wiring.
// Implementation provided by the reloader passed in from cmd/miner.
type Reloader interface {
	Reload(ctx context.Context) error
}
```

- [ ] **Step 3: Build**

```bash
go build ./...
```

Clean.

- [ ] **Step 4: Commit**

```bash
git add internal/api/handlers_accounts.go internal/web/templates/accounts_list.html internal/web/templates/accounts_new.html internal/web/templates/accounts_detail.html
git commit -m "$(cat <<'EOF'
feat(api): accounts CRUD pages (list/new/detail/update/delete)

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 12: Discord webhook notifier

**Files:**
- Create: `internal/notify/discord.go`
- Create: `internal/notify/discord_test.go`

- [ ] **Step 1: Write the test**

```go
// internal/notify/discord_test.go
package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiscordWebhook_SendsExpectedPayload(t *testing.T) {
	var mu sync.Mutex
	var got []map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		require.NoError(t, json.Unmarshal(body, &payload))
		mu.Lock()
		got = append(got, payload)
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	n := NewDiscordWebhook(srv.URL, nil)
	require.NoError(t, n.Notify(context.Background(), EventClaim, map[string]any{
		"account": "acc1", "benefit": "ben_helmet",
	}))

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, got, 1)
	embeds, _ := got[0]["embeds"].([]any)
	require.Len(t, embeds, 1)
	embed := embeds[0].(map[string]any)
	assert.Equal(t, "Drop claimed", embed["title"])
}

func TestDiscordWebhook_DropsBelowVerbosity(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not have been called")
	}))
	defer srv.Close()

	n := NewDiscordWebhook(srv.URL, &VerbosityFilter{Allow: map[string]bool{
		EventClaim: true,
	}})
	require.NoError(t, n.Notify(context.Background(), EventState, map[string]any{}))
}
```

- [ ] **Step 2: Implement**

```go
// internal/notify/discord.go
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type VerbosityFilter struct {
	Allow map[string]bool
}

type DiscordWebhook struct {
	URL    string
	Filter *VerbosityFilter
	HTTP   *http.Client
}

func NewDiscordWebhook(url string, filter *VerbosityFilter) *DiscordWebhook {
	return &DiscordWebhook{
		URL:    url,
		Filter: filter,
		HTTP:   &http.Client{Timeout: 10 * time.Second},
	}
}

func (d *DiscordWebhook) Notify(ctx context.Context, event Event, fields map[string]any) error {
	if d.Filter != nil && !d.Filter.Allow[event] {
		return nil
	}
	embed := buildEmbed(event, fields)
	body, err := json.Marshal(map[string]any{"embeds": []any{embed}})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := d.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("discord webhook %s: %s", d.URL, resp.Status)
	}
	return nil
}

func buildEmbed(event Event, fields map[string]any) map[string]any {
	title := titleFor(event)
	color := colorFor(event)
	desc := descFor(event, fields)
	return map[string]any{
		"title": title, "description": desc, "color": color,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}
}

func titleFor(event Event) string {
	switch event {
	case EventClaim:
		return "Drop claimed"
	case EventProgress:
		return "Drop progress"
	case EventState:
		return "State change"
	case EventAuth:
		return "Auth event"
	case EventError:
		return "Error"
	default:
		return event
	}
}

func colorFor(event Event) int {
	switch event {
	case EventClaim:
		return 0x2ecc71 // green
	case EventError:
		return 0xe74c3c // red
	case EventProgress:
		return 0xf1c40f // yellow
	default:
		return 0x95a5a6 // grey
	}
}

func descFor(_ Event, fields map[string]any) string {
	if len(fields) == 0 {
		return ""
	}
	parts := make([]string, 0, len(fields))
	for k, v := range fields {
		parts = append(parts, fmt.Sprintf("%s: %v", k, v))
	}
	return joinLines(parts)
}

func joinLines(parts []string) string {
	var buf bytes.Buffer
	for i, p := range parts {
		if i > 0 {
			buf.WriteByte('\n')
		}
		buf.WriteString(p)
	}
	return buf.String()
}
```

- [ ] **Step 3: Run tests**

```bash
go test ./internal/notify/... -v
```

Expected: 2 PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/notify/discord.go internal/notify/discord_test.go
git commit -m "$(cat <<'EOF'
feat(notify): Discord webhook notifier with verbosity filter

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 13: Config additions

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`

- [ ] **Step 1: Extend config**

```go
// internal/config/config.go (replace whole file)
package config

import (
	"fmt"
	"os"
	"strings"
)

type Config struct {
	HTTPAddr          string
	DBPath            string
	MasterKey         string
	DiscordWebhookURL string // empty = no Discord, use Noop
}

func Load() (Config, error) {
	cfg := Config{
		HTTPAddr:          getenv("MINER_HTTP_ADDR", "0.0.0.0:8080"),
		DBPath:            getenv("MINER_DB_PATH", "/data/miner.db"),
		MasterKey:         os.Getenv("MINER_MASTER_KEY"),
		DiscordWebhookURL: os.Getenv("MINER_DISCORD_WEBHOOK"),
	}
	if strings.TrimSpace(cfg.MasterKey) == "" {
		return Config{}, fmt.Errorf("MINER_MASTER_KEY is required")
	}
	return cfg, nil
}

func getenv(k, d string) string {
	if v, ok := os.LookupEnv(k); ok && v != "" {
		return v
	}
	return d
}
```

> The `SeedFakeAccount` field is removed. The dev seed migration stays in place (it's idempotent and harmless), and accounts seeded by it appear in the GUI like any other account; the operator can delete them via the GUI when they don't want them anymore.

- [ ] **Step 2: Update tests**

Replace the `SeedFakeAccount` checks in `internal/config/config_test.go`:

```go
package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad_RequiresMasterKey(t *testing.T) {
	t.Setenv("MINER_MASTER_KEY", "")
	_, err := Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "MINER_MASTER_KEY")
}

func TestLoad_DefaultsApplied(t *testing.T) {
	t.Setenv("MINER_MASTER_KEY", "AGE-SECRET-KEY-1XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX0")
	t.Setenv("MINER_DISCORD_WEBHOOK", "")

	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, "0.0.0.0:8080", cfg.HTTPAddr)
	assert.Equal(t, "/data/miner.db", cfg.DBPath)
	assert.Equal(t, "", cfg.DiscordWebhookURL)
}

func TestLoad_Overrides(t *testing.T) {
	t.Setenv("MINER_MASTER_KEY", "AGE-SECRET-KEY-1XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX0")
	t.Setenv("MINER_HTTP_ADDR", "127.0.0.1:9000")
	t.Setenv("MINER_DB_PATH", "/tmp/m.db")
	t.Setenv("MINER_DISCORD_WEBHOOK", "https://discord.example/wh/x")

	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, "127.0.0.1:9000", cfg.HTTPAddr)
	assert.Equal(t, "/tmp/m.db", cfg.DBPath)
	assert.Equal(t, "https://discord.example/wh/x", cfg.DiscordWebhookURL)
}
```

- [ ] **Step 3: Run**

```bash
go test ./internal/config/... -v
go build ./...
```

`go build ./...` will fail because `cmd/miner/main.go` still references `cfg.SeedFakeAccount` (it doesn't — but if so, fix in next task). Hold; the wiring task replaces main.

- [ ] **Step 4: Commit**

```bash
git add internal/config/
git commit -m "$(cat <<'EOF'
feat(config): add MINER_DISCORD_WEBHOOK, drop unused SeedFakeAccount

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 14: Update .env.example

**Files:**
- Modify: `.env.example`

- [ ] **Step 1: Replace file**

```dotenv
# Required: age secret key (generate once with `age-keygen`)
MINER_MASTER_KEY=AGE-SECRET-KEY-1XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX0

# Optional overrides
MINER_HTTP_ADDR=0.0.0.0:8080
MINER_DB_PATH=/data/miner.db

# Optional: Discord webhook URL for state/progress/claim/error notifications.
# Leave empty to disable Discord (falls back to log-only NoopNotifier).
MINER_DISCORD_WEBHOOK=
```

- [ ] **Step 2: Commit**

```bash
git add .env.example
git commit -m "$(cat <<'EOF'
docs(.env): document MINER_DISCORD_WEBHOOK and drop SeedFakeAccount

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 15: Main wiring — mount everything

**Files:**
- Modify: `cmd/miner/main.go`
- Modify: `internal/api/server.go`

This is the integration step. We expose a single `api.Deps` struct that carries everything the handlers need.

- [ ] **Step 1: Rewrite `internal/api/server.go`**

```go
// internal/api/server.go
package api

import (
	"context"
	"database/sql"
	"html/template"
	"net/http"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/aalejandrofer/rust-drops-miner/internal/scheduler"
	"github.com/aalejandrofer/rust-drops-miner/internal/store/gen"
)

type Deps struct {
	DB         *sql.DB
	Q          *gen.Queries
	Templates  *template.Template
	Session    *scs.SessionManager
	Scheduler  *scheduler.Scheduler
	Reload     func(context.Context) error
}

func NewRouter(d Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})

	if d.Session == nil {
		// Skeleton mode (used by TestHealthz) — no business routes.
		return r
	}

	// Everything below requires the full deps wiring.
	setup := setupDeps{q: d.Q, t: d.Templates, sm: d.Session}
	authH := authDeps{q: d.Q, t: d.Templates, sm: d.Session}
	dash := dashboardDeps{q: d.Q, t: d.Templates, sch: d.Scheduler}
	accs := accountsDeps{q: d.Q, t: d.Templates, sm: d.Session}

	withSession := func(h http.Handler) http.Handler { return d.Session.LoadAndSave(h) }
	withSessionCSRF := func(h http.HandlerFunc) http.Handler {
		return withSession(CSRF(http.HandlerFunc(h)))
	}

	r.Method(http.MethodGet, "/setup", withSessionCSRF(setup.get))
	r.Method(http.MethodPost, "/setup", withSessionCSRF(setup.post))
	r.Method(http.MethodGet, "/login", withSessionCSRF(authH.loginGet))
	r.Method(http.MethodPost, "/login", withSessionCSRF(authH.loginPost))

	// Authed area
	authed := chi.NewRouter()
	authed.Use(func(next http.Handler) http.Handler { return RequireAdmin(d.Session)(next) })
	authed.Get("/", dash.page)
	authed.Get("/dashboard/cards", dash.cards)
	authed.Get("/accounts", accs.list)
	authed.Get("/accounts/new", accs.newGet)
	authed.Post("/accounts/new", accs.newPost)
	authed.Get("/accounts/{id}", accs.detail)
	authed.Post("/accounts/{id}/update", accs.update)
	authed.Post("/accounts/{id}/delete", accs.delete)
	authed.Post("/accounts/apply", func(w http.ResponseWriter, r *http.Request) {
		if err := d.Reload(r.Context()); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		d.Session.Put(r.Context(), "flash", "watchers reloaded")
		http.Redirect(w, r, "/accounts", http.StatusSeeOther)
	})
	authed.Post("/logout", authH.logoutPost)

	r.Mount("/", withSession(CSRF(authed)))
	return r
}
```

- [ ] **Step 2: Rewrite `cmd/miner/main.go`**

```go
// cmd/miner/main.go
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/alexedwards/scs/v2"

	"github.com/aalejandrofer/rust-drops-miner/internal/api"
	"github.com/aalejandrofer/rust-drops-miner/internal/config"
	mlog "github.com/aalejandrofer/rust-drops-miner/internal/log"
	"github.com/aalejandrofer/rust-drops-miner/internal/notify"
	"github.com/aalejandrofer/rust-drops-miner/internal/platform"
	"github.com/aalejandrofer/rust-drops-miner/internal/platform/fake"
	"github.com/aalejandrofer/rust-drops-miner/internal/scheduler"
	"github.com/aalejandrofer/rust-drops-miner/internal/store"
	"github.com/aalejandrofer/rust-drops-miner/internal/store/gen"
	"github.com/aalejandrofer/rust-drops-miner/internal/watcher"
	"github.com/aalejandrofer/rust-drops-miner/internal/web"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ring := mlog.NewRing(1000)
	logger := mlog.NewWithRing(os.Stdout, "info", ring)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	db, err := store.Open(ctx, cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer db.Close()

	cryptor, err := store.NewCryptor(cfg.MasterKey)
	if err != nil {
		return fmt.Errorf("master key invalid: %w", err)
	}
	_ = cryptor

	q := gen.New(db)

	templates, err := web.Templates()
	if err != nil {
		return fmt.Errorf("load templates: %w", err)
	}

	sm := scs.New()
	sm.Store = api.NewKVSessionStore(db)
	sm.Lifetime = 12 * time.Hour
	sm.Cookie.HttpOnly = true
	sm.Cookie.SameSite = http.SameSiteStrictMode

	registry := platform.NewRegistry()
	registry.Register(fake.New(fake.WithFastTime()))

	notifier := makeNotifier(cfg, logger)
	sched := scheduler.New(scheduler.Options{Notifier: notifier})

	build := func(a gen.Account) (scheduler.Entry, error) {
		b, ok := registry.Get(a.Platform)
		if !ok {
			return scheduler.Entry{}, fmt.Errorf("no backend for platform %q", a.Platform)
		}
		sess, err := b.PollDeviceLogin(ctx, platform.DeviceChallenge{})
		if err != nil {
			return scheduler.Entry{}, fmt.Errorf("device login: %w", err)
		}
		w := watcher.New(watcher.Config{
			AccountID: a.ID, Backend: b, Session: sess,
			Notifier: notifier, TickInterval: 500 * time.Millisecond,
		})
		return scheduler.NewEntry(a.ID, w), nil
	}

	loadAndStart := func(parent context.Context) error {
		accs, err := q.ListEnabledAccounts(parent)
		if err != nil {
			return err
		}
		builders := make([]scheduler.EntryBuilder, 0, len(accs))
		for _, a := range accs {
			a := a
			builders = append(builders, func() scheduler.Entry {
				e, err := build(a)
				if err != nil {
					logger.Error("account skipped", "account", a.ID, "err", err)
					return scheduler.NewEntry(a.ID, nopRunner{})
				}
				return e
			})
		}
		return sched.Reload(parent, builders)
	}

	if err := loadAndStart(ctx); err != nil {
		return fmt.Errorf("initial scheduler boot: %w", err)
	}

	deps := api.Deps{
		DB: db, Q: q, Templates: templates, Session: sm,
		Scheduler: sched, Reload: loadAndStart,
	}

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           api.NewRouter(deps),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		logger.Info("http listening", "addr", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("http server", "err", err)
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)

	sched.Stop(shutdownCtx)
	return nil
}

func makeNotifier(cfg config.Config, logger interface {
	Info(string, ...any)
	Error(string, ...any)
}) notify.Notifier {
	if cfg.DiscordWebhookURL != "" {
		return notify.NewDiscordWebhook(cfg.DiscordWebhookURL, &notify.VerbosityFilter{Allow: map[string]bool{
			notify.EventClaim: true, notify.EventError: true, notify.EventProgress: true, notify.EventAuth: true,
		}})
	}
	return &notify.NoopNotifier{Logger: nil}
}

type nopRunner struct{}

func (nopRunner) Run(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }
```

- [ ] **Step 3: Helper for `NewEntry`**

Add to `internal/scheduler/control.go`:

```go
// Append at end of control.go
func NewEntry(id string, r runner) Entry { return Entry{id: id, runner: r} }
```

> `Entry` is `type Entry = entry`. `entry`'s `runner` field is unexported but `runner` is an interface type in the same package — exposing via a constructor function gives external packages a way to build entries without leaking the field name.

- [ ] **Step 4: Compile**

```bash
go build ./...
```

If `NoopNotifier{Logger: nil}` panics because the field needs a non-nil slog logger, fix `internal/notify/noop.go` to handle a nil logger gracefully:

```go
func (n *NoopNotifier) Notify(_ context.Context, event Event, fields map[string]any) error {
    if n.Logger == nil {
        return nil
    }
    // existing body...
}
```

- [ ] **Step 5: Run all tests**

```bash
go test -race ./...
```

All green.

- [ ] **Step 6: Smoke run**

```bash
mkdir -p data
export MINER_MASTER_KEY="$(go run filippo.io/age/cmd/age-keygen 2>&1 | awk '/AGE-SECRET-KEY/ {print $NF}')"
go build -o bin/miner ./cmd/miner
MINER_DB_PATH=./data/miner.db ./bin/miner > /tmp/miner.log 2>&1 &
PID=$!
sleep 3
curl -fsS -i http://127.0.0.1:8080/setup | head -1
# Should be HTTP/1.1 200 OK (no admin yet, /setup serves the form)

curl -fsS -i http://127.0.0.1:8080/ | head -1
# Should be 303 See Other (redirect to /login because /setup hasn't been completed)

kill $PID
wait $PID 2>/dev/null || true
```

- [ ] **Step 7: Commit**

```bash
git add cmd/miner/main.go internal/api/server.go internal/scheduler/control.go internal/notify/noop.go
git commit -m "$(cat <<'EOF'
feat(cmd/miner): wire web GUI, session manager, Discord notifier, Reload

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 16: End-to-end manual verification

**Files:** none (verification only)

- [ ] **Step 1: Boot stack with Docker compose**

```bash
cd deploy
mkdir -p data
export MINER_MASTER_KEY="$(go run filippo.io/age/cmd/age-keygen 2>&1 | awk '/AGE-SECRET-KEY/ {print $NF}')"
docker compose up --build -d
sleep 5
```

- [ ] **Step 2: Walk the flow**

Open http://127.0.0.1:8080 in a browser.

1. Redirects to `/setup`. Enter a password twice, submit.
2. Lands on dashboard with the `acc_fake_dev` card showing current state (idle → pick_stream → watching → claiming → idle → sleeping).
3. Click "Accounts" → see the seeded fake account row.
4. Click into it, rename "Dev User" → "demo", click Save.
5. Toggle Enabled off, Save. Confirm flash message about "Apply changes".
6. Back to `/accounts`, click "Apply changes (reload watchers)".
7. Dashboard now shows no live state badge (the disabled account isn't running).
8. Toggle Enabled back on, Apply changes again. Mining resumes.
9. Logout → should land on `/login`.

- [ ] **Step 3: Discord smoke (optional)**

If you have a test Discord webhook URL, set it and rerun:

```bash
docker compose down
MINER_DISCORD_WEBHOOK=https://discord.com/api/webhooks/... \
MINER_MASTER_KEY=... docker compose up --build -d
```

Expect within 5 seconds: one "State change", one "Drop progress" (or several), one "Drop claimed" green embed in your Discord channel.

- [ ] **Step 4: Teardown**

```bash
docker compose down
```

- [ ] **Step 5: Commit a marker note (optional)**

If you keep handwritten test notes, add a one-liner under `docs/superpowers/notes/2026-06-04-plan-02-manual-verification.md`. Otherwise skip.

---

## Done definition

After Task 16:

1. `docker compose up --build -d` boots a daemon serving the web GUI on `:8080`.
2. Fresh browser visit → `/setup` form, after submit → dashboard, after logout → `/login`.
3. Accounts CRUD works end-to-end through the GUI; "Apply changes" reloads watchers without restarting the process.
4. When `MINER_DISCORD_WEBHOOK` is set, Discord receives green/yellow/red embeds for claim/progress/error events.
5. `go test -race ./...` green across all packages.

## Self-review notes

- The dev seed migration is untouched. The GUI can simply delete `acc_fake_dev` if the operator doesn't want it.
- `Reload` is invoked at boot AND on POST `/accounts/apply`. That keeps the wiring path uniform.
- CSRF is wrapped around the entire mount including login, so even the unauthenticated forms have CSRF protection.
- `cryptor` is still held but unused; Plan 3 will wire it into session-blob persistence for real platform sessions.
- The `nopRunner` fallback in `cmd/miner/main.go` keeps a slot alive when an account fails to build a watcher — it blocks on the context until cancellation, so `Reload` and `Stop` still behave deterministically. Alternative: skip the entry entirely. Either is fine; chose the former so the dashboard reports a stable AccountID list.
- Verbosity is hard-coded in `makeNotifier`. A per-account verbosity setting belongs in a settings page (Plan 5 or later).

## Next plan preview

Plan 3: Twitch backend (real GraphQL client, device-code flow, MinuteWatched mutation, drops campaign discovery). It replaces the FakeBackend on the registry for the `twitch` platform string and exercises the cryptor for session persistence.
