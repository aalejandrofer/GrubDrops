# Update checker Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Show an "update available" badge (and recolor the green pulse dot orange) in the nav when the latest GitHub release is newer than the running version, driven by a cached background check.

**Architecture:** A new `internal/update` package fetches the latest GitHub release on boot + every 6h and caches the tag (in-memory atomic + kv). A cheap `Status(current)` (no HTTP) compares it against the running version via a tiny semver parser. A per-request middleware stores the status in request context; `render()` reads it into `templateData` so every page's nav can render the badge. Opt-out via `GRUB_UPDATE_CHECK`.

**Tech Stack:** Go (stdlib net/http, sync/atomic), html/template, HTMX, SQLite/sqlc, testify.

## Global Constraints

- **gofmt:** run `gofmt -w` on every changed `.go` file before committing.
- **i18n:** new UI strings need keys in all three locales (`en.json`, `es.json`, `zh-CN.json`); `internal/i18n` `TestLocaleParity` fails on drift.
- **No new dependencies:** semver compare is a small internal function — do NOT add a module.
- **No `queries/*.sql` changes** (reuse existing kv get/set).
- **Repo slug (verbatim):** `aalejandrofer/GrubDrops`. API URL: `https://api.github.com/repos/aalejandrofer/GrubDrops/releases/latest`.
- **No false positives:** if the running version or the latest tag doesn't parse as semver, the badge must NOT show.
- **Render path never blocks on HTTP:** the per-request path only reads cached state; only the background `Run` loop calls GitHub.
- Ships as **v1.3.5** (default release gate: green build + unit tests).

---

### Task 1: semver parse + compare (pure)

**Files:**
- Create: `internal/update/semver.go`
- Test: `internal/update/semver_test.go`

**Interfaces:**
- Produces: `func Newer(current, latest string) bool` — true iff BOTH parse as semver AND latest > current. Used by `Checker.Status`.

- [ ] **Step 1: Write the failing test.** Create `internal/update/semver_test.go`:

```go
package update

import "testing"

func TestNewer(t *testing.T) {
	cases := []struct {
		current, latest string
		want            bool
	}{
		{"v1.3.4", "v1.3.5", true},   // newer patch
		{"1.3.4", "1.4.0", true},     // newer minor, no v prefix
		{"v1.3.5", "v1.3.5", false},  // equal
		{"v1.3.5", "v1.3.4", false},  // older latest
		{"v2.0.0", "v1.9.9", false},  // older major
		{"v1.3.4-itempull", "v1.3.5", true}, // build suffix on current
		{"v1.3.4", "v1.3.5+build", true},    // build suffix on latest
		{"v1.3", "v1.3.1", true},     // missing patch defaults to 0
		{"", "v1.3.5", false},        // empty current -> no false positive
		{"dev", "v1.3.5", false},     // unparseable current
		{"v1.3.4", "", false},        // empty latest
		{"v1.3.4", "garbage", false}, // unparseable latest
	}
	for _, c := range cases {
		if got := Newer(c.current, c.latest); got != c.want {
			t.Errorf("Newer(%q,%q) = %v, want %v", c.current, c.latest, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails.** Run: `go test ./internal/update/ -run TestNewer -v`. Expected: FAIL — package/func not defined.

- [ ] **Step 3: Implement.** Create `internal/update/semver.go`:

```go
// Package update checks GitHub for a newer GrubDrops release and exposes the
// result to the UI.
package update

import (
	"strconv"
	"strings"
)

// parseSemver extracts MAJOR.MINOR.PATCH from a tag like "v1.3.4",
// "1.3.4-itempull", or "v1.3". A leading "v" is stripped and anything from the
// first non-(digit|dot) character on is ignored (build/prerelease suffix).
// A missing patch (or minor) defaults to 0. ok is false when there aren't at
// least major+minor numeric components.
func parseSemver(s string) (maj, min, patch int, ok bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "v")
	// Truncate at the first char that isn't a digit or '.'.
	end := len(s)
	for i, r := range s {
		if (r < '0' || r > '9') && r != '.' {
			end = i
			break
		}
	}
	s = s[:end]
	if s == "" {
		return 0, 0, 0, false
	}
	parts := strings.Split(s, ".")
	nums := make([]int, 3)
	have := 0
	for i := 0; i < len(parts) && i < 3; i++ {
		if parts[i] == "" {
			return 0, 0, 0, false
		}
		n, err := strconv.Atoi(parts[i])
		if err != nil {
			return 0, 0, 0, false
		}
		nums[i] = n
		have++
	}
	if have < 2 { // need at least MAJOR.MINOR
		return 0, 0, 0, false
	}
	return nums[0], nums[1], nums[2], true
}

// Newer reports whether latest is a strictly higher semver than current.
// Returns false if EITHER side fails to parse (so dev/source/empty builds never
// show an update).
func Newer(current, latest string) bool {
	cMaj, cMin, cPatch, cok := parseSemver(current)
	lMaj, lMin, lPatch, lok := parseSemver(latest)
	if !cok || !lok {
		return false
	}
	if lMaj != cMaj {
		return lMaj > cMaj
	}
	if lMin != cMin {
		return lMin > cMin
	}
	return lPatch > cPatch
}
```

- [ ] **Step 4: Run test to verify it passes.** Run: `go test ./internal/update/ -run TestNewer -v`. Expected: PASS.

- [ ] **Step 5: Commit.**

```bash
gofmt -w internal/update/semver.go internal/update/semver_test.go
git add internal/update/
git commit -m "feat(update): semver Newer() comparison (suffix-tolerant, no false positives)"
```

---

### Task 2: store.Settings release accessors

**Files:**
- Modify: `internal/store/settings.go` (key constants block ~line 35; add accessors after the proxy accessors)
- Test: `internal/store/settings_release_test.go` (create)

**Interfaces:**
- Produces: `(*Settings).LatestRelease(ctx) (string, error)`, `SetLatestRelease(ctx, string) error`, `LastReleaseCheck(ctx) (int64, error)`, `SetLastReleaseCheck(ctx, int64) error`.

- [ ] **Step 1: Write the failing test.** Create `internal/store/settings_release_test.go`:

```go
package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSettings_ReleaseAccessors(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	s := NewSettings(genFromDB(t, db))

	// Defaults before anything is set.
	v, err := s.LatestRelease(ctx)
	require.NoError(t, err)
	require.Equal(t, "", v)
	ts, err := s.LastReleaseCheck(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(0), ts)

	require.NoError(t, s.SetLatestRelease(ctx, "v1.3.5"))
	require.NoError(t, s.SetLastReleaseCheck(ctx, 1751200000))

	v, err = s.LatestRelease(ctx)
	require.NoError(t, err)
	require.Equal(t, "v1.3.5", v)
	ts, err = s.LastReleaseCheck(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(1751200000), ts)
}
```

> **Note for implementer:** confirm how existing `store` tests obtain a `*gen.Queries` from the opened DB (look at the existing settings/queries tests for the exact helper — e.g. `gen.New(db)`). Replace `genFromDB(t, db)` with the real construction used elsewhere in the `store` package tests (it may simply be `gen.New(db)` with a `gen` import; adjust the test accordingly so it compiles).

- [ ] **Step 2: Run test to verify it fails.** Run: `go test ./internal/store/ -run TestSettings_ReleaseAccessors -v`. Expected: FAIL — accessors undefined.

- [ ] **Step 3: Add key constants.** In `internal/store/settings.go`, add to the key-constants group (near `keyCanaryIntervalSec` etc.):

```go
	keyLatestRelease    = "settings:latest_release"     // most recent GitHub release tag
	keyLastReleaseCheck = "settings:last_release_check"  // unix seconds of last successful check
```

- [ ] **Step 4: Add accessors.** In `internal/store/settings.go`, after the proxy accessors, add (uses `strconv`, already imported in this file):

```go
// LatestRelease is the most recent GitHub release tag the update checker saw
// (e.g. "v1.3.5"). Empty when no check has succeeded yet.
func (s *Settings) LatestRelease(ctx context.Context) (string, error) {
	return s.getString(ctx, keyLatestRelease)
}

func (s *Settings) SetLatestRelease(ctx context.Context, tag string) error {
	return s.setString(ctx, keyLatestRelease, strings.TrimSpace(tag))
}

// LastReleaseCheck is the unix-seconds time of the last successful release
// check. 0 when none has succeeded.
func (s *Settings) LastReleaseCheck(ctx context.Context) (int64, error) {
	raw, err := s.getString(ctx, keyLastReleaseCheck)
	if err != nil || raw == "" {
		return 0, err
	}
	n, perr := strconv.ParseInt(raw, 10, 64)
	if perr != nil {
		return 0, nil
	}
	return n, nil
}

func (s *Settings) SetLastReleaseCheck(ctx context.Context, unixSec int64) error {
	return s.setString(ctx, keyLastReleaseCheck, strconv.FormatInt(unixSec, 10))
}
```

(If `strings` isn't already imported in settings.go, add it — verify the import block first.)

- [ ] **Step 5: Run test to verify it passes.** Run: `go test ./internal/store/ -run TestSettings_ReleaseAccessors -v`. Expected: PASS.

- [ ] **Step 6: Commit.**

```bash
gofmt -w internal/store/settings.go internal/store/settings_release_test.go
git add internal/store/
git commit -m "feat(store): latest-release + last-check settings accessors"
```

---

### Task 3: `update.Checker` (fetch + cache + status)

**Files:**
- Create: `internal/update/checker.go`
- Test: `internal/update/checker_test.go`

**Interfaces:**
- Consumes: `Newer` (Task 1), `*store.Settings` release accessors (Task 2).
- Produces:
  - `func NewChecker(client *http.Client, repo string, s *store.Settings) *Checker`
  - `func (c *Checker) RunOnce(ctx context.Context) error`
  - `func (c *Checker) Run(ctx context.Context, every time.Duration)`
  - `func (c *Checker) Status(current string) (available bool, latest string)`

- [ ] **Step 1: Write the failing test.** Create `internal/update/checker_test.go`:

```go
package update

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/store"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

func newTestSettings(t *testing.T) *store.Settings {
	t.Helper()
	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "u.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return store.NewSettings(gen.New(db))
}

func TestRunOnce_CachesTagAndStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Contains(t, r.URL.Path, "/releases/latest")
		_, _ = w.Write([]byte(`{"tag_name":"v1.3.5","name":"v1.3.5"}`))
	}))
	t.Cleanup(srv.Close)

	c := NewChecker(srv.Client(), "owner/repo", newTestSettings(t))
	c.apiBase = srv.URL // test seam: override the GitHub base

	require.NoError(t, c.RunOnce(context.Background()))

	avail, latest := c.Status("v1.3.4")
	require.True(t, avail)
	require.Equal(t, "v1.3.5", latest)

	avail, _ = c.Status("v1.3.5")
	require.False(t, avail, "equal version is not an update")
}

func TestRunOnce_BadResponseKeepsPriorCache(t *testing.T) {
	c := NewChecker(http.DefaultClient, "owner/repo", newTestSettings(t))
	// Seed a known-good cached value.
	c.setLatest("v1.3.5")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	c.httpClient = srv.Client()
	c.apiBase = srv.URL

	require.Error(t, c.RunOnce(context.Background()))
	_, latest := c.Status("v1.3.4")
	require.Equal(t, "v1.3.5", latest, "prior cache retained on error")
}
```

> **Note for implementer:** the test uses two small seams — an exported-within-package `apiBase` field (default `https://api.github.com`) and a `setLatest(string)` helper that stores into the atomic. Add both to the Checker (lowercase, package-private is fine since the test is in package `update`). `gen.New(db)` is the real queries constructor.

- [ ] **Step 2: Run test to verify it fails.** Run: `go test ./internal/update/ -run TestRunOnce -v`. Expected: FAIL — Checker undefined.

- [ ] **Step 3: Implement.** Create `internal/update/checker.go`:

```go
package update

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/store"
)

// Checker polls the GitHub releases API for the latest GrubDrops tag and caches
// it (in memory + kv) so the render path can ask Status() without any network
// call. Best-effort: any fetch error keeps the last-known cached value.
type Checker struct {
	httpClient *http.Client
	repo       string
	apiBase    string // overridable in tests; defaults to https://api.github.com
	s          *store.Settings
	latest     atomic.Value // string
}

// NewChecker builds a checker. The cached latest is seeded from kv so a restart
// keeps the prior answer until the next successful fetch.
func NewChecker(client *http.Client, repo string, s *store.Settings) *Checker {
	c := &Checker{httpClient: client, repo: repo, apiBase: "https://api.github.com", s: s}
	c.latest.Store("")
	if s != nil {
		if v, err := s.LatestRelease(context.Background()); err == nil && v != "" {
			c.latest.Store(v)
		}
	}
	return c
}

func (c *Checker) setLatest(tag string) { c.latest.Store(tag) }

func (c *Checker) getLatest() string {
	if v, ok := c.latest.Load().(string); ok {
		return v
	}
	return ""
}

type ghRelease struct {
	TagName string `json:"tag_name"`
}

// RunOnce fetches the latest release once and caches it. Returns an error on
// any failure; the cached value is left unchanged on error.
func (c *Checker) RunOnce(ctx context.Context) error {
	url := fmt.Sprintf("%s/repos/%s/releases/latest", c.apiBase, c.repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("github releases: status %d", resp.StatusCode)
	}
	var rel ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return err
	}
	if rel.TagName == "" {
		return fmt.Errorf("github releases: empty tag_name")
	}
	c.setLatest(rel.TagName)
	if c.s != nil {
		_ = c.s.SetLatestRelease(ctx, rel.TagName)
		_ = c.s.SetLastReleaseCheck(ctx, time.Now().Unix())
	}
	return nil
}

// Run checks once immediately, then every `every` until ctx is cancelled.
func (c *Checker) Run(ctx context.Context, every time.Duration) {
	if err := c.RunOnce(ctx); err != nil {
		slog.Warn("update check failed", "component", "update", "err", err)
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := c.RunOnce(ctx); err != nil {
				slog.Warn("update check failed", "component", "update", "err", err)
			}
		}
	}
}

// Status reports whether the cached latest release is newer than current, plus
// the latest tag. No network call. Safe to call from the render hot path.
func (c *Checker) Status(current string) (available bool, latest string) {
	latest = c.getLatest()
	return Newer(current, latest), latest
}
```

- [ ] **Step 4: Run test to verify it passes.** Run: `go test ./internal/update/ -v`. Expected: all PASS.

- [ ] **Step 5: Commit.**

```bash
gofmt -w internal/update/checker.go internal/update/checker_test.go
git add internal/update/
git commit -m "feat(update): GitHub release Checker (RunOnce/Run/Status, cached)"
```

---

### Task 4: templateData fields + injection middleware + render wiring

**Files:**
- Modify: `internal/api/render.go` (templateData struct ~16-26; render() ~28-42)
- Modify: `internal/api/middleware.go` (ctxKey const block ~17-21; add middleware + ctx reader)
- Modify: `internal/api/server.go` (Deps struct ~65-117; router mount ~365)
- Test: `internal/api/update_badge_test.go` (create)

**Interfaces:**
- Consumes: `Checker.Status` shape via `Deps.UpdateStatus func(current string) (bool, string)`.
- Produces: `templateData.UpdateAvailable bool`, `templateData.LatestRelease string`; middleware `updateBadge(status func(string)(bool,string), current string) func(http.Handler) http.Handler`.

- [ ] **Step 1: Write the failing test.** Create `internal/api/update_badge_test.go`:

```go
package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestUpdateBadgeMiddleware_InjectsContext proves the middleware stores the
// status from the closure into request context for render() to read.
func TestUpdateBadgeMiddleware_InjectsContext(t *testing.T) {
	status := func(current string) (bool, string) {
		if current != "v1.3.4" {
			t.Fatalf("got current %q", current)
		}
		return true, "v1.3.5"
	}
	var gotAvail bool
	var gotLatest string
	h := updateBadge(status, "v1.3.4")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAvail, gotLatest = updateInfoFromContext(r.Context())
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	if !gotAvail || gotLatest != "v1.3.5" {
		t.Fatalf("ctx not injected: avail=%v latest=%q", gotAvail, gotLatest)
	}
}

// TestUpdateBadgeMiddleware_NilStatusNoPanic proves a nil status (checker
// disabled) is a safe passthrough.
func TestUpdateBadgeMiddleware_NilStatusNoPanic(t *testing.T) {
	called := false
	h := updateBadge(nil, "v1.3.4")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if avail, _ := updateInfoFromContext(r.Context()); avail {
			t.Fatal("nil status must not report an update")
		}
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil).WithContext(context.Background()))
	if !called {
		t.Fatal("next handler not called")
	}
}
```

- [ ] **Step 2: Run test to verify it fails.** Run: `go test ./internal/api/ -run TestUpdateBadgeMiddleware -v`. Expected: FAIL — `updateBadge`/`updateInfoFromContext` undefined.

- [ ] **Step 3: Add the middleware + ctx reader.** In `internal/api/middleware.go`, add a ctx key to the existing `const ( ctxAdminAuthed ctxKey = iota )` block:

```go
const (
	ctxAdminAuthed ctxKey = iota
	ctxUpdateInfo
)

type updateInfoVal struct {
	available bool
	latest    string
}

// updateBadge stores the cached update status in request context so render()
// can surface the nav badge. status may be nil (checker disabled) — then it's a
// safe passthrough that reports no update. current is the running version.
func updateBadge(status func(current string) (bool, string), current string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if status != nil {
				avail, latest := status(current)
				ctx := context.WithValue(r.Context(), ctxUpdateInfo, updateInfoVal{available: avail, latest: latest})
				r = r.WithContext(ctx)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// updateInfoFromContext reads the injected update status (false/"" when absent).
func updateInfoFromContext(ctx context.Context) (bool, string) {
	if v, ok := ctx.Value(ctxUpdateInfo).(updateInfoVal); ok {
		return v.available, v.latest
	}
	return false, ""
}
```

(Ensure `context` is imported in middleware.go.)

- [ ] **Step 4: Add the templateData fields + read them in render().** In `internal/api/render.go`, add to `templateData`:

```go
	Lang             string // current language code (e.g. "en", "zh-CN")
	UpdateAvailable  bool   // a newer GitHub release exists
	LatestRelease    string // latest release tag, for the nav badge
```

Then in `render()`, after the existing `data.Lang = ...` line, add:

```go
	data.UpdateAvailable, data.LatestRelease = updateInfoFromContext(r.Context())
```

- [ ] **Step 5: Add the Deps field + mount the middleware.** In `internal/api/server.go` `Deps` struct (near `Version string`):

```go
	Version    string // semver / release tag
	// UpdateStatus reports (updateAvailable, latestTag) for the running version.
	// Nil when the update checker is disabled — the nav badge then never shows.
	UpdateStatus func(current string) (bool, string)
```

In `NewRouter`, where the authed sub-router is mounted (`r.Mount("/", withSession(csrf(authed)))`), wrap with the badge middleware:

```go
	r.Mount("/", withSession(csrf(updateBadge(d.UpdateStatus, d.Version)(authed))))
```

- [ ] **Step 6: Run tests.** Run: `go test ./internal/api/ -run TestUpdateBadgeMiddleware -v` then `go build ./...`. Expected: PASS + clean build (existing render() callers unaffected — the new fields default to zero and are filled from context).

- [ ] **Step 7: Commit.**

```bash
gofmt -w internal/api/render.go internal/api/middleware.go internal/api/server.go internal/api/update_badge_test.go
git add internal/api/
git commit -m "feat(api): inject update status into templateData via middleware"
```

---

### Task 5: main.go wiring (build checker, schedule, env gate)

**Files:**
- Modify: `cmd/miner/main.go` (after the canary wiring; in the `api.Deps{...}` literal)

**Interfaces:**
- Consumes: `update.NewChecker`, `Checker.Run`, `Checker.Status` (Task 3); `Deps.UpdateStatus` (Task 4); existing `proxyTransport`, `settingsStore`, resolved `version`, `parseDuration`.

- [ ] **Step 1: Build the checker + schedule it (gated by env).** In `cmd/miner/main.go`, after the canary `go canaryRunner.Run(...)` block and BEFORE the `deps := api.Deps{...}` construction, add:

```go
	// Update checker: background GitHub-release poll (boot + every interval),
	// cached so the render path never calls GitHub. Disabled by GRUB_UPDATE_CHECK=0.
	var updateStatus func(current string) (bool, string)
	if v := strings.ToLower(strings.TrimSpace(os.Getenv("GRUB_UPDATE_CHECK"))); v != "0" && v != "false" {
		updateClient := &http.Client{Timeout: 10 * time.Second}
		if proxyTransport != nil {
			updateClient.Transport = proxyTransport
		}
		updateChecker := update.NewChecker(updateClient, "aalejandrofer/GrubDrops", settingsStore)
		updateInterval := parseDuration(os.Getenv("GRUB_UPDATE_INTERVAL"), 6*time.Hour)
		go updateChecker.Run(ctx, updateInterval)
		updateStatus = updateChecker.Status
	}
```

- [ ] **Step 2: Pass it into Deps.** In the `api.Deps{...}` literal, add (next to `Version:`):

```go
		Version:           cmp.Or(version, os.Getenv("GRUB_VERSION")),
		UpdateStatus:      updateStatus,
```

- [ ] **Step 3: Add the import.** Ensure `cmd/miner/main.go` imports `"github.com/aalejandrofer/grubdrops/internal/update"` (and that `net/http`, `strings`, `time` are already imported — they are, but verify).

- [ ] **Step 4: Build.** Run: `go build ./...`. Expected: clean. (No unit test for this glue — it's wiring; the checker is tested in Task 3, the middleware in Task 4. The render path is exercised in Task 6's nav test.)

- [ ] **Step 5: Commit.**

```bash
gofmt -w cmd/miner/main.go
git add cmd/miner/main.go
git commit -m "feat(miner): wire update checker (env-gated, 6h default)"
```

---

### Task 6: Nav badge + pulse recolor + CSS + i18n + render test

**Files:**
- Modify: `internal/web/templates/_nav.html` (pulse span line 19; after the gh-link `</a>` line 22)
- Modify: `internal/web/static/css/app.css` (`.pulse` area + new badge rules)
- Modify: `internal/i18n/locales/en.json`, `es.json`, `zh-CN.json`
- Test: `internal/api/update_badge_test.go` (append a nav render test)

**Interfaces:**
- Consumes: `templateData.UpdateAvailable`, `templateData.LatestRelease` (Task 4).

- [ ] **Step 1: Add i18n keys (all three locales).** In `en.json`, after `"nav.github_aria": ...`:

```json
  "nav.update_available": "update",
  "nav.update_title": "new version available",
```

In `es.json` after its `nav.github_aria`:

```json
  "nav.update_available": "actualizar",
  "nav.update_title": "nueva versión disponible",
```

In `zh-CN.json` after its `nav.github_aria`:

```json
  "nav.update_available": "更新",
  "nav.update_title": "有新版本可用",
```

- [ ] **Step 2: Recolor the pulse dot + add the badge.** In `internal/web/templates/_nav.html`, replace the pulse span (line 19):

```html
      <span class="pulse" title="{{t "dashboard.streaming"}}"></span>
```

with:

```html
      <span class="pulse{{if .UpdateAvailable}} update{{end}}" title="{{if .UpdateAvailable}}{{t "nav.update_title"}} {{.LatestRelease}}{{else}}{{t "dashboard.streaming"}}{{end}}"></span>
```

Then, immediately after the GitHub link's closing `</a>` (line 22), add the badge:

```html
      {{if .UpdateAvailable}}
      <a class="update-pill" href="https://github.com/aalejandrofer/GrubDrops/releases/latest" target="_blank" rel="noopener" title="{{t "nav.update_title"}} {{.LatestRelease}}">{{t "nav.update_available"}}</a>
      {{end}}
```

- [ ] **Step 3: Add CSS.** In `internal/web/static/css/app.css`, after the existing `.user .pulse` rule, add:

```css
  /* Update available: pulse dot turns amber, plus a small pill by the GitHub icon. */
  .user .pulse.update { background: var(--amber); box-shadow: 0 0 10px var(--amber); }
  .update-pill {
    font-family: 'JetBrains Mono', monospace; font-size: 10px; line-height: 1;
    padding: 3px 7px; margin-left: 6px; border-radius: 10px;
    color: var(--amber); border: 1px solid var(--amber);
    text-decoration: none; text-transform: uppercase; letter-spacing: 0.04em;
  }
  .update-pill:hover { background: var(--amber); color: #1a1a1a; }
```

(If `--amber` isn't defined in this stylesheet, it is — confirmed at the `:root` block. Reuse it.)

- [ ] **Step 4: Write the nav render test.** Append to `internal/api/update_badge_test.go`:

```go
import (
	"bytes"
	"strings"
	// keep existing imports

	"github.com/aalejandrofer/grubdrops/internal/web"
)

func renderNav(t *testing.T, data templateData) string {
	t.Helper()
	tmpl, err := web.Templates()
	if err != nil {
		t.Fatalf("load templates: %v", err)
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "nav", data); err != nil {
		t.Fatalf("render nav: %v", err)
	}
	return buf.String()
}

func TestNav_UpdateBadgeShownWhenAvailable(t *testing.T) {
	out := renderNav(t, templateData{AuthedAdmin: true, UpdateAvailable: true, LatestRelease: "v1.3.5"})
	if !strings.Contains(out, "update-pill") {
		t.Errorf("update pill missing when UpdateAvailable")
	}
	if !strings.Contains(out, "/releases/latest") {
		t.Errorf("pill must link to releases/latest")
	}
	if !strings.Contains(out, "v1.3.5") {
		t.Errorf("pill/title must show the latest version")
	}
	if !strings.Contains(out, "pulse update") {
		t.Errorf("pulse dot must get the .update class (orange)")
	}
}

func TestNav_NoBadgeWhenUpToDate(t *testing.T) {
	out := renderNav(t, templateData{AuthedAdmin: true, UpdateAvailable: false})
	if strings.Contains(out, "update-pill") {
		t.Errorf("no pill when up to date")
	}
	if strings.Contains(out, "pulse update") {
		t.Errorf("pulse must stay plain (green) when up to date")
	}
}
```

> **Note for implementer:** confirm `web.Templates()` returns a value with an `ExecuteTemplate(io.Writer, name string, data any)` method (it does — it backs the server renderer). If the `api` test package already has a nav/layout render helper, reuse it instead of adding `renderNav`.

- [ ] **Step 5: Run tests.** Run: `go test ./internal/api/ -run 'TestNav_' -v` and `go test ./internal/i18n/ -run TestLocaleParity -v`. Expected: all PASS.

- [ ] **Step 6: Commit.**

```bash
gofmt -w internal/api/update_badge_test.go
git add internal/web/ internal/i18n/ internal/api/
git commit -m "feat(nav): update-available pill + amber pulse dot"
```

---

### Task 7: Changelog + full gate

**Files:**
- Modify: `docs/CHANGELOG.md`

- [ ] **Step 1: Add the changelog entry.** Under `## [Unreleased]`, add (merge into existing `### Added` if present):

```markdown
### Added

- **Update checker.** GrubDrops now checks GitHub for new releases in the
  background (on boot and every 6 hours) and shows an "update" pill by the
  GitHub icon plus an amber status dot when a newer version is out, linking to
  the release. Set `GRUB_UPDATE_CHECK=0` to disable the check.
```

- [ ] **Step 2: Full gate.** Run:

```bash
gofmt -l internal/ cmd/
go build ./...
go test ./internal/update/ ./internal/store/ ./internal/api/ ./internal/i18n/
```

Expected: `gofmt -l` prints nothing; build clean; all packages `ok`.

- [ ] **Step 3: Commit.**

```bash
git add docs/CHANGELOG.md
git commit -m "docs(changelog): update checker"
```

---

## Self-Review

**Spec coverage:**
- `internal/update` checker (RunOnce/Run/Status, cached, proxy client) → Tasks 1+3. ✓
- Safe semver compare, no false positives on dev/empty → Task 1. ✓
- kv cache (latest + last-check) → Task 2, used by Task 3. ✓
- templateData fields + one-place injection middleware → Task 4. ✓
- main.go wiring + `GRUB_UPDATE_CHECK` opt-out + 6h default → Task 5. ✓
- Nav "update" pill (links to releases/latest) + green→amber pulse recolor + CSS → Task 6. ✓
- i18n en/es/zh-CN → Task 6. ✓
- Tests: compare, fetch/cache + error-retains-cache, middleware inject + nil-safe, nav render shown/hidden → Tasks 1,3,4,6. ✓

**Placeholder scan:** Three implementer-confirmation notes (Task 2 `gen.New(db)` test construction, Task 3 test seams already specified, Task 6 `web.Templates()` render helper) — each gives the concrete expected symbol + intent, not vague directives. No TBDs.

**Type consistency:** `Status(current string) (bool, string)` is the shape of `Deps.UpdateStatus func(current string)(bool,string)` (Task 3 ↔ Task 4 ↔ Task 5). `updateBadge(status func(string)(bool,string), current string)` matches its call `updateBadge(d.UpdateStatus, d.Version)` (Task 4). `templateData.UpdateAvailable/LatestRelease` set in render() (Task 4) and read in `_nav.html` (Task 6). `Newer(current, latest string) bool` (Task 1) used by `Status` (Task 3). kv keys `settings:latest_release`/`settings:last_release_check` consistent (Task 2 ↔ Task 3 via the accessors). Repo slug `aalejandrofer/GrubDrops` consistent (Task 5 wiring, Task 6 link).
