# Plan 1: Foundation + Vertical Slice (FakeBackend)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stand up the daemon skeleton (config, sqlite, logging, account scheduler, watcher state machine, notifier interface) and prove the design end-to-end against a deterministic `FakeBackend`. No real Twitch/Kick code, no web GUI surface beyond `/healthz`. By the end, `docker compose up` runs a process that mines a fake drop and logs a successful claim.

**Architecture:** Single Go binary, goroutine-per-account. Concrete impls behind small interfaces (`Backend`, `Notifier`, `Store`) so subsequent plans can swap in real Twitch/Kick/Discord without touching the watcher.

**Tech Stack:** Go 1.22+, sqlite via `modernc.org/sqlite` (CGO-free), `sqlc` for queries, `goose` for migrations, `filippo.io/age` for at-rest encryption, `log/slog`, `chi` for HTTP routing, `testify` for tests, distroless multi-stage Docker image.

**Out of scope for this plan:**
- Real Twitch / Kick adapters (Plan 3 / Plan 4)
- HTMX web GUI surface (Plan 2)
- Discord webhook delivery (Plan 2; this plan ships `NoopNotifier`)
- Browser sidecar (Plan 4)
- Homelab/k3s deployment (separate deploy plan)

---

## File Map

| File | Responsibility |
|---|---|
| `go.mod`, `go.sum` | Module declaration, deps |
| `.gitignore` | Ignore binaries, `data/`, `.env` |
| `cmd/miner/main.go` | Entrypoint: config load, store open, scheduler boot, HTTP server, signal shutdown |
| `internal/config/config.go` | Env-driven config with validation |
| `internal/log/log.go` | `slog` setup + bounded ring buffer (used later by GUI) |
| `internal/store/db.go` | Open sqlite, run migrations, expose `*sql.DB` |
| `internal/store/crypto.go` | Age encrypt/decrypt for `sessions.ciphertext` |
| `internal/store/queries/*.sql` | sqlc input |
| `internal/store/gen/*.go` | sqlc output (committed) |
| `migrations/0001_init.sql` | Initial schema |
| `migrations/0002_seed_fake_account.sql` | Dev-only seed (gated by env) |
| `internal/platform/platform.go` | `Backend` interface + domain types (`Campaign`, `Stream`, `Session`, `DropBenefit`, `Progress`, `WatchHandle`, `DeviceChallenge`, `BrowserRPC`) |
| `internal/platform/fake/fake.go` | Deterministic in-memory backend |
| `internal/watcher/state.go` | `State` enum, transition rules |
| `internal/watcher/watcher.go` | State machine loop driven by ticker + ctx |
| `internal/scheduler/scheduler.go` | Per-account supervisor; spawn / restart / shutdown |
| `internal/notify/notify.go` | `Notifier` interface + `Event` types |
| `internal/notify/noop.go` | `NoopNotifier` (logs only) |
| `internal/api/server.go` | Minimal HTTP server (`/healthz` only this plan) |
| `sqlc.yaml` | sqlc config |
| `deploy/Dockerfile.miner` | Multi-stage build, distroless runtime |
| `deploy/docker-compose.yml` | Local-testing compose with volume |
| `.env.example` | Documented env vars |

---

## Task 0: Initial commit of design doc

**Files:**
- Existing: `docs/superpowers/specs/2026-06-04-rust-drops-miner-design.md`
- Existing: `docs/superpowers/plans/2026-06-04-plan-01-foundation-vertical-slice.md`

- [ ] **Step 1: Stage docs and create initial commit**

```bash
git add docs/
git commit -m "docs: add design spec and plan 1 (foundation + vertical slice)"
```

Expected: commit succeeds on `master`.

---

## Task 1: Module init and gitignore

**Files:**
- Create: `go.mod`
- Create: `.gitignore`
- Create: `README.md` (one-paragraph stub, since this is the only marker the repo isn't empty)

- [ ] **Step 1: Initialize Go module**

```bash
go mod init github.com/chano-fernandez/rust-drops-miner
```

Expected: creates `go.mod` with `module github.com/chano-fernandez/rust-drops-miner` and `go 1.22` (or newer).

- [ ] **Step 2: Create `.gitignore`**

```gitignore
# binaries
/miner
/browser-sidecar
/bin/

# local data
/data/
*.db
*.db-wal
*.db-shm

# env
.env
!.env.example

# tooling
.DS_Store
*.swp
*.swo

# coverage
coverage.out
```

- [ ] **Step 3: Create minimal `README.md`**

```markdown
# rust-drops-miner

Headless drops miner for Twitch and Kick, focused on the game Rust. See `docs/superpowers/specs/2026-06-04-rust-drops-miner-design.md` for design.
```

- [ ] **Step 4: Commit**

```bash
git add go.mod .gitignore README.md
git commit -m "chore: init go module and project skeleton"
```

---

## Task 2: Config package

**Files:**
- Create: `internal/config/config.go`
- Test: `internal/config/config_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/config/config_test.go
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

	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, "0.0.0.0:8080", cfg.HTTPAddr)
	assert.Equal(t, "/data/miner.db", cfg.DBPath)
	assert.Equal(t, false, cfg.SeedFakeAccount)
}

func TestLoad_Overrides(t *testing.T) {
	t.Setenv("MINER_MASTER_KEY", "AGE-SECRET-KEY-1XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX0")
	t.Setenv("MINER_HTTP_ADDR", "127.0.0.1:9000")
	t.Setenv("MINER_DB_PATH", "/tmp/m.db")
	t.Setenv("MINER_SEED_FAKE_ACCOUNT", "true")

	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, "127.0.0.1:9000", cfg.HTTPAddr)
	assert.Equal(t, "/tmp/m.db", cfg.DBPath)
	assert.True(t, cfg.SeedFakeAccount)
}
```

- [ ] **Step 2: Add testify dependency and run test (expect compile failure)**

```bash
go get github.com/stretchr/testify@v1.9.0
go test ./internal/config/...
```

Expected: fails — `Load` undefined.

- [ ] **Step 3: Implement `Load`**

```go
// internal/config/config.go
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	HTTPAddr        string
	DBPath          string
	MasterKey       string
	SeedFakeAccount bool
}

func Load() (Config, error) {
	cfg := Config{
		HTTPAddr:        getenv("MINER_HTTP_ADDR", "0.0.0.0:8080"),
		DBPath:          getenv("MINER_DB_PATH", "/data/miner.db"),
		MasterKey:       os.Getenv("MINER_MASTER_KEY"),
		SeedFakeAccount: parseBool(os.Getenv("MINER_SEED_FAKE_ACCOUNT")),
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

func parseBool(s string) bool {
	if s == "" {
		return false
	}
	b, _ := strconv.ParseBool(s)
	return b
}
```

- [ ] **Step 4: Run tests**

```bash
go mod tidy
go test ./internal/config/... -v
```

Expected: 3 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add go.mod go.sum internal/config/
git commit -m "feat(config): env-based config loader with master key requirement"
```

---

## Task 3: Logging package

**Files:**
- Create: `internal/log/log.go`
- Test: `internal/log/log_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/log/log_test.go
package log

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew_WritesJSONToWriter(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf, "info")
	l.Info("hello", "account", "acc1")

	out := buf.String()
	assert.Contains(t, out, `"msg":"hello"`)
	assert.Contains(t, out, `"account":"acc1"`)
	assert.Contains(t, out, `"level":"INFO"`)
}

func TestRingBuffer_KeepsLastN(t *testing.T) {
	rb := NewRing(3)
	for i := 0; i < 5; i++ {
		rb.Push(LogLine{Msg: "m"})
	}
	require.Equal(t, 3, len(rb.Snapshot()))
}

func TestNewWithRing_WritesToBoth(t *testing.T) {
	var buf bytes.Buffer
	rb := NewRing(10)
	l := NewWithRing(&buf, "debug", rb)
	l.Info("ping")

	assert.True(t, strings.Contains(buf.String(), "ping"))
	assert.Equal(t, 1, len(rb.Snapshot()))
	assert.Equal(t, "ping", rb.Snapshot()[0].Msg)
}
```

- [ ] **Step 2: Run test (expect failure)**

```bash
go test ./internal/log/...
```

Expected: compile failure — `New`, `NewRing`, `LogLine`, `NewWithRing` undefined.

- [ ] **Step 3: Implement logger + ring buffer**

```go
// internal/log/log.go
package log

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"time"
)

type LogLine struct {
	TS      time.Time      `json:"ts"`
	Level   string         `json:"level"`
	Msg     string         `json:"msg"`
	Fields  map[string]any `json:"fields,omitempty"`
}

type Ring struct {
	mu    sync.Mutex
	buf   []LogLine
	size  int
	next  int
	count int
}

func NewRing(size int) *Ring {
	return &Ring{buf: make([]LogLine, size), size: size}
}

func (r *Ring) Push(l LogLine) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buf[r.next] = l
	r.next = (r.next + 1) % r.size
	if r.count < r.size {
		r.count++
	}
}

func (r *Ring) Snapshot() []LogLine {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]LogLine, 0, r.count)
	start := (r.next - r.count + r.size) % r.size
	for i := 0; i < r.count; i++ {
		out = append(out, r.buf[(start+i)%r.size])
	}
	return out
}

func New(w io.Writer, level string) *slog.Logger {
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: parseLevel(level)}))
}

func NewWithRing(w io.Writer, level string, r *Ring) *slog.Logger {
	base := slog.NewJSONHandler(w, &slog.HandlerOptions{Level: parseLevel(level)})
	return slog.New(&ringHandler{inner: base, ring: r})
}

func parseLevel(s string) slog.Level {
	switch s {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

type ringHandler struct {
	inner slog.Handler
	ring  *Ring
}

func (h *ringHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.inner.Enabled(ctx, l)
}

func (h *ringHandler) Handle(ctx context.Context, rec slog.Record) error {
	fields := map[string]any{}
	rec.Attrs(func(a slog.Attr) bool {
		fields[a.Key] = a.Value.Any()
		return true
	})
	h.ring.Push(LogLine{
		TS:     rec.Time,
		Level:  rec.Level.String(),
		Msg:    rec.Message,
		Fields: fields,
	})
	return h.inner.Handle(ctx, rec)
}

func (h *ringHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &ringHandler{inner: h.inner.WithAttrs(attrs), ring: h.ring}
}

func (h *ringHandler) WithGroup(name string) slog.Handler {
	return &ringHandler{inner: h.inner.WithGroup(name), ring: h.ring}
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/log/... -v
```

Expected: 3 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/log/
git commit -m "feat(log): slog wrapper with bounded ring buffer for later GUI tail"
```

---

## Task 4: Migrations + sqlite open

**Files:**
- Create: `internal/store/migrations/0001_init.sql` (note: under the package so `go:embed` can reach it)
- Create: `internal/store/db.go`
- Test: `internal/store/db_test.go`

- [ ] **Step 1: Add goose + sqlite driver deps**

```bash
go get github.com/pressly/goose/v3@v3.20.0
go get modernc.org/sqlite@v1.29.0
```

- [ ] **Step 2: Write migration**

```sql
-- internal/store/migrations/0001_init.sql
-- +goose Up
-- +goose StatementBegin
CREATE TABLE accounts (
    id              TEXT PRIMARY KEY,
    platform        TEXT NOT NULL,
    login           TEXT NOT NULL,
    display_name    TEXT NOT NULL DEFAULT '',
    status          TEXT NOT NULL DEFAULT 'idle',
    proxy_url       TEXT,
    webhook_url     TEXT,
    fingerprint_json TEXT NOT NULL DEFAULT '{}',
    enabled         INTEGER NOT NULL DEFAULT 1,
    created_at      INTEGER NOT NULL,
    updated_at      INTEGER NOT NULL,
    UNIQUE(platform, login)
);

CREATE TABLE sessions (
    account_id  TEXT PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
    ciphertext  BLOB NOT NULL,
    expires_at  INTEGER NOT NULL
);

CREATE TABLE campaigns (
    id              TEXT PRIMARY KEY,
    platform        TEXT NOT NULL,
    game            TEXT NOT NULL,
    name            TEXT NOT NULL,
    starts_at       INTEGER NOT NULL,
    ends_at         INTEGER NOT NULL,
    status          TEXT NOT NULL,
    raw_json        TEXT NOT NULL DEFAULT '{}',
    discovered_at   INTEGER NOT NULL
);

CREATE TABLE benefits (
    id                TEXT PRIMARY KEY,
    campaign_id       TEXT NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
    name              TEXT NOT NULL,
    required_minutes  INTEGER NOT NULL,
    image_url         TEXT NOT NULL DEFAULT ''
);

CREATE TABLE progress (
    account_id       TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    benefit_id       TEXT NOT NULL REFERENCES benefits(id) ON DELETE CASCADE,
    minutes_watched  INTEGER NOT NULL DEFAULT 0,
    claimed_at       INTEGER,
    updated_at       INTEGER NOT NULL,
    PRIMARY KEY (account_id, benefit_id)
);

CREATE TABLE claims (
    id               TEXT PRIMARY KEY,
    account_id       TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    benefit_id       TEXT NOT NULL REFERENCES benefits(id) ON DELETE CASCADE,
    claimed_at       INTEGER NOT NULL,
    value_meta_json  TEXT NOT NULL DEFAULT '{}'
);

CREATE TABLE campaign_priorities (
    account_id   TEXT REFERENCES accounts(id) ON DELETE CASCADE,
    campaign_id  TEXT NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
    rank         INTEGER NOT NULL,
    PRIMARY KEY (account_id, campaign_id)
);

CREATE TABLE games (
    id        TEXT PRIMARY KEY,
    name      TEXT NOT NULL UNIQUE,
    slug      TEXT NOT NULL UNIQUE,
    priority  INTEGER NOT NULL DEFAULT 100
);

CREATE TABLE notifications (
    id           TEXT PRIMARY KEY,
    account_id   TEXT REFERENCES accounts(id) ON DELETE SET NULL,
    kind         TEXT NOT NULL,
    payload_json TEXT NOT NULL,
    status       TEXT NOT NULL DEFAULT 'pending',
    created_at   INTEGER NOT NULL,
    sent_at      INTEGER
);

CREATE TABLE logs (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    ts           INTEGER NOT NULL,
    level        TEXT NOT NULL,
    account_id   TEXT,
    msg          TEXT NOT NULL,
    fields_json  TEXT NOT NULL DEFAULT '{}'
);
CREATE INDEX idx_logs_ts ON logs(ts);

CREATE TABLE kv (
    key    TEXT PRIMARY KEY,
    value  BLOB NOT NULL
);

CREATE TABLE admin (
    id            INTEGER PRIMARY KEY CHECK (id = 1),
    password_hash TEXT NOT NULL,
    created_at    INTEGER NOT NULL
);

INSERT INTO games (id, name, slug, priority) VALUES ('g_rust', 'Rust', 'rust', 0);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE admin;
DROP TABLE kv;
DROP TABLE logs;
DROP TABLE notifications;
DROP TABLE games;
DROP TABLE campaign_priorities;
DROP TABLE claims;
DROP TABLE progress;
DROP TABLE benefits;
DROP TABLE campaigns;
DROP TABLE sessions;
DROP TABLE accounts;
-- +goose StatementEnd
```

- [ ] **Step 3: Write failing test**

```go
// internal/store/db_test.go
package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpen_RunsMigrations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := Open(context.Background(), path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	row := db.QueryRowContext(context.Background(),
		`SELECT name FROM sqlite_master WHERE type='table' AND name='accounts'`)
	var name string
	require.NoError(t, row.Scan(&name))
	assert.Equal(t, "accounts", name)
}

func TestOpen_SeedsRustGame(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := Open(context.Background(), path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	row := db.QueryRowContext(context.Background(),
		`SELECT priority FROM games WHERE slug='rust'`)
	var p int
	require.NoError(t, row.Scan(&p))
	assert.Equal(t, 0, p)
}
```

- [ ] **Step 4: Implement `store.Open`**

```go
// internal/store/db.go
package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

func Open(ctx context.Context, path string) (*sql.DB, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}

	goose.SetBaseFS(migrationsFS)
	if err := goose.SetDialect("sqlite3"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("set goose dialect: %w", err)
	}
	if err := goose.UpContext(ctx, db, "migrations"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return db, nil
}
```

Note: `go:embed` cannot use `..` to escape the source file's directory tree. Migrations live at `internal/store/migrations/*.sql` so the embed pattern is `migrations/*.sql` relative to `db.go`. The seed migration in Task 13 must also land in `internal/store/migrations/`.

- [ ] **Step 5: Run tests**

```bash
go mod tidy
go test ./internal/store/... -v
```

Expected: both PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/store/ go.mod go.sum
git commit -m "feat(store): sqlite open with embedded goose migrations and seed"
```

---

## Task 5: sqlc query layer

**Files:**
- Create: `sqlc.yaml`
- Create: `internal/store/queries/accounts.sql`
- Create: `internal/store/queries/campaigns.sql`
- Create: `internal/store/queries/progress.sql`
- Create: `internal/store/queries/sessions.sql`
- Generated: `internal/store/gen/*.go` (committed)
- Test: `internal/store/queries_test.go`

- [ ] **Step 1: Install sqlc (one-time, locally)**

```bash
go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.26.0
```

- [ ] **Step 2: Write sqlc config**

```yaml
# sqlc.yaml
version: "2"
sql:
  - engine: "sqlite"
    queries: "internal/store/queries"
    schema: "internal/store/migrations"
    gen:
      go:
        package: "gen"
        out: "internal/store/gen"
        emit_json_tags: true
        emit_prepared_queries: false
        emit_interface: true
```

- [ ] **Step 3: Write query files**

```sql
-- internal/store/queries/accounts.sql

-- name: CreateAccount :one
INSERT INTO accounts (id, platform, login, display_name, status, proxy_url, webhook_url, fingerprint_json, enabled, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetAccount :one
SELECT * FROM accounts WHERE id = ?;

-- name: ListEnabledAccounts :many
SELECT * FROM accounts WHERE enabled = 1 ORDER BY created_at ASC;

-- name: UpdateAccountStatus :exec
UPDATE accounts SET status = ?, updated_at = ? WHERE id = ?;

-- name: SetAccountEnabled :exec
UPDATE accounts SET enabled = ?, updated_at = ? WHERE id = ?;
```

```sql
-- internal/store/queries/campaigns.sql

-- name: UpsertCampaign :exec
INSERT INTO campaigns (id, platform, game, name, starts_at, ends_at, status, raw_json, discovered_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    name = excluded.name,
    starts_at = excluded.starts_at,
    ends_at = excluded.ends_at,
    status = excluded.status,
    raw_json = excluded.raw_json;

-- name: UpsertBenefit :exec
INSERT INTO benefits (id, campaign_id, name, required_minutes, image_url)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    name = excluded.name,
    required_minutes = excluded.required_minutes,
    image_url = excluded.image_url;

-- name: ListActiveCampaignsForPlatform :many
SELECT * FROM campaigns
WHERE platform = ? AND status = 'active' AND starts_at <= ? AND ends_at >= ?
ORDER BY discovered_at DESC;

-- name: ListBenefitsForCampaign :many
SELECT * FROM benefits WHERE campaign_id = ?;
```

```sql
-- internal/store/queries/progress.sql

-- name: UpsertProgress :exec
INSERT INTO progress (account_id, benefit_id, minutes_watched, claimed_at, updated_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(account_id, benefit_id) DO UPDATE SET
    minutes_watched = excluded.minutes_watched,
    claimed_at = COALESCE(excluded.claimed_at, progress.claimed_at),
    updated_at = excluded.updated_at;

-- name: GetProgress :one
SELECT * FROM progress WHERE account_id = ? AND benefit_id = ?;

-- name: ListUnclaimedProgressForAccount :many
SELECT p.* FROM progress p
JOIN benefits b ON b.id = p.benefit_id
JOIN campaigns c ON c.id = b.campaign_id
WHERE p.account_id = ?
  AND p.claimed_at IS NULL
  AND c.status = 'active'
  AND c.starts_at <= ?
  AND c.ends_at >= ?;

-- name: InsertClaim :exec
INSERT INTO claims (id, account_id, benefit_id, claimed_at, value_meta_json)
VALUES (?, ?, ?, ?, ?);
```

```sql
-- internal/store/queries/sessions.sql

-- name: UpsertSession :exec
INSERT INTO sessions (account_id, ciphertext, expires_at)
VALUES (?, ?, ?)
ON CONFLICT(account_id) DO UPDATE SET
    ciphertext = excluded.ciphertext,
    expires_at = excluded.expires_at;

-- name: GetSession :one
SELECT * FROM sessions WHERE account_id = ?;
```

- [ ] **Step 4: Generate sqlc code**

```bash
sqlc generate
```

Expected: writes files to `internal/store/gen/`.

- [ ] **Step 5: Write test exercising generated queries**

```go
// internal/store/queries_test.go
package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chano-fernandez/rust-drops-miner/internal/store/gen"
)

func openTest(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "t.db")
	db, err := Open(context.Background(), path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestQueries_AccountRoundtrip(t *testing.T) {
	db := openTest(t)
	q := gen.New(db)
	now := time.Now().Unix()

	acc, err := q.CreateAccount(context.Background(), gen.CreateAccountParams{
		ID:              "acc1",
		Platform:        "fake",
		Login:           "user1",
		DisplayName:     "User One",
		Status:          "idle",
		FingerprintJson: "{}",
		Enabled:         1,
		CreatedAt:       now,
		UpdatedAt:       now,
	})
	require.NoError(t, err)
	assert.Equal(t, "acc1", acc.ID)

	list, err := q.ListEnabledAccounts(context.Background())
	require.NoError(t, err)
	assert.Len(t, list, 1)
}
```

- [ ] **Step 6: Run tests**

```bash
go mod tidy
go test ./internal/store/... -v
```

Expected: all PASS.

- [ ] **Step 7: Commit**

```bash
git add sqlc.yaml internal/store/queries/ internal/store/gen/ internal/store/queries_test.go
git commit -m "feat(store): sqlc-generated queries for accounts, sessions, campaigns, progress"
```

---

## Task 6: Age-encrypted session blob helper

**Files:**
- Create: `internal/store/crypto.go`
- Test: `internal/store/crypto_test.go`

- [ ] **Step 1: Add age dep**

```bash
go get filippo.io/age@v1.2.0
```

- [ ] **Step 2: Write failing test**

```go
// internal/store/crypto_test.go
package store

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Generated once with `age-keygen` for tests; this is a throwaway key.
const testKey = "AGE-SECRET-KEY-1GFPYYSJZGFPYYSJZGFPYYSJZGFPYYSJZGFPYYSJZGFPYYSJZGFQ4J0LLN"

func TestCrypto_RoundTrip(t *testing.T) {
	c, err := NewCryptor(testKey)
	require.NoError(t, err)

	ct, err := c.Encrypt([]byte("hello"))
	require.NoError(t, err)
	assert.NotEmpty(t, ct)

	pt, err := c.Decrypt(ct)
	require.NoError(t, err)
	assert.Equal(t, []byte("hello"), pt)
}

func TestCrypto_RejectsBadKey(t *testing.T) {
	_, err := NewCryptor("not-a-key")
	require.Error(t, err)
}
```

> If the test key fails decoding (age key format strict), generate a real one once via `go run filippo.io/age/cmd/age-keygen` and paste the secret into the test. The point of the test is round-trip and rejection, not a fixed value.

- [ ] **Step 3: Implement crypto helper**

```go
// internal/store/crypto.go
package store

import (
	"bytes"
	"fmt"
	"io"

	"filippo.io/age"
)

type Cryptor struct {
	identity  *age.X25519Identity
	recipient *age.X25519Recipient
}

func NewCryptor(secret string) (*Cryptor, error) {
	id, err := age.ParseX25519Identity(secret)
	if err != nil {
		return nil, fmt.Errorf("parse age identity: %w", err)
	}
	return &Cryptor{
		identity:  id,
		recipient: id.Recipient(),
	}, nil
}

func (c *Cryptor) Encrypt(plain []byte) ([]byte, error) {
	var buf bytes.Buffer
	w, err := age.Encrypt(&buf, c.recipient)
	if err != nil {
		return nil, fmt.Errorf("age encrypt: %w", err)
	}
	if _, err := w.Write(plain); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (c *Cryptor) Decrypt(ct []byte) ([]byte, error) {
	r, err := age.Decrypt(bytes.NewReader(ct), c.identity)
	if err != nil {
		return nil, fmt.Errorf("age decrypt: %w", err)
	}
	return io.ReadAll(r)
}
```

- [ ] **Step 4: Run tests**

```bash
go mod tidy
go test ./internal/store/... -run TestCrypto -v
```

Expected: both crypto tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store/crypto.go internal/store/crypto_test.go go.mod go.sum
git commit -m "feat(store): age-encrypted session blob helper"
```

---

## Task 7: Platform interface + domain types

**Files:**
- Create: `internal/platform/platform.go`
- Create: `internal/platform/types.go`

- [ ] **Step 1: Write types**

```go
// internal/platform/types.go
package platform

import "time"

type Session struct {
	AccessToken  string            `json:"access_token,omitempty"`
	RefreshToken string            `json:"refresh_token,omitempty"`
	Cookies      map[string]string `json:"cookies,omitempty"`
	CSRF         string            `json:"csrf,omitempty"`
	ExpiresAt    time.Time         `json:"expires_at"`
	Fingerprint  string            `json:"fingerprint,omitempty"`
}

type Campaign struct {
	ID        string
	Platform  string
	Game      string
	Name      string
	StartsAt  time.Time
	EndsAt    time.Time
	Status    string
	Benefits  []DropBenefit
}

type DropBenefit struct {
	ID              string
	CampaignID      string
	Name            string
	RequiredMinutes int
	ImageURL        string
}

type Stream struct {
	Channel      string
	ViewerCount  int
	DropsEnabled bool
}

type Progress struct {
	BenefitID       string
	MinutesWatched  int
	Claimed         bool
}

type WatchHandle struct {
	Channel    string
	Internal   any // backend-specific opaque state
}

type DeviceChallenge struct {
	UserCode        string
	VerificationURL string
	ExpiresAt       time.Time
	Interval        time.Duration
	Internal        any
}

// BrowserRPC is the minimal contract the auth flow uses to talk to the sidecar.
// Implemented in internal/auth/browser; passed in here to keep platform/* free
// of network deps. In Plan 1 only FakeBackend exists and never calls this.
type BrowserRPC interface {
	LoginInteractive(platform string) (Session, error)
}
```

- [ ] **Step 2: Write the `Backend` interface**

```go
// internal/platform/platform.go
package platform

import "context"

type Backend interface {
	Name() string

	// Auth
	StartDeviceLogin(ctx context.Context) (DeviceChallenge, error)
	PollDeviceLogin(ctx context.Context, ch DeviceChallenge) (Session, error)
	LoginViaBrowser(ctx context.Context, rpc BrowserRPC) (Session, error)
	RefreshSession(ctx context.Context, s Session) (Session, error)

	// Discovery
	ListActiveCampaigns(ctx context.Context, s Session) ([]Campaign, error)
	ListEligibleChannels(ctx context.Context, s Session, c Campaign) ([]Stream, error)
	InventoryProgress(ctx context.Context, s Session) ([]Progress, error)

	// Mining
	StartWatch(ctx context.Context, s Session, stream Stream) (WatchHandle, error)
	Heartbeat(ctx context.Context, h WatchHandle) error
	StopWatch(ctx context.Context, h WatchHandle) error
	Claim(ctx context.Context, s Session, b DropBenefit) error
}

type Registry struct {
	backends map[string]Backend
}

func NewRegistry() *Registry {
	return &Registry{backends: map[string]Backend{}}
}

func (r *Registry) Register(b Backend) {
	r.backends[b.Name()] = b
}

func (r *Registry) Get(name string) (Backend, bool) {
	b, ok := r.backends[name]
	return b, ok
}
```

- [ ] **Step 3: Verify compilation**

```bash
go build ./internal/platform/...
```

Expected: no output, exit 0.

- [ ] **Step 4: Commit**

```bash
git add internal/platform/
git commit -m "feat(platform): Backend interface and domain types"
```

---

## Task 8: FakeBackend implementation

**Files:**
- Create: `internal/platform/fake/fake.go`
- Test: `internal/platform/fake/fake_test.go`

- [ ] **Step 1: Write failing test**

```go
// internal/platform/fake/fake_test.go
package fake

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chano-fernandez/rust-drops-miner/internal/platform"
)

func TestFake_LifecycleClaims(t *testing.T) {
	ctx := context.Background()
	b := New(WithFastTime())

	challenge, err := b.StartDeviceLogin(ctx)
	require.NoError(t, err)

	sess, err := b.PollDeviceLogin(ctx, challenge)
	require.NoError(t, err)
	assert.NotEmpty(t, sess.AccessToken)

	campaigns, err := b.ListActiveCampaigns(ctx, sess)
	require.NoError(t, err)
	require.NotEmpty(t, campaigns)
	require.NotEmpty(t, campaigns[0].Benefits)

	streams, err := b.ListEligibleChannels(ctx, sess, campaigns[0])
	require.NoError(t, err)
	require.NotEmpty(t, streams)

	h, err := b.StartWatch(ctx, sess, streams[0])
	require.NoError(t, err)

	for i := 0; i < campaigns[0].Benefits[0].RequiredMinutes; i++ {
		require.NoError(t, b.Heartbeat(ctx, h))
	}

	progress, err := b.InventoryProgress(ctx, sess)
	require.NoError(t, err)
	require.NotEmpty(t, progress)
	assert.Equal(t, campaigns[0].Benefits[0].RequiredMinutes, progress[0].MinutesWatched)

	require.NoError(t, b.Claim(ctx, sess, campaigns[0].Benefits[0]))
	require.NoError(t, b.StopWatch(ctx, h))

	_ = platform.Session{} // import anchor
}
```

- [ ] **Step 2: Implement FakeBackend**

```go
// internal/platform/fake/fake.go
package fake

import (
	"context"
	"sync"
	"time"

	"github.com/chano-fernandez/rust-drops-miner/internal/platform"
)

type Option func(*Backend)

func WithFastTime() Option {
	return func(b *Backend) { b.fast = true }
}

type Backend struct {
	mu       sync.Mutex
	fast     bool
	progress map[string]int   // benefit_id -> minutes
	claims   map[string]time.Time
	handles  map[string]platform.Stream
}

func New(opts ...Option) *Backend {
	b := &Backend{
		progress: map[string]int{},
		claims:   map[string]time.Time{},
		handles:  map[string]platform.Stream{},
	}
	for _, o := range opts {
		o(b)
	}
	return b
}

func (b *Backend) Name() string { return "fake" }

func (b *Backend) StartDeviceLogin(ctx context.Context) (platform.DeviceChallenge, error) {
	return platform.DeviceChallenge{
		UserCode:        "FAKE-CODE",
		VerificationURL: "https://example.invalid/device",
		ExpiresAt:       time.Now().Add(5 * time.Minute),
		Interval:        100 * time.Millisecond,
	}, nil
}

func (b *Backend) PollDeviceLogin(ctx context.Context, ch platform.DeviceChallenge) (platform.Session, error) {
	return platform.Session{
		AccessToken: "fake-access",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
	}, nil
}

func (b *Backend) LoginViaBrowser(ctx context.Context, rpc platform.BrowserRPC) (platform.Session, error) {
	return platform.Session{AccessToken: "fake-browser", ExpiresAt: time.Now().Add(1 * time.Hour)}, nil
}

func (b *Backend) RefreshSession(ctx context.Context, s platform.Session) (platform.Session, error) {
	s.ExpiresAt = time.Now().Add(1 * time.Hour)
	return s, nil
}

func (b *Backend) ListActiveCampaigns(ctx context.Context, s platform.Session) ([]platform.Campaign, error) {
	now := time.Now()
	required := 5
	if b.fast {
		required = 2
	}
	return []platform.Campaign{
		{
			ID: "camp_fake_rust_1", Platform: "fake", Game: "Rust",
			Name: "Fake Rust Drops", Status: "active",
			StartsAt: now.Add(-1 * time.Hour), EndsAt: now.Add(24 * time.Hour),
			Benefits: []platform.DropBenefit{
				{ID: "ben_fake_helmet", CampaignID: "camp_fake_rust_1", Name: "Fake Helmet", RequiredMinutes: required, ImageURL: ""},
			},
		},
	}, nil
}

func (b *Backend) ListEligibleChannels(ctx context.Context, s platform.Session, c platform.Campaign) ([]platform.Stream, error) {
	return []platform.Stream{
		{Channel: "fakestreamer", ViewerCount: 9001, DropsEnabled: true},
	}, nil
}

func (b *Backend) InventoryProgress(ctx context.Context, s platform.Session) ([]platform.Progress, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := []platform.Progress{}
	for benefitID, mins := range b.progress {
		_, claimed := b.claims[benefitID]
		out = append(out, platform.Progress{
			BenefitID: benefitID, MinutesWatched: mins, Claimed: claimed,
		})
	}
	return out, nil
}

func (b *Backend) StartWatch(ctx context.Context, s platform.Session, stream platform.Stream) (platform.WatchHandle, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handles[stream.Channel] = stream
	return platform.WatchHandle{Channel: stream.Channel}, nil
}

func (b *Backend) Heartbeat(ctx context.Context, h platform.WatchHandle) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	// Hard-coded to the single fake benefit; sufficient for the slice.
	b.progress["ben_fake_helmet"]++
	return nil
}

func (b *Backend) StopWatch(ctx context.Context, h platform.WatchHandle) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.handles, h.Channel)
	return nil
}

func (b *Backend) Claim(ctx context.Context, s platform.Session, ben platform.DropBenefit) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.claims[ben.ID] = time.Now()
	return nil
}
```

- [ ] **Step 3: Run tests**

```bash
go test ./internal/platform/... -v
```

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/platform/fake/
git commit -m "feat(platform/fake): deterministic in-memory backend for vertical slice"
```

---

## Task 9: Watcher state machine

**Files:**
- Create: `internal/watcher/state.go`
- Create: `internal/watcher/watcher.go`
- Test: `internal/watcher/watcher_test.go`

- [ ] **Step 1: Write failing test (full lifecycle against FakeBackend)**

```go
// internal/watcher/watcher_test.go
package watcher

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chano-fernandez/rust-drops-miner/internal/platform"
	"github.com/chano-fernandez/rust-drops-miner/internal/platform/fake"
)

type recordingNotifier struct{ events []string }

func (r *recordingNotifier) Notify(_ context.Context, ev string, _ map[string]any) error {
	r.events = append(r.events, ev)
	return nil
}

func TestWatcher_MinesUntilClaim(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	backend := fake.New(fake.WithFastTime()) // RequiredMinutes = 2
	notif := &recordingNotifier{}

	sess, err := backend.PollDeviceLogin(ctx, platform.DeviceChallenge{})
	require.NoError(t, err)

	w := New(Config{
		AccountID:     "acc1",
		Backend:       backend,
		Session:       sess,
		Notifier:      notif,
		TickInterval:  5 * time.Millisecond, // very fast for tests
	})

	err = w.Run(ctx)
	require.NoError(t, err)

	assert.Contains(t, notif.events, "claim")
	// After claim, watcher goes Idle → pickCampaign finds nothing → Sleeping → errComplete.
	// Run returns with final state = StateSleeping.
	assert.Equal(t, StateSleeping, w.State())
}
```

- [ ] **Step 2: Define states and transitions**

```go
// internal/watcher/state.go
package watcher

type State int

const (
	StateIdle State = iota
	StatePickCampaign
	StatePickStream
	StateWatching
	StateClaiming
	StateSleeping
	StateAuthRequired
	StatePaused
)

func (s State) String() string {
	switch s {
	case StateIdle:
		return "idle"
	case StatePickCampaign:
		return "pick_campaign"
	case StatePickStream:
		return "pick_stream"
	case StateWatching:
		return "watching"
	case StateClaiming:
		return "claiming"
	case StateSleeping:
		return "sleeping"
	case StateAuthRequired:
		return "auth_required"
	case StatePaused:
		return "paused"
	default:
		return "unknown"
	}
}
```

- [ ] **Step 3: Implement the state machine**

```go
// internal/watcher/watcher.go
package watcher

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/chano-fernandez/rust-drops-miner/internal/platform"
)

type Notifier interface {
	Notify(ctx context.Context, event string, fields map[string]any) error
}

type Config struct {
	AccountID    string
	Backend      platform.Backend
	Session      platform.Session
	Notifier     Notifier
	TickInterval time.Duration
}

type Watcher struct {
	cfg Config

	mu    sync.Mutex
	state State

	currentCampaign *platform.Campaign
	currentBenefit  *platform.DropBenefit
	currentStream   *platform.Stream
	handle          *platform.WatchHandle
}

func New(cfg Config) *Watcher {
	if cfg.TickInterval == 0 {
		cfg.TickInterval = time.Minute
	}
	return &Watcher{cfg: cfg, state: StateIdle}
}

func (w *Watcher) State() State {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.state
}

func (w *Watcher) setState(s State) {
	w.mu.Lock()
	w.state = s
	w.mu.Unlock()
	_ = w.cfg.Notifier.Notify(context.Background(), "state", map[string]any{
		"account": w.cfg.AccountID, "state": s.String(),
	})
}

func (w *Watcher) Run(ctx context.Context) error {
	t := time.NewTicker(w.cfg.TickInterval)
	defer t.Stop()

	for {
		if err := w.step(ctx); err != nil {
			if errors.Is(err, errComplete) {
				return nil
			}
			return err
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
		}
	}
}

var errComplete = errors.New("nothing left to mine")

func (w *Watcher) step(ctx context.Context) error {
	switch w.State() {
	case StateIdle, StatePickCampaign:
		return w.pickCampaign(ctx)
	case StatePickStream:
		return w.pickStream(ctx)
	case StateWatching:
		return w.tickWatch(ctx)
	case StateClaiming:
		return w.claim(ctx)
	case StateSleeping:
		// In Plan 1 with FakeBackend there's only one campaign; "sleeping"
		// means nothing more to do — exit the run loop.
		return errComplete
	case StateAuthRequired, StatePaused:
		return errComplete
	default:
		return fmt.Errorf("unknown state %s", w.State())
	}
}

func (w *Watcher) pickCampaign(ctx context.Context) error {
	campaigns, err := w.cfg.Backend.ListActiveCampaigns(ctx, w.cfg.Session)
	if err != nil {
		return fmt.Errorf("list campaigns: %w", err)
	}
	progress, err := w.cfg.Backend.InventoryProgress(ctx, w.cfg.Session)
	if err != nil {
		return fmt.Errorf("inventory: %w", err)
	}
	claimed := map[string]bool{}
	for _, p := range progress {
		if p.Claimed {
			claimed[p.BenefitID] = true
		}
	}

	for _, c := range campaigns {
		for _, b := range c.Benefits {
			if claimed[b.ID] {
				continue
			}
			campaignCopy, benefitCopy := c, b
			w.mu.Lock()
			w.currentCampaign = &campaignCopy
			w.currentBenefit = &benefitCopy
			w.mu.Unlock()
			w.setState(StatePickStream)
			return nil
		}
	}
	w.setState(StateSleeping)
	return nil
}

func (w *Watcher) pickStream(ctx context.Context) error {
	w.mu.Lock()
	camp := *w.currentCampaign
	w.mu.Unlock()

	streams, err := w.cfg.Backend.ListEligibleChannels(ctx, w.cfg.Session, camp)
	if err != nil {
		return fmt.Errorf("list channels: %w", err)
	}
	if len(streams) == 0 {
		w.setState(StateSleeping)
		return nil
	}
	s := streams[0]
	h, err := w.cfg.Backend.StartWatch(ctx, w.cfg.Session, s)
	if err != nil {
		return fmt.Errorf("start watch: %w", err)
	}
	w.mu.Lock()
	w.currentStream = &s
	w.handle = &h
	w.mu.Unlock()
	w.setState(StateWatching)
	return nil
}

func (w *Watcher) tickWatch(ctx context.Context) error {
	w.mu.Lock()
	handle := *w.handle
	benefit := *w.currentBenefit
	w.mu.Unlock()

	if err := w.cfg.Backend.Heartbeat(ctx, handle); err != nil {
		return fmt.Errorf("heartbeat: %w", err)
	}

	progress, err := w.cfg.Backend.InventoryProgress(ctx, w.cfg.Session)
	if err != nil {
		return fmt.Errorf("inventory: %w", err)
	}
	for _, p := range progress {
		if p.BenefitID == benefit.ID && p.MinutesWatched >= benefit.RequiredMinutes {
			w.setState(StateClaiming)
			return nil
		}
	}

	_ = w.cfg.Notifier.Notify(ctx, "progress", map[string]any{
		"account": w.cfg.AccountID, "benefit": benefit.ID,
	})
	return nil
}

func (w *Watcher) claim(ctx context.Context) error {
	w.mu.Lock()
	benefit := *w.currentBenefit
	handle := *w.handle
	w.mu.Unlock()

	if err := w.cfg.Backend.Claim(ctx, w.cfg.Session, benefit); err != nil {
		return fmt.Errorf("claim: %w", err)
	}
	_ = w.cfg.Backend.StopWatch(ctx, handle)

	_ = w.cfg.Notifier.Notify(ctx, "claim", map[string]any{
		"account": w.cfg.AccountID, "benefit": benefit.ID,
	})

	w.mu.Lock()
	w.currentBenefit = nil
	w.currentStream = nil
	w.handle = nil
	w.mu.Unlock()

	w.setState(StateIdle)
	return nil
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/watcher/... -v
```

Expected: `TestWatcher_MinesUntilClaim` PASS. `notif.events` contains "claim".

- [ ] **Step 5: Commit**

```bash
git add internal/watcher/
git commit -m "feat(watcher): state machine pick-campaign → watch → claim"
```

---

## Task 10: Notifier interface + noop impl

**Files:**
- Create: `internal/notify/notify.go`
- Create: `internal/notify/noop.go`

- [ ] **Step 1: Write the package**

```go
// internal/notify/notify.go
package notify

import "context"

type Event = string

const (
	EventState    Event = "state"
	EventProgress Event = "progress"
	EventClaim    Event = "claim"
	EventError    Event = "error"
	EventAuth     Event = "auth"
)

type Notifier interface {
	Notify(ctx context.Context, event Event, fields map[string]any) error
}
```

```go
// internal/notify/noop.go
package notify

import (
	"context"
	"log/slog"
)

type NoopNotifier struct {
	Logger *slog.Logger
}

func (n *NoopNotifier) Notify(_ context.Context, event Event, fields map[string]any) error {
	args := []any{"event", event}
	for k, v := range fields {
		args = append(args, k, v)
	}
	n.Logger.Info("notify", args...)
	return nil
}
```

- [ ] **Step 2: Verify build**

```bash
go build ./internal/notify/...
```

Expected: clean.

- [ ] **Step 3: Commit**

```bash
git add internal/notify/
git commit -m "feat(notify): Notifier interface and Noop log-only impl"
```

---

## Task 11: Scheduler (per-account supervisor)

**Files:**
- Create: `internal/scheduler/scheduler.go`
- Test: `internal/scheduler/scheduler_test.go`

- [ ] **Step 1: Write failing test**

```go
// internal/scheduler/scheduler_test.go
package scheduler

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chano-fernandez/rust-drops-miner/internal/platform"
	"github.com/chano-fernandez/rust-drops-miner/internal/platform/fake"
	"github.com/chano-fernandez/rust-drops-miner/internal/watcher"
)

type captureNotifier struct{ claims atomic.Int64 }

func (c *captureNotifier) Notify(_ context.Context, ev string, _ map[string]any) error {
	if ev == "claim" {
		c.claims.Add(1)
	}
	return nil
}

func TestScheduler_RunsMultipleAccountsConcurrently(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	notif := &captureNotifier{}
	s := New(Options{Notifier: notif})

	for i := 0; i < 3; i++ {
		backend := fake.New(fake.WithFastTime())
		sess := platform.Session{AccessToken: "x"}
		w := watcher.New(watcher.Config{
			AccountID:    fmt.Sprintf("acc%d", i),
			Backend:      backend,
			Session:      sess,
			Notifier:     notif,
			TickInterval: 5 * time.Millisecond,
		})
		s.Add(fmt.Sprintf("acc%d", i), w)
	}

	require.NoError(t, s.Start(ctx))
	s.Wait()

	assert.Equal(t, int64(3), notif.claims.Load())
}
```

- [ ] **Step 2: Implement scheduler**

```go
// internal/scheduler/scheduler.go
package scheduler

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/chano-fernandez/rust-drops-miner/internal/notify"
	"github.com/chano-fernandez/rust-drops-miner/internal/watcher"
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
	opts    Options
	mu      sync.Mutex
	entries []entry
	wg      sync.WaitGroup
}

func New(opts Options) *Scheduler {
	return &Scheduler{opts: opts}
}

func (s *Scheduler) Add(id string, w *watcher.Watcher) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = append(s.entries, entry{id: id, runner: w})
}

func (s *Scheduler) Start(ctx context.Context) error {
	s.mu.Lock()
	entries := append([]entry(nil), s.entries...)
	s.mu.Unlock()

	for _, e := range entries {
		s.wg.Add(1)
		go s.supervise(ctx, e)
	}
	return nil
}

func (s *Scheduler) Wait() { s.wg.Wait() }

func (s *Scheduler) supervise(ctx context.Context, e entry) {
	defer s.wg.Done()
	defer func() {
		if r := recover(); r != nil {
			if s.opts.Notifier != nil {
				_ = s.opts.Notifier.Notify(ctx, notify.EventError, map[string]any{
					"account": e.id,
					"panic":   fmt.Sprint(r),
				})
			}
		}
	}()
	if err := e.runner.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		if s.opts.Notifier != nil {
			_ = s.opts.Notifier.Notify(ctx, notify.EventError, map[string]any{
				"account": e.id,
				"error":   err.Error(),
			})
		}
	}
}
```

- [ ] **Step 3: Run tests**

```bash
go test ./internal/scheduler/... -v
```

Expected: PASS, 3 claims observed.

- [ ] **Step 4: Commit**

```bash
git add internal/scheduler/
git commit -m "feat(scheduler): per-account supervisor with panic recovery"
```

---

## Task 12: Minimal HTTP server with /healthz

**Files:**
- Create: `internal/api/server.go`
- Test: `internal/api/server_test.go`

- [ ] **Step 1: Add chi**

```bash
go get github.com/go-chi/chi/v5@v5.0.12
```

- [ ] **Step 2: Write failing test**

```go
// internal/api/server_test.go
package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHealthz(t *testing.T) {
	h := NewRouter(Deps{})
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "ok\n", rec.Body.String())
}
```

- [ ] **Step 3: Implement router**

```go
// internal/api/server.go
package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

type Deps struct {
	// Filled out in Plan 2 (sessions, store, etc.). Empty for now.
}

func NewRouter(d Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	return r
}
```

- [ ] **Step 4: Run tests**

```bash
go mod tidy
go test ./internal/api/... -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/api/ go.mod go.sum
git commit -m "feat(api): bare http router with /healthz"
```

---

## Task 13: Main wiring + dev seed migration

**Files:**
- Create: `internal/store/migrations/0002_dev_seed.sql`
- Create: `cmd/miner/main.go`
- Create: `.env.example`

- [ ] **Step 1: Write conditional dev seed**

```sql
-- internal/store/migrations/0002_dev_seed.sql
-- +goose Up
-- +goose StatementBegin
-- Seed a single fake-backend account. Plan 1 boots this; later plans
-- replace this with real account creation through the GUI.
INSERT INTO accounts (id, platform, login, display_name, status, proxy_url, webhook_url, fingerprint_json, enabled, created_at, updated_at)
SELECT 'acc_fake_dev', 'fake', 'devuser', 'Dev User', 'idle', NULL, NULL, '{}', 1, strftime('%s','now'), strftime('%s','now')
WHERE NOT EXISTS (SELECT 1 FROM accounts WHERE id = 'acc_fake_dev');
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DELETE FROM accounts WHERE id = 'acc_fake_dev';
-- +goose StatementEnd
```

- [ ] **Step 2: Write `cmd/miner/main.go`**

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

	"github.com/chano-fernandez/rust-drops-miner/internal/api"
	"github.com/chano-fernandez/rust-drops-miner/internal/config"
	mlog "github.com/chano-fernandez/rust-drops-miner/internal/log"
	"github.com/chano-fernandez/rust-drops-miner/internal/notify"
	"github.com/chano-fernandez/rust-drops-miner/internal/platform"
	"github.com/chano-fernandez/rust-drops-miner/internal/platform/fake"
	"github.com/chano-fernandez/rust-drops-miner/internal/scheduler"
	"github.com/chano-fernandez/rust-drops-miner/internal/store"
	"github.com/chano-fernandez/rust-drops-miner/internal/store/gen"
	"github.com/chano-fernandez/rust-drops-miner/internal/watcher"
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

	if _, err := store.NewCryptor(cfg.MasterKey); err != nil {
		return fmt.Errorf("master key invalid: %w", err)
	}

	q := gen.New(db)
	accounts, err := q.ListEnabledAccounts(ctx)
	if err != nil {
		return fmt.Errorf("list accounts: %w", err)
	}

	registry := platform.NewRegistry()
	// Fast time keeps the demo loop short (RequiredMinutes=2 vs default 5)
	// so a claim happens within a few seconds during smoke runs.
	registry.Register(fake.New(fake.WithFastTime()))

	notifier := &notify.NoopNotifier{Logger: logger}
	sched := scheduler.New(scheduler.Options{Notifier: notifier})

	for _, a := range accounts {
		backend, ok := registry.Get(a.Platform)
		if !ok {
			logger.Warn("no backend registered for account", "platform", a.Platform, "account", a.ID)
			continue
		}
		// Plan 1 uses an in-memory session; persistence lands in Plan 3.
		sess, err := backend.PollDeviceLogin(ctx, platform.DeviceChallenge{})
		if err != nil {
			logger.Error("device login failed", "account", a.ID, "err", err)
			continue
		}
		w := watcher.New(watcher.Config{
			AccountID:    a.ID,
			Backend:      backend,
			Session:      sess,
			Notifier:     notifier,
			TickInterval: 500 * time.Millisecond,
		})
		sched.Add(a.ID, w)
	}

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           api.NewRouter(api.Deps{}),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		logger.Info("http listening", "addr", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("http server", "err", err)
		}
	}()

	if err := sched.Start(ctx); err != nil {
		return err
	}

	<-ctx.Done()
	logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)

	sched.Wait()
	return nil
}
```

- [ ] **Step 3: Create `.env.example`**

```dotenv
# Required: age secret key (generate once with `age-keygen`)
MINER_MASTER_KEY=AGE-SECRET-KEY-1XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX0

# Optional overrides
MINER_HTTP_ADDR=0.0.0.0:8080
MINER_DB_PATH=/data/miner.db
MINER_SEED_FAKE_ACCOUNT=true
```

- [ ] **Step 4: Build the binary**

```bash
mkdir -p data
export $(grep -v '^#' .env.example | xargs)
go build -o bin/miner ./cmd/miner
```

Expected: `bin/miner` exists.

- [ ] **Step 5: Smoke run**

```bash
MINER_MASTER_KEY="$(go run filippo.io/age/cmd/age-keygen 2>&1 | awk '/AGE-SECRET-KEY/ {print $NF}')" \
MINER_DB_PATH=./data/miner.db \
./bin/miner > /tmp/miner.log 2>&1 &
PID=$!
sleep 10
curl -fsS http://127.0.0.1:8080/healthz
grep -c '"event":"claim"' /tmp/miner.log
kill $PID
wait $PID 2>/dev/null || true
```

Expected: `ok` printed; `grep -c` returns ≥1 (at least one `event=claim` JSON line emitted within 10s).

- [ ] **Step 6: Commit**

```bash
git add internal/store/migrations/0002_dev_seed.sql cmd/miner/main.go .env.example
git commit -m "feat(cmd/miner): wire daemon — config, store, scheduler, http"
```

---

## Task 14: Dockerfile (multi-stage, distroless)

**Files:**
- Create: `deploy/Dockerfile.miner`
- Create: `.dockerignore`

- [ ] **Step 1: Write `.dockerignore`**

```
.git
.github
data/
*.db*
.env
docs/
deploy/
README.md
```

- [ ] **Step 2: Write Dockerfile**

```dockerfile
# deploy/Dockerfile.miner
FROM golang:1.22-alpine AS build
WORKDIR /src
RUN apk add --no-cache git ca-certificates
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/miner ./cmd/miner

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/miner /miner
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/miner"]
```

- [ ] **Step 3: Build image**

```bash
docker build -f deploy/Dockerfile.miner -t rust-drops-miner:dev .
docker images rust-drops-miner:dev
```

Expected: image size <40MB.

- [ ] **Step 4: Commit**

```bash
git add deploy/Dockerfile.miner .dockerignore
git commit -m "feat(deploy): multi-stage distroless Dockerfile for miner"
```

---

## Task 15: docker-compose for local testing

**Files:**
- Create: `deploy/docker-compose.yml`

- [ ] **Step 1: Write compose file**

```yaml
# deploy/docker-compose.yml
services:
  miner:
    image: rust-drops-miner:dev
    build:
      context: ..
      dockerfile: deploy/Dockerfile.miner
    restart: unless-stopped
    ports:
      - "8080:8080"
    environment:
      MINER_MASTER_KEY: ${MINER_MASTER_KEY:?MINER_MASTER_KEY required}
      MINER_DB_PATH: /data/miner.db
      MINER_HTTP_ADDR: 0.0.0.0:8080
    volumes:
      - ./data:/data
```

- [ ] **Step 2: Boot the stack**

```bash
cd deploy
mkdir -p data
export MINER_MASTER_KEY="$(go run filippo.io/age/cmd/age-keygen 2>&1 | awk '/AGE-SECRET-KEY/ {print $NF}')"
docker compose up --build -d
sleep 10
curl -fsS http://127.0.0.1:8080/healthz
docker compose logs miner | grep -c '"event":"claim"'
```

Expected: `healthz` returns `ok`; `grep -c` returns ≥1.

- [ ] **Step 3: Tear down**

```bash
docker compose down
```

- [ ] **Step 4: Commit**

```bash
git add deploy/docker-compose.yml
git commit -m "feat(deploy): docker-compose for local end-to-end testing"
```

---

## Task 16: End-to-end smoke test

**Files:**
- Create: `e2e/smoke_test.go`
- Create: `e2e/go.mod` (separate module to keep e2e deps out of main)

> Optional in Plan 1 — only add if you want the CI gate. If skipping, mark this task complete and move on.

- [ ] **Step 1: Initialize e2e module**

```bash
mkdir -p e2e
cd e2e
go mod init github.com/chano-fernandez/rust-drops-miner-e2e
go get github.com/stretchr/testify@v1.9.0
cd ..
```

- [ ] **Step 2: Write smoke test**

```go
// e2e/smoke_test.go
//go:build e2e

package e2e

import (
	"context"
	"net/http"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSmoke_ComposeUp(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	t.Cleanup(func() {
		_ = exec.Command("docker", "compose", "-f", "../deploy/docker-compose.yml", "down", "-v").Run()
	})

	up := exec.CommandContext(ctx, "docker", "compose", "-f", "../deploy/docker-compose.yml", "up", "--build", "-d")
	out, err := up.CombinedOutput()
	require.NoError(t, err, string(out))

	// Wait for healthz
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get("http://127.0.0.1:8080/healthz")
		if err == nil && resp.StatusCode == 200 {
			resp.Body.Close()
			return
		}
		time.Sleep(time.Second)
	}
	t.Fatal("healthz never returned 200")
}
```

- [ ] **Step 3: Run smoke test**

```bash
export MINER_MASTER_KEY="$(go run filippo.io/age/cmd/age-keygen 2>&1 | awk '/AGE-SECRET-KEY/ {print $NF}')"
cd e2e
go test -tags=e2e -v ./...
cd ..
```

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add e2e/
git commit -m "test(e2e): compose-up smoke test gated by build tag"
```

---

## Done definition

After Task 15, the following must all hold true:

1. `docker compose -f deploy/docker-compose.yml up --build -d` boots the daemon.
2. `curl http://127.0.0.1:8080/healthz` returns `ok`.
3. Container logs show at least one `notify event=claim` line within 60 seconds (FakeBackend mines `RequiredMinutes` ticks then claims).
4. `docker compose logs miner` shows clean shutdown on `docker compose down` (no panic, no leaked goroutine warnings).
5. `go test ./...` is green across all packages.

## Self-review notes

- All types referenced (`platform.DeviceChallenge`, `platform.Session`, etc.) are defined in Task 7.
- `internal/watcher.Notifier` and `internal/notify.Notifier` have identical method signatures. `notify.Event` is declared as a type **alias** (`type Event = string`) which keeps the signatures structurally identical, so `notify.NoopNotifier` satisfies `watcher.Notifier` without an adapter. If you ever change `Event` to a named type, an adapter becomes necessary.
- The dev-seed migration is unconditional in Plan 1 to keep the slice trivially testable. In Plan 2 it will be replaced by a first-run wizard flow and the seed removed.
- `MINER_SEED_FAKE_ACCOUNT` is parsed by `config.Load` but unused in Plan 1 main wiring (the seed migration is unconditional). The env var is reserved so we don't break compose later when we make the seed conditional.
- Deploy to the homelab (10.10.2.40 → rdrops.ryuzec.dev via Traefik + `traeky_proxynet`) is intentionally out of scope here; the local `deploy/docker-compose.yml` is enough to exercise the slice. Production compose file lives in the separate `homelab` repo and is built in a later plan.

## Next plan preview

Plan 2:
- Replace `/healthz`-only router with HTMX surface (`/setup`, `/login`, `/`, `/accounts*`)
- Add admin password + session cookie middleware
- Add real Discord webhook notifier (replaces Noop)
- Drop dev-seed migration; accounts come from GUI
- WebSocket hub for live dashboard updates

Plan 3: Twitch backend (real GraphQL + device-code + minute-watch). Plan 4: Kick backend + browser sidecar. Plan 5: homelab compose, Traefik labels, image push to ghcr, deploy via `homelab-update`.
