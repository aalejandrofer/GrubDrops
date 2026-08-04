# Operator Chores Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Alert the operator the moment an account's auth dies, keep Kick sessions alive by persisting rotated cookies, and let the operator download a consistent DB snapshot from the UI.

**Architecture:** Four independent changes. (1) `internal/authcheck` gains an optional `notify.Notifier` and fires `notify.EventAuth` on OK↔fail *transitions* detected inside `persist()`. (2) The dashboard alert builder additionally reads the persisted authcheck result so one truth shows in both places. (3) The Kick utls HTTP client captures `Set-Cookie` via a callback hook and persists a rotated session through a new `SessionPersister` seam. (4) A Settings → Health route streams a `VACUUM INTO` snapshot.

**Tech Stack:** Go 1.26.2, chi v5, sqlc-generated queries (`internal/store/gen`), modernc.org/sqlite, `html/template` + HTMX, `testing` stdlib (no testify in the packages touched here except where already imported).

## Global Constraints

- **Go 1.26.2** (`go.mod`). Format with `gofmt -w` before every commit — CI has a gofmt gate that fails fast.
- **Stay Go + `html/template` + HTMX.** No JS framework, no build step.
- **Never put `?` or parentheses in a `queries/*.sql` comment** — it corrupts sqlc placeholder rewriting for later queries. (No new queries in this plan, but Task 4 touches SQL.)
- **Log every change to `docs/CHANGELOG.md` under `## [Unreleased]`** as you commit — Added / Changed / Fixed / Removed.
- **Release gate:** Tasks 1, 2, 5 are taggable on green build + passing unit tests. **Task 3 and 4 touch the Kick watch path and must NOT be tagged without live-drop verification** — they stay in `[Unreleased]` until confirmed against a live Kick session.
- **Never concatenate outside-controlled data into a chromedp `Evaluate` script.** (Task 4 touches the Kick sidecar; use the existing `jsB64JSON` helper if any value is embedded.)
- **The browser sidecar stack is core, not dead code.** Do not delete `cmd/browser-sidecar`, `internal/auth/browser`, `internal/dockerctl`, `proto/`, or the Twitch `BrowserBackend`.
- **Zero open CodeQL alerts** must be maintained; CodeQL runs on every push to `master`.
- Existing test style in these packages: hand-rolled fakes and `t.Fatalf`, not testify. Match it.

## File Structure

| File | Responsibility | Task |
|---|---|---|
| `internal/authcheck/authcheck.go` | Add `notifier` field, `WithNotifier`, transition detection in `persist` | 1 |
| `internal/authcheck/notify_test.go` | Transition-matrix tests (new file, keeps the 204-line authcheck_test.go focused) | 1 |
| `cmd/miner/main.go` | Wire notifier into `authcheck.New`; wire `SessionPersister` into Kick backend | 1, 3 |
| `internal/api/handlers_dashboard.go` | OR the authcheck result into the `needs_auth` alert | 2 |
| `internal/api/handlers_dashboard_alerts_test.go` | Alert-dedup tests (new file; `handlers_dashboard.go` is already 1214 lines) | 2 |
| `internal/platform/kick/transport.go` | `onCookies` hook on `httpDoer`, invoked from `do` and `getRaw` | 3 |
| `internal/platform/kick/cookies.go` | `mergeCookies` pure function + rotatable-name set (new file) | 3 |
| `internal/platform/kick/cookies_test.go` | Merge unit tests (new file) | 3 |
| `internal/platform/kick/backend.go` | `SessionPersister` field, capture wiring, `RefreshSession` returns freshest | 3 |
| `internal/platform/kick/backend_persist_test.go` | Persister-invocation tests (new file; `backend_test.go` is already 473 lines) | 3 |
| `internal/auth/browser/sidecar/kick.go` | `GetCookies` after watch, round-trip out over existing proto field | 4 |
| `internal/api/handlers_snapshot.go` | `VACUUM INTO` + stream (new file) | 5 |
| `internal/api/handlers_snapshot_test.go` | Snapshot tests (new file) | 5 |
| `internal/api/server.go` | Register `POST /settings/snapshot` | 5 |
| `internal/web/templates/settings.html` | Download-snapshot button on the Health tab | 5 |
| `internal/i18n/locales/{en,es,zh-CN}.json` | Keys for the snapshot button | 5 |
| `docs/CHANGELOG.md` | `[Unreleased]` entries | all |

Task 3 is split across `cookies.go` (pure merge logic, trivially testable) and `transport.go`/`backend.go` (wiring) deliberately: the merge rules are the part with real edge cases, and isolating them keeps them out of the 848-line `backend.go` and the 259-line `transport.go`.

---

### Task 1: Fire EventAuth on auth-health transitions

This is the core fix. `notify.EventAuth` currently has **zero producers** anywhere in the codebase — it is declared, has a Discord embed case, and is registered in the verbosity filter, but nothing ever calls it. `authcheck` detects dead sessions hourly and tells nobody.

**Files:**
- Modify: `internal/authcheck/authcheck.go` (struct at `:38-48`, `New` at `:46`, `persist` at `:178`)
- Modify: `cmd/miner/main.go:579` (the `authcheck.New` call)
- Test: `internal/authcheck/notify_test.go` (create)

**Interfaces:**
- Consumes: `notify.Notifier` (`internal/notify/notify.go:22`) — `Notify(ctx context.Context, event Event, fields map[string]any) error`, where `notify.Event` is a `string` alias. `notify.EventAuth` is the constant `"auth"`. Existing `authcheck.Load(ctx, q, accountID) (Result, bool)` at `:187`. Existing `Result{OK bool; CheckedAt int64; Msg string}`.
- Produces: `func (c *Checker) WithNotifier(n Notifier) *Checker` — returns the receiver for chaining. A local `Notifier` interface declared in the `authcheck` package (so `authcheck` does not import `internal/notify`, matching how `internal/api` declares its own `Notifier` at `server.go:66`).

- [ ] **Step 1: Write the failing test**

Create `internal/authcheck/notify_test.go`:

```go
package authcheck

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/aalejandrofer/grubdrops/internal/store"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

// fakeNotifier records every Notify call.
type fakeNotifier struct {
	mu    sync.Mutex
	calls []map[string]any
}

func (f *fakeNotifier) Notify(_ context.Context, _ string, fields map[string]any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	copied := map[string]any{}
	for k, v := range fields {
		copied[k] = v
	}
	f.calls = append(f.calls, copied)
	return nil
}

func (f *fakeNotifier) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func testQueries(t *testing.T) *gen.Queries {
	t.Helper()
	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "authcheck.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return gen.New(db)
}

// An hourly sweep against a dead account must notify ONCE, not once per
// sweep. Level-triggering here would send 24 messages a day per broken
// account — the same defect shape as the Kick claim spam fixed in v1.3.11.
func TestPersist_EdgeTriggeredOnly(t *testing.T) {
	ctx := context.Background()
	q := testQueries(t)
	fn := &fakeNotifier{}
	c := New(q, nil, nil).WithNotifier(fn)

	// none -> OK: healthy first observation, no news to report.
	c.persist(ctx, "acc_1", Result{OK: true, Msg: "ok"})
	if got := fn.count(); got != 0 {
		t.Fatalf("none->OK should not notify, got %d sends", got)
	}

	// OK -> fail: the account just died. Notify.
	c.persist(ctx, "acc_1", Result{OK: false, Msg: "401 unauthorized"})
	if got := fn.count(); got != 1 {
		t.Fatalf("OK->fail should notify once, got %d sends", got)
	}

	// fail -> fail: still dead, already told them. Silence.
	c.persist(ctx, "acc_1", Result{OK: false, Msg: "401 unauthorized"})
	c.persist(ctx, "acc_1", Result{OK: false, Msg: "401 unauthorized"})
	if got := fn.count(); got != 1 {
		t.Fatalf("fail->fail must NOT re-fire, got %d sends", got)
	}

	// fail -> OK: recovered. Notify.
	c.persist(ctx, "acc_1", Result{OK: true, Msg: "ok"})
	if got := fn.count(); got != 2 {
		t.Fatalf("fail->OK should notify recovery, got %d sends", got)
	}
}

// A first-ever observation that is already failing must notify — otherwise a
// miner started with an already-expired session stays silent forever.
func TestPersist_FirstEverFailureNotifies(t *testing.T) {
	ctx := context.Background()
	fn := &fakeNotifier{}
	c := New(testQueries(t), nil, nil).WithNotifier(fn)

	c.persist(ctx, "acc_1", Result{OK: false, Msg: "no session"})
	if got := fn.count(); got != 1 {
		t.Fatalf("first-ever fail should notify, got %d sends", got)
	}
}

// The notification must say WHICH account and WHY, and carry the account id
// in the "account" field so notify.AccountRoutedNotifier can route it to a
// per-account webhook.
func TestPersist_FieldsCarryAccountAndReason(t *testing.T) {
	ctx := context.Background()
	fn := &fakeNotifier{}
	c := New(testQueries(t), nil, nil).WithNotifier(fn)

	c.persist(ctx, "acc_abc", Result{OK: false, Msg: "401 unauthorized"})
	if fn.count() != 1 {
		t.Fatalf("expected 1 send, got %d", fn.count())
	}
	got := fn.calls[0]
	if got["account"] != "acc_abc" {
		t.Fatalf("account field = %v, want acc_abc", got["account"])
	}
	if got["ok"] != false {
		t.Fatalf("ok field = %v, want false", got["ok"])
	}
	if got["reason"] != "401 unauthorized" {
		t.Fatalf("reason field = %v, want the Result.Msg", got["reason"])
	}
}

// Notifications are best-effort: a send failure must never stop the health
// result from being persisted, because auth health is the source of truth.
func TestPersist_SendFailureStillPersists(t *testing.T) {
	ctx := context.Background()
	q := testQueries(t)
	c := New(q, nil, nil).WithNotifier(errNotifier{})

	c.persist(ctx, "acc_1", Result{OK: false, Msg: "boom"})
	res, ok := Load(ctx, q, "acc_1")
	if !ok {
		t.Fatal("result was not persisted after a failed send")
	}
	if res.OK {
		t.Fatal("persisted result should be a failure")
	}
}

type errNotifier struct{}

func (errNotifier) Notify(context.Context, string, map[string]any) error {
	return errors.New("webhook down")
}

// A miner with no webhook configured has a nil notifier and must behave
// exactly as before: persist, no panic.
func TestPersist_NilNotifierIsNoop(t *testing.T) {
	ctx := context.Background()
	q := testQueries(t)
	c := New(q, nil, nil)

	c.persist(ctx, "acc_1", Result{OK: false, Msg: "boom"})
	if _, ok := Load(ctx, q, "acc_1"); !ok {
		t.Fatal("result was not persisted with a nil notifier")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/authcheck/ -run 'TestPersist' -v`
Expected: FAIL to **compile**, with `c.WithNotifier undefined (type *Checker has no field or method WithNotifier)`.

- [ ] **Step 3: Write minimal implementation**

In `internal/authcheck/authcheck.go`, add the local interface and the field. Put the interface declaration just above `type Checker struct`:

```go
// Notifier fires an operator notification. Matches notify.Notifier; kept as
// a local interface so this package stays decoupled from internal/notify
// (same pattern as internal/api's Notifier in server.go).
type Notifier interface {
	Notify(ctx context.Context, event string, fields map[string]any) error
}
```

Add to the `Checker` struct, after the `retryDelay` field:

```go
	// notifier fires EventAuth on an auth-health transition. Nil means no
	// notification is sent (the miner runs fine with no webhook set).
	notifier Notifier
```

Add after `New`:

```go
// WithNotifier attaches the operator notifier and returns the receiver, so
// wiring reads as one expression at the call site.
func (c *Checker) WithNotifier(n Notifier) *Checker {
	c.notifier = n
	return c
}
```

Replace `persist` (currently at `:178`) with:

```go
func (c *Checker) persist(ctx context.Context, accountID string, res Result) {
	prev, hadPrev := Load(ctx, c.q, accountID)

	b, _ := json.Marshal(res)
	if err := c.q.UpsertSettingString(ctx, gen.UpsertSettingStringParams{Key: Prefix + accountID, Value: b}); err != nil {
		c.log.Warn("authcheck: persist failed", "account", accountID, "err", err)
	}

	c.notifyTransition(ctx, accountID, prev, hadPrev, res)
}

// notifyTransition fires EventAuth only when auth health CHANGED:
// OK→fail, first-ever fail, or fail→OK (recovery). A fail→fail repeat is
// deliberately silent — the sweep runs hourly, so level-triggering would
// send 24 messages a day per dead account.
func (c *Checker) notifyTransition(ctx context.Context, accountID string, prev Result, hadPrev bool, res Result) {
	if c.notifier == nil {
		return
	}
	if hadPrev && prev.OK == res.OK {
		return // no change
	}
	if !hadPrev && res.OK {
		return // first observation, and it's healthy — nothing to report
	}
	fields := map[string]any{
		"account": accountID,
		"ok":      res.OK,
		"reason":  res.Msg,
	}
	if err := c.notifier.Notify(ctx, "auth", fields); err != nil {
		// Best-effort: auth health is already persisted above and is the
		// source of truth. A dead webhook must not mask a dead account.
		c.log.Warn("authcheck: notify failed", "account", accountID, "err", err)
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/authcheck/ -v`
Expected: PASS — the five new `TestPersist_*` tests plus the two pre-existing `TestVerifyWithRetry_*` tests.

- [ ] **Step 5: Enrich the notification with platform and display name**

`checkOne` knows the platform and can look up the account. Without this the Discord message reads "acc_89de4e08 died", which is not actionable at a glance.

Add a test to `internal/authcheck/notify_test.go`:

```go
// The notification should name the account the way the operator sees it in
// the UI, not just by opaque id.
func TestPersist_FieldsCarryPlatformAndName(t *testing.T) {
	ctx := context.Background()
	fn := &fakeNotifier{}
	c := New(testQueries(t), nil, nil).WithNotifier(fn)

	c.persistWithMeta(ctx, "acc_abc", "kick", "MyKickAccount", Result{OK: false, Msg: "401"})
	if fn.count() != 1 {
		t.Fatalf("expected 1 send, got %d", fn.count())
	}
	got := fn.calls[0]
	if got["platform"] != "kick" {
		t.Fatalf("platform field = %v, want kick", got["platform"])
	}
	if got["name"] != "MyKickAccount" {
		t.Fatalf("name field = %v, want MyKickAccount", got["name"])
	}
}
```

- [ ] **Step 6: Run test to verify it fails**

Run: `go test ./internal/authcheck/ -run TestPersist_FieldsCarryPlatformAndName -v`
Expected: FAIL to compile — `c.persistWithMeta undefined`.

- [ ] **Step 7: Implement persistWithMeta**

In `internal/authcheck/authcheck.go`, make `persist` delegate so both entry points share one path:

```go
func (c *Checker) persist(ctx context.Context, accountID string, res Result) {
	c.persistWithMeta(ctx, accountID, "", "", res)
}

// persistWithMeta is persist plus the platform and display name used to make
// the notification human-readable. Empty meta values are omitted from the
// notify fields.
func (c *Checker) persistWithMeta(ctx context.Context, accountID, plat, name string, res Result) {
	prev, hadPrev := Load(ctx, c.q, accountID)

	b, _ := json.Marshal(res)
	if err := c.q.UpsertSettingString(ctx, gen.UpsertSettingStringParams{Key: Prefix + accountID, Value: b}); err != nil {
		c.log.Warn("authcheck: persist failed", "account", accountID, "err", err)
	}

	c.notifyTransition(ctx, accountID, plat, name, prev, hadPrev, res)
}
```

Update `notifyTransition`'s signature and field map:

```go
func (c *Checker) notifyTransition(ctx context.Context, accountID, plat, name string, prev Result, hadPrev bool, res Result) {
	if c.notifier == nil {
		return
	}
	if hadPrev && prev.OK == res.OK {
		return
	}
	if !hadPrev && res.OK {
		return
	}
	fields := map[string]any{
		"account": accountID,
		"ok":      res.OK,
		"reason":  res.Msg,
	}
	if plat != "" {
		fields["platform"] = plat
	}
	if name != "" {
		fields["name"] = name
	}
	if err := c.notifier.Notify(ctx, "auth", fields); err != nil {
		c.log.Warn("authcheck: notify failed", "account", accountID, "err", err)
	}
}
```

In `checkOne`, thread the metadata through. `checkOne` currently takes `(ctx, accountID, plat string)` and calls `c.persist(ctx, accountID, res)` in five places. Change the signature to `checkOne(ctx context.Context, accountID, plat, name string)` and replace every `c.persist(ctx, accountID, res)` inside it with `c.persistWithMeta(ctx, accountID, plat, name, res)`. Update the caller in `CheckAll`:

```go
	for _, a := range accs {
		c.checkOne(ctx, a.ID, a.Platform, a.DisplayName)
	}
```

- [ ] **Step 8: Run the full package tests**

Run: `go test ./internal/authcheck/ -v`
Expected: PASS, all tests.

Then confirm nothing else called the changed private signature:

Run: `go build ./... && go vet ./...`
Expected: no output, exit 0.

- [ ] **Step 9: Wire the notifier in main.go**

`cmd/miner/main.go:579` currently reads:

```go
	authChecker := authcheck.New(q, sessions, registry)
```

The notifier variable in scope at that point is the one already built for the watchers and the `/settings` test button. Find it by searching upward for the `notify.` construction (it is the value assigned into `Deps.Notifier` and passed to watcher configs). Change the call to:

```go
	// Attach the notifier so an account whose auth dies raises an alert
	// instead of silently stopping. EventAuth had no producer before this.
	authChecker := authcheck.New(q, sessions, registry).WithNotifier(notifier)
```

Replace `notifier` with the actual identifier in scope. If that value's static type is a concrete `*notify.AccountRoutedNotifier` or `*notify.DiscordWebhook`, it satisfies `authcheck.Notifier` structurally with no cast, because `notify.Event` is a `string` alias — verify with the build in the next step rather than assuming.

- [ ] **Step 10: Verify the wiring compiles and the whole suite is green**

Run: `gofmt -l ./cmd ./internal`
Expected: no output (no unformatted files).

Run: `go build ./... && go test ./... 2>&1 | tail -35`
Expected: build clean; every package `ok` or `[no test files]`.

- [ ] **Step 11: Changelog + commit**

Add under `## [Unreleased]` in `docs/CHANGELOG.md`:

```markdown
### Fixed

- **An account whose login dies now actually tells you.** The auth-health
  sweep has always detected expired sessions hourly and written the result to
  the Accounts page, but the notification event it was supposed to send
  (`EventAuth`) had no producer anywhere in the code — it was declared, had a
  Discord message template, was registered in the notification filter, and was
  never once fired. A Kick account could sit dead for days with the dashboard
  showing nothing. The sweep now sends a notification the moment auth health
  changes, and a recovery notice when it comes back. It fires only on the
  change, so a still-broken account is not re-reported every hour.
```

```bash
gofmt -w ./cmd ./internal
git add internal/authcheck/authcheck.go internal/authcheck/notify_test.go cmd/miner/main.go docs/CHANGELOG.md
git commit -m "fix(authcheck): notify on auth-health transitions

notify.EventAuth was declared, had a Discord embed case, and was registered
in the verbosity filter, but nothing in the codebase ever fired it. The
hourly auth sweep detected expired sessions and told nobody, so a dead
account could go unnoticed for days.

Fire EventAuth from persist() on OK->fail, first-ever fail, and fail->OK.
Edge-triggered on purpose: the sweep is hourly, so level-triggering would
send 24 messages a day per dead account."
```

---

### Task 2: One expiry signal on the dashboard

Two unrelated signals disagree today. The dashboard alert list is built from *scheduler state* (`handlers_dashboard.go:309-316`); the Accounts page shows the *persisted authcheck result* (`handlers_accounts.go:112`). An account that is not actively watching can fail authcheck without ever flipping scheduler state, so the dashboard reads green while the account is dead.

**Files:**
- Modify: `internal/api/handlers_dashboard.go:307-337` (the alert-building loop in `collectPage`)
- Test: `internal/api/handlers_dashboard_alerts_test.go` (create)

**Interfaces:**
- Consumes: `authcheck.Load(ctx, q, accountID) (authcheck.Result, bool)` — already imported in this package's `handlers_accounts.go:16`, so the import path is proven. `dashAlert{Kind, Account, URL, Action string}` at `handlers_dashboard.go:252`. `dashMineCard` has fields `ID`, `Name`, `Platform`, `State` (used at `:311-337`).
- Produces: `func authAlertAccounts(ctx context.Context, q *gen.Queries, cards []dashMineCard) map[string]bool` — the set of account IDs whose persisted authcheck result is a failure. Task 2 is the only consumer.

- [ ] **Step 1: Write the failing test**

Create `internal/api/handlers_dashboard_alerts_test.go`:

```go
package api

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/authcheck"
	"github.com/aalejandrofer/grubdrops/internal/store"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

func alertTestQueries(t *testing.T) *gen.Queries {
	t.Helper()
	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "alerts.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return gen.New(db)
}

func writeAuthResult(t *testing.T, q *gen.Queries, accountID string, ok bool) {
	t.Helper()
	b, err := json.Marshal(authcheck.Result{OK: ok, CheckedAt: time.Now().Unix(), Msg: "test"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := q.UpsertSettingString(context.Background(), gen.UpsertSettingStringParams{
		Key:   authcheck.Prefix + accountID,
		Value: b,
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
}

// An account that is idle (not "needs_auth" by scheduler state) but whose
// persisted auth check FAILED must still raise a dashboard alert. This is the
// gap: the dashboard read green while the account was dead.
func TestAuthAlertAccounts_FlagsFailedCheck(t *testing.T) {
	ctx := context.Background()
	q := alertTestQueries(t)
	writeAuthResult(t, q, "acc_dead", false)
	writeAuthResult(t, q, "acc_live", true)

	cards := []dashMineCard{
		{ID: "acc_dead", Name: "dead", State: "sleeping"},
		{ID: "acc_live", Name: "live", State: "watching"},
	}
	got := authAlertAccounts(ctx, q, cards)

	if !got["acc_dead"] {
		t.Fatal("account with a failed auth check should be flagged")
	}
	if got["acc_live"] {
		t.Fatal("account with a passing auth check must not be flagged")
	}
}

// An account with no recorded check yet must NOT be flagged — a fresh install
// would otherwise show a wall of false alerts before the first sweep runs.
func TestAuthAlertAccounts_NoResultIsNotFlagged(t *testing.T) {
	ctx := context.Background()
	q := alertTestQueries(t)

	cards := []dashMineCard{{ID: "acc_new", Name: "new", State: "sleeping"}}
	if got := authAlertAccounts(ctx, q, cards); got["acc_new"] {
		t.Fatal("account with no auth-check result must not be flagged")
	}
}

// When BOTH signals fire for one account, the operator needs one row, not two.
func TestBuildAlerts_NoDuplicateForBothSignals(t *testing.T) {
	ctx := context.Background()
	q := alertTestQueries(t)
	writeAuthResult(t, q, "acc_dead", false)

	cards := []dashMineCard{{ID: "acc_dead", Name: "dead", State: "needs_auth"}}
	alerts := buildDashAlerts(ctx, q, cards, "en")

	var n int
	for _, a := range alerts {
		if a.Kind == "needs_auth" && a.Account == "dead" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("expected exactly 1 needs_auth alert, got %d", n)
	}
}

// The pre-existing alert kinds must survive the refactor.
func TestBuildAlerts_KeepsOtherKinds(t *testing.T) {
	ctx := context.Background()
	q := alertTestQueries(t)

	cards := []dashMineCard{
		{ID: "acc_t", Name: "t", Platform: "twitch", State: "sleeping"},
		{ID: "acc_c", Name: "c", State: "awaiting_connect"},
		{ID: "acc_g", Name: "g", State: "no_games"},
	}
	alerts := buildDashAlerts(ctx, q, cards, "en")

	kinds := map[string]bool{}
	for _, a := range alerts {
		kinds[a.Kind] = true
	}
	for _, want := range []string{"no_drops", "awaiting_connect", "no_games"} {
		if !kinds[want] {
			t.Fatalf("alert kind %q went missing after the refactor", want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api/ -run 'TestAuthAlertAccounts|TestBuildAlerts' -v`
Expected: FAIL to compile — `undefined: authAlertAccounts` and `undefined: buildDashAlerts`.

- [ ] **Step 3: Extract the alert loop into a testable function**

In `internal/api/handlers_dashboard.go`, delete the inline alert-building block (the `var alerts []dashAlert` loop currently at `:309-337`) and replace it with a single call:

```go
	alerts := buildDashAlerts(r.Context(), d.q, cards, lang)
```

Then add both new functions below `collectPage` (keeping them in this file, next to the `dashAlert` type they build):

```go
// authAlertAccounts returns the set of account IDs whose PERSISTED auth-health
// check failed. This is a second, independent signal from scheduler state: an
// account that is merely idle never flips to "needs_auth", so before this the
// dashboard could read green while the account's session was dead.
func authAlertAccounts(ctx context.Context, q *gen.Queries, cards []dashMineCard) map[string]bool {
	failed := make(map[string]bool, len(cards))
	for _, c := range cards {
		res, ok := authcheck.Load(ctx, q, c.ID)
		if !ok {
			continue // never checked yet — not evidence of a problem
		}
		if !res.OK {
			failed[c.ID] = true
		}
	}
	return failed
}

// buildDashAlerts turns the mining cards into top-of-page CTA banners. An
// account is "needs auth" if EITHER the scheduler says so OR its persisted
// auth check failed, OR'd into one alert so the operator sees one row per
// broken account rather than two.
func buildDashAlerts(ctx context.Context, q *gen.Queries, cards []dashMineCard, lang string) []dashAlert {
	authFailed := authAlertAccounts(ctx, q, cards)
	var alerts []dashAlert
	for _, c := range cards {
		if c.State == "needs_auth" || authFailed[c.ID] {
			alerts = append(alerts, dashAlert{
				Kind: "needs_auth", Account: c.Name,
				URL: "/accounts/" + c.ID + "/login", Action: i18n.T(lang, "dashboard.action_re_auth"),
			})
			continue // one row per broken account
		}
		switch c.State {
		case "sleeping":
			if c.Platform == "twitch" {
				alerts = append(alerts, dashAlert{
					Kind: "no_drops", Account: c.Name,
					URL: "/accounts/" + c.ID + "/login", Action: i18n.T(lang, "dashboard.action_device_code"),
				})
			}
		case "awaiting_connect":
			alerts = append(alerts, dashAlert{
				Kind: "awaiting_connect", Account: c.Name,
				URL: "/drops", Action: i18n.T(lang, "dashboard.action_connect"),
			})
		case "no_games":
			alerts = append(alerts, dashAlert{
				Kind: "no_games", Account: c.Name,
				URL: "/accounts/" + c.ID, Action: i18n.T(lang, "dashboard.action_add_games"),
			})
		}
	}
	return alerts
}
```

The `continue` after the `needs_auth` append is what guarantees one row: an account that is both `needs_auth` and auth-failed cannot also fall through into the `switch`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/api/ -run 'TestAuthAlertAccounts|TestBuildAlerts' -v`
Expected: PASS, four tests.

- [ ] **Step 5: Run the full api suite for regressions**

The dashboard has existing tests (`handlers_dashboard_test.go`, 241 lines) that may assert on alerts.

Run: `go test ./internal/api/ 2>&1 | tail -20`
Expected: `ok`. If a pre-existing test fails, read it before changing it — a genuine behaviour change needs the test updated *and* called out; do not delete an assertion to get green.

- [ ] **Step 6: Changelog + commit**

Add under `## [Unreleased]` → `### Fixed` in `docs/CHANGELOG.md`:

```markdown
- **The dashboard no longer reads green while an account is dead.** Broken auth
  was tracked by two separate signals that could disagree: the watcher's live
  state, which drove the dashboard banner, and the hourly auth check, which
  only showed on the Accounts page. An account sitting idle never flipped its
  watcher state, so a dead session raised no dashboard alert at all. Both
  signals now feed the same banner, and an account that trips both still shows
  a single row.
```

```bash
gofmt -w ./internal/api
git add internal/api/handlers_dashboard.go internal/api/handlers_dashboard_alerts_test.go docs/CHANGELOG.md
git commit -m "fix(dashboard): raise needs-auth alert from the auth check too

The alert list was built only from scheduler state, so an idle account
could fail the hourly auth check while the dashboard showed nothing. OR
the persisted authcheck result into the same needs_auth alert, deduped to
one row per account, and extract the loop into buildDashAlerts so it is
testable."
```

---

### Task 3: Capture and persist rotated Kick cookies

The Kick utls client discards `Set-Cookie` entirely, and there is no runtime session-persist path at all. `SessionStore.Put` is called only from the two login handlers and the boot-time entry build, and that boot-time branch is gated on `s.ExpiresAt.Before(time.Now())` **and** a non-empty `s.RefreshToken` (`main.go:393-397`) — a Kick session has neither, so it never fires for Kick.

**Files:**
- Create: `internal/platform/kick/cookies.go`
- Create: `internal/platform/kick/cookies_test.go`
- Create: `internal/platform/kick/backend_persist_test.go`
- Modify: `internal/platform/kick/transport.go` (`httpDoer` struct at `:36-44`, `newHTTPDoer` at `:56`, `getRaw` at `:130`, `do` at `:170`)
- Modify: `internal/platform/kick/backend.go` (`RefreshSession` at `:352`)
- Modify: `cmd/miner/main.go` (Kick backend construction)

**Interfaces:**
- Consumes: `kickSession{Cookies []cookie; XSRFToken, UserAgent string}` and `cookie{Name, Value, Domain, Path string}` (`internal/platform/kick/types.go:13-27`); `decodeSession(platform.Session) (kickSession, error)` at `:41`; `encodeSession(kickSession) (platform.Session, error)` at `:30`; `cookieHeaderFor(kickSession) (cookieHeader, xsrf, bearer string)` at `transport.go:234`.
- Produces:
  - `func mergeCookies(ks kickSession, set []*http.Cookie) (kickSession, bool)` — returns the merged session and whether anything changed.
  - `var rotatableCookies = map[string]bool{...}` — the names that count.
  - `type SessionPersister func(accountID string, s platform.Session) error` in package `kick`.
  - `Backend.SessionPersister` field of that type.

- [ ] **Step 1: Write the failing merge test**

Create `internal/platform/kick/cookies_test.go`:

```go
package kick

import (
	"net/http"
	"testing"
)

// A rotated session_token must replace the stored one. This is the whole
// point: Kick reissuing a cookie mid-session was previously thrown away.
func TestMergeCookies_RotatedSessionTokenReplaces(t *testing.T) {
	ks := kickSession{Cookies: []cookie{
		{Name: "session_token", Value: "old"},
		{Name: "kick_session", Value: "ks1"},
	}}
	set := []*http.Cookie{{Name: "session_token", Value: "new"}}

	got, changed := mergeCookies(ks, set)
	if !changed {
		t.Fatal("a rotated session_token must report changed=true")
	}
	if v := cookieValue(got, "session_token"); v != "new" {
		t.Fatalf("session_token = %q, want new", v)
	}
	if v := cookieValue(got, "kick_session"); v != "ks1" {
		t.Fatalf("untouched cookie was lost: kick_session = %q, want ks1", v)
	}
}

// Analytics/consent cookies must never churn the stored session, or every
// request would look like a rotation and hammer the session store.
func TestMergeCookies_IgnoresIrrelevantNames(t *testing.T) {
	ks := kickSession{Cookies: []cookie{{Name: "session_token", Value: "old"}}}
	set := []*http.Cookie{
		{Name: "_ga", Value: "GA1.2.3"},
		{Name: "cf_clearance", Value: "abc"},
	}

	got, changed := mergeCookies(ks, set)
	if changed {
		t.Fatal("non-rotatable cookie names must not report a change")
	}
	if len(got.Cookies) != 1 {
		t.Fatalf("merge added unexpected cookies: %+v", got.Cookies)
	}
}

// Re-sending the SAME value is not a rotation.
func TestMergeCookies_SameValueIsNotAChange(t *testing.T) {
	ks := kickSession{Cookies: []cookie{{Name: "session_token", Value: "same"}}}
	set := []*http.Cookie{{Name: "session_token", Value: "same"}}

	if _, changed := mergeCookies(ks, set); changed {
		t.Fatal("an identical value must not report a change")
	}
}

// A rotatable cookie the session did not have yet is added.
func TestMergeCookies_AddsNewRotatableCookie(t *testing.T) {
	ks := kickSession{Cookies: []cookie{{Name: "session_token", Value: "tok"}}}
	set := []*http.Cookie{{Name: "XSRF-TOKEN", Value: "xsrf1"}}

	got, changed := mergeCookies(ks, set)
	if !changed {
		t.Fatal("a new rotatable cookie must report changed=true")
	}
	if v := cookieValue(got, "XSRF-TOKEN"); v != "xsrf1" {
		t.Fatalf("XSRF-TOKEN = %q, want xsrf1", v)
	}
}

// A rotated XSRF-TOKEN must also update the mirrored XSRFToken field, which
// cookieHeaderFor falls back to and encodeSession copies into Session.CSRF.
func TestMergeCookies_RotatedXSRFUpdatesMirrorField(t *testing.T) {
	ks := kickSession{
		Cookies:   []cookie{{Name: "XSRF-TOKEN", Value: "old"}},
		XSRFToken: "old",
	}
	set := []*http.Cookie{{Name: "XSRF-TOKEN", Value: "new"}}

	got, changed := mergeCookies(ks, set)
	if !changed {
		t.Fatal("expected changed=true")
	}
	if got.XSRFToken != "new" {
		t.Fatalf("XSRFToken mirror = %q, want new", got.XSRFToken)
	}
}

// An empty Set-Cookie value is a deletion signal from the server, not a
// rotation to empty — dropping a live token because of one would log the
// account out on the next request.
func TestMergeCookies_EmptyValueIsIgnored(t *testing.T) {
	ks := kickSession{Cookies: []cookie{{Name: "session_token", Value: "tok"}}}
	set := []*http.Cookie{{Name: "session_token", Value: ""}}

	got, changed := mergeCookies(ks, set)
	if changed {
		t.Fatal("an empty cookie value must not report a change")
	}
	if v := cookieValue(got, "session_token"); v != "tok" {
		t.Fatalf("session_token = %q, want the original tok", v)
	}
}

// No Set-Cookie at all leaves the session untouched.
func TestMergeCookies_NoSetCookieIsNoChange(t *testing.T) {
	ks := kickSession{Cookies: []cookie{{Name: "session_token", Value: "tok"}}}
	if _, changed := mergeCookies(ks, nil); changed {
		t.Fatal("nil Set-Cookie must not report a change")
	}
}

// cookieValue is a test helper: the value of the named cookie, or "".
func cookieValue(ks kickSession, name string) string {
	for _, c := range ks.Cookies {
		if c.Name == name {
			return c.Value
		}
	}
	return ""
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/platform/kick/ -run TestMergeCookies -v`
Expected: FAIL to compile — `undefined: mergeCookies`.

- [ ] **Step 3: Implement the merge**

Create `internal/platform/kick/cookies.go`:

```go
package kick

import "net/http"

// rotatableCookies are the ONLY cookie names a Set-Cookie response may
// update in a stored session. These are the three the session actually
// authenticates with, per cookieHeaderFor: session_token is the Sanctum
// bearer, XSRF-TOKEN is the CSRF header, kick_session is the web session.
// Restricting the set keeps analytics and Cloudflare cookies from churning
// the session store on every single request.
var rotatableCookies = map[string]bool{
	"session_token": true,
	"XSRF-TOKEN":    true,
	"kick_session":  true,
}

// mergeCookies folds a response's Set-Cookie headers into a stored session,
// returning the merged session and whether any authenticating cookie actually
// changed. Cookies not named in the response are preserved untouched.
//
// Kick reissues these cookies mid-session; before this they were discarded,
// so an account rode its original login until it expired.
func mergeCookies(ks kickSession, set []*http.Cookie) (kickSession, bool) {
	if len(set) == 0 {
		return ks, false
	}
	out := kickSession{
		Cookies:   make([]cookie, len(ks.Cookies)),
		XSRFToken: ks.XSRFToken,
		UserAgent: ks.UserAgent,
	}
	copy(out.Cookies, ks.Cookies)

	changed := false
	for _, sc := range set {
		if sc == nil || !rotatableCookies[sc.Name] {
			continue
		}
		// An empty value is the server DELETING a cookie. Honouring it would
		// discard a live token and log the account out on the next request,
		// so treat it as "no information" instead.
		if sc.Value == "" {
			continue
		}
		idx := -1
		for i, c := range out.Cookies {
			if c.Name == sc.Name {
				idx = i
				break
			}
		}
		if idx == -1 {
			out.Cookies = append(out.Cookies, cookie{Name: sc.Name, Value: sc.Value})
			changed = true
		} else if out.Cookies[idx].Value != sc.Value {
			out.Cookies[idx].Value = sc.Value
			changed = true
		}
		// Keep the mirrored XSRFToken in step: cookieHeaderFor falls back to
		// it and encodeSession copies it into Session.CSRF.
		if sc.Name == "XSRF-TOKEN" && out.XSRFToken != sc.Value {
			out.XSRFToken = sc.Value
			changed = true
		}
	}
	return out, changed
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/platform/kick/ -run TestMergeCookies -v`
Expected: PASS, seven tests.

- [ ] **Step 5: Commit the pure merge logic**

```bash
gofmt -w ./internal/platform/kick
git add internal/platform/kick/cookies.go internal/platform/kick/cookies_test.go
git commit -m "feat(kick): mergeCookies folds Set-Cookie into a stored session

Pure merge step for capturing rotated Kick cookies. Only the three names
the session authenticates with can rotate; empty values are treated as no
information rather than as a deletion, so a live token is never dropped."
```

- [ ] **Step 6: Write the failing persister test**

Create `internal/platform/kick/backend_persist_test.go`:

```go
package kick

import (
	"net/http"
	"testing"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// sessionFor builds a platform.Session carrying the given cookies.
func sessionFor(t *testing.T, accountID string, cookies ...cookie) platform.Session {
	t.Helper()
	s, err := encodeSession(kickSession{Cookies: cookies})
	if err != nil {
		t.Fatalf("encodeSession: %v", err)
	}
	s.AccountID = accountID
	return s
}

// A rotation must reach the session store exactly once, or the fresh cookie
// dies with the process and nothing is gained.
func TestCaptureCookies_PersistsOnChange(t *testing.T) {
	var gotAccount string
	var calls int
	b := &Backend{SessionPersister: func(accountID string, _ platform.Session) error {
		calls++
		gotAccount = accountID
		return nil
	}}

	sess := sessionFor(t, "acc_1", cookie{Name: "session_token", Value: "old"})
	b.captureCookies(sess, []*http.Cookie{{Name: "session_token", Value: "new"}})

	if calls != 1 {
		t.Fatalf("persister calls = %d, want 1", calls)
	}
	if gotAccount != "acc_1" {
		t.Fatalf("persisted under account %q, want acc_1", gotAccount)
	}
}

// No rotation means no write — otherwise every API call would hit the
// session store, which encrypts on every Put.
func TestCaptureCookies_NoWriteWithoutChange(t *testing.T) {
	var calls int
	b := &Backend{SessionPersister: func(string, platform.Session) error {
		calls++
		return nil
	}}

	sess := sessionFor(t, "acc_1", cookie{Name: "session_token", Value: "same"})
	b.captureCookies(sess, []*http.Cookie{{Name: "session_token", Value: "same"}})

	if calls != 0 {
		t.Fatalf("persister calls = %d, want 0", calls)
	}
}

// With no account id there is nothing to key the write on, so it must be
// skipped rather than written under an empty key.
func TestCaptureCookies_SkipsWithoutAccountID(t *testing.T) {
	var calls int
	b := &Backend{SessionPersister: func(string, platform.Session) error {
		calls++
		return nil
	}}

	sess := sessionFor(t, "", cookie{Name: "session_token", Value: "old"})
	b.captureCookies(sess, []*http.Cookie{{Name: "session_token", Value: "new"}})

	if calls != 0 {
		t.Fatalf("persister calls = %d, want 0 for an empty account id", calls)
	}
}

// A backend with no persister wired (tests, any non-production caller) must
// not panic.
func TestCaptureCookies_NilPersisterIsNoop(t *testing.T) {
	b := &Backend{}
	sess := sessionFor(t, "acc_1", cookie{Name: "session_token", Value: "old"})
	b.captureCookies(sess, []*http.Cookie{{Name: "session_token", Value: "new"}})
}

// RefreshSession must hand back the freshest captured cookie rather than
// returning its input unchanged, which is what it did before.
func TestRefreshSession_ReturnsCapturedCookie(t *testing.T) {
	b := &Backend{}
	sess := sessionFor(t, "acc_1", cookie{Name: "session_token", Value: "old"})
	b.captureCookies(sess, []*http.Cookie{{Name: "session_token", Value: "new"}})

	got, err := b.RefreshSession(nil, sess)
	if err != nil {
		t.Fatalf("RefreshSession: %v", err)
	}
	ks, err := decodeSession(got)
	if err != nil {
		t.Fatalf("decodeSession: %v", err)
	}
	if v := cookieValue(ks, "session_token"); v != "new" {
		t.Fatalf("RefreshSession returned session_token = %q, want new", v)
	}
}

// An account with nothing captured gets its input back untouched.
func TestRefreshSession_PassthroughWhenNothingCaptured(t *testing.T) {
	b := &Backend{}
	sess := sessionFor(t, "acc_1", cookie{Name: "session_token", Value: "tok"})

	got, err := b.RefreshSession(nil, sess)
	if err != nil {
		t.Fatalf("RefreshSession: %v", err)
	}
	ks, err := decodeSession(got)
	if err != nil {
		t.Fatalf("decodeSession: %v", err)
	}
	if v := cookieValue(ks, "session_token"); v != "tok" {
		t.Fatalf("session_token = %q, want the original tok", v)
	}
}
```

- [ ] **Step 7: Run test to verify it fails**

Run: `go test ./internal/platform/kick/ -run 'TestCaptureCookies|TestRefreshSession' -v`
Expected: FAIL to compile — `unknown field SessionPersister` and `b.captureCookies undefined`.

- [ ] **Step 8: Implement the capture and persist seam**

In `internal/platform/kick/backend.go`, add the type above `type Backend struct`:

```go
// SessionPersister writes an updated session back to durable storage. Wired
// in cmd/miner to store.SessionStore.Put. Needed because nothing else
// persists a session at runtime: SessionStore.Put is otherwise called only
// by the two login handlers and by the boot-time watcher build, and that
// boot-time path is gated on an expiry plus a refresh token, neither of
// which a Kick session has.
type SessionPersister func(accountID string, s platform.Session) error
```

Add to the `Backend` struct:

```go
	// SessionPersister persists a rotated session. Nil disables persistence
	// (the capture still updates the in-memory freshest copy).
	SessionPersister SessionPersister

	// freshest holds the newest captured session per account, so
	// RefreshSession can hand back a rotated cookie even before the next
	// process restart. Guarded by mu, which the Backend already holds.
	freshest map[string]kickSession
```

Add the capture method:

```go
// captureCookies folds a response's Set-Cookie into the account's stored
// session. It updates the in-memory freshest copy and, when an
// authenticating cookie actually changed, persists it.
//
// Called on every Kick API response, so the no-change path must stay cheap:
// SessionStore.Put encrypts, and writing on every request would be wasteful.
func (b *Backend) captureCookies(sess platform.Session, set []*http.Cookie) {
	if len(set) == 0 {
		return
	}
	ks, err := decodeSession(sess)
	if err != nil {
		return // not a Kick session blob; nothing to merge
	}
	merged, changed := mergeCookies(ks, set)
	if !changed {
		return
	}
	accountID := sess.AccountID
	if accountID == "" {
		// Nothing to key the write on. Discovery and canary borrow a shared
		// session without an account id; the watcher always sets one
		// (internal/watcher/watcher.go:266).
		slog.Debug("kick: rotated cookie with no account id, not persisting")
		return
	}

	b.mu.Lock()
	if b.freshest == nil {
		b.freshest = map[string]kickSession{}
	}
	b.freshest[accountID] = merged
	b.mu.Unlock()

	if b.SessionPersister == nil {
		return
	}
	updated, err := encodeSession(merged)
	if err != nil {
		slog.Warn("kick: encode rotated session failed", "account", accountID, "err", err)
		return
	}
	updated.AccountID = accountID
	if err := b.SessionPersister(accountID, updated); err != nil {
		slog.Warn("kick: persist rotated session failed", "account", accountID, "err", err)
		return
	}
	slog.Info("kick: session cookie rotated and persisted", "account", accountID)
}
```

Replace `RefreshSession` at `:352`:

```go
// RefreshSession returns the freshest captured session for the account.
// Kick has no server-side refresh endpoint, but it does reissue cookies on
// ordinary requests; captureCookies banks those, so this hands back the
// newest one instead of the caller's possibly-stale copy.
func (b *Backend) RefreshSession(_ context.Context, s platform.Session) (platform.Session, error) {
	if s.AccountID == "" {
		return s, nil
	}
	b.mu.Lock()
	ks, ok := b.freshest[s.AccountID]
	b.mu.Unlock()
	if !ok {
		return s, nil
	}
	updated, err := encodeSession(ks)
	if err != nil {
		return s, nil // never fail a refresh over an encode problem
	}
	updated.AccountID = s.AccountID
	return updated, nil
}
```

Add `"net/http"` and `"log/slog"` to the imports if not already present.

- [ ] **Step 9: Run test to verify it passes**

Run: `go test ./internal/platform/kick/ -run 'TestCaptureCookies|TestRefreshSession' -v`
Expected: PASS, six tests.

- [ ] **Step 10: Wire the hook through the transport**

`httpDoer.do` already holds the `*http.Response` after `RoundTrip` (`transport.go:220`), so `resp.Cookies()` is available with no new plumbing. But `do` returns `([]byte, int, error)` and has many callers, so its signature must not change. Add a hook instead.

In `internal/platform/kick/transport.go`, add to the `httpDoer` struct:

```go
	// onCookies, when set, receives the Set-Cookie headers of every response
	// so the backend can bank a rotated session cookie. Nil is a no-op.
	onCookies func(sess platform.Session, set []*http.Cookie)
```

In `do`, immediately after `defer resp.Body.Close()`:

```go
	if d.onCookies != nil {
		if set := resp.Cookies(); len(set) > 0 {
			d.onCookies(sess, set)
		}
	}
```

Leave `getRaw` alone. It fetches public CDN assets on `files.kick.com` with **no session cookies at all** (see its doc comment at `:126-129`) and has no `platform.Session` to merge into, so there is nothing there to capture. The spec's mention of `getRaw` is superseded by this: adding a hook to it would only ever fire for CDN cookies, all of which are non-rotatable and would be dropped by `mergeCookies` anyway.

Then, wherever the `Backend` constructs its `httpDoer` (search `newHTTPDoer(` in `internal/platform/kick/`), set the hook after construction:

```go
	doer.onCookies = b.captureCookies
```

If the doer is built before `b` exists, assign the hook right after the `Backend` value is constructed instead. Do not change `newHTTPDoer`'s signature — every existing call site passes only `dial`.

- [ ] **Step 11: Verify the transport wiring compiles and the package is green**

Run: `go build ./... && go test ./internal/platform/kick/ -v 2>&1 | tail -30`
Expected: build clean; all kick tests PASS, including the pre-existing `backend_test.go` and `sidecars_test.go`.

- [ ] **Step 12: Wire SessionPersister in main.go**

Find the Kick backend construction in `cmd/miner/main.go` (search `kickBackend`). After it is built and while `sessions` is in scope, add:

```go
		// Persist a rotated Kick cookie so a reissued session survives a
		// restart. Nothing else writes a session at runtime: SessionStore.Put
		// is otherwise reached only from the login handlers and the boot-time
		// entry build, whose refresh branch needs an expiry plus a refresh
		// token that a Kick session does not have.
		kickBackend.SessionPersister = func(accountID string, s platform.Session) error {
			return sessions.Put(ctx, accountID, s)
		}
```

Use whichever context variable the surrounding code uses for long-lived work (the same one passed to `authChecker.Run`), not a request context.

- [ ] **Step 13: Full verification**

Run: `gofmt -l ./cmd ./internal`
Expected: no output.

Run: `go build ./... && go vet ./... && go test ./... 2>&1 | tail -35`
Expected: all green.

- [ ] **Step 14: Changelog + commit**

Add under `## [Unreleased]` in `docs/CHANGELOG.md`:

```markdown
### Fixed

- **Kick logins last longer.** Kick reissues session cookies on ordinary API
  responses, but the miner threw every one of them away, so an account rode
  the cookies from its original `cookies.txt` paste until they expired and
  mining silently stopped. Reissued cookies are now folded into the stored
  session and saved, so a session that Kick keeps refreshing stays alive
  instead of aging out. Only the three cookies the session actually
  authenticates with can update, so analytics cookies never touch it.
```

```bash
gofmt -w ./cmd ./internal
git add internal/platform/kick/ cmd/miner/main.go docs/CHANGELOG.md
git commit -m "fix(kick): persist rotated session cookies

Kick reissues session_token/XSRF-TOKEN/kick_session on normal responses,
but the utls client discarded every Set-Cookie and no runtime session
persist path existed at all: SessionStore.Put was reached only from the
login handlers and from a boot-time branch gated on an expiry plus a
refresh token, neither of which a Kick session has.

Capture Set-Cookie via an httpDoer hook, merge only authenticating names,
and persist through a new SessionPersister wired to SessionStore.Put.
RefreshSession now returns the freshest captured session.

NOT TAGGABLE until live-verified against a Kick session (touches the Kick
watch path)."
```

---

### Task 4: Round-trip cookies out of the Kick sidecar

The Kick sidecar only ever *sets* cookies (`sidecar/kick.go:84`) and never reads them back, whereas the Twitch sidecar does (`sidecar/twitch.go:1303,1378`, commented "may round-trip refreshed cookies"). Any cookie Kick rotates during a sidecar watch is lost.

**Files:**
- Modify: `internal/auth/browser/sidecar/kick.go`
- Test: `internal/auth/browser/sidecar/kick_test.go:196` (extend the existing file)

**Interfaces:**
- Consumes: the existing `KickSession` proto message, which already has a `Cookies []*Cookie` field (`internal/auth/browser/gen/browser/v1/browser.pb.go:136`) — **no proto change and no `buf generate` run needed**. `mergeCookies` from Task 3 is *not* used here; the sidecar returns a full cookie set, not `Set-Cookie` deltas.
- Produces: cookies populated on the response message the Kick watch RPC already returns.

- [ ] **Step 1: Read the current watch RPC and its test**

Before writing code, read `internal/auth/browser/sidecar/kick.go` around the watch entry point and `internal/auth/browser/sidecar/kick_test.go` in full (196 lines). The existing tests establish how a chromedp context is faked in this package; follow that pattern exactly rather than inventing one.

Run: `go test ./internal/auth/browser/sidecar/ -v 2>&1 | head -40`
Expected: PASS — confirms the baseline before changes.

- [ ] **Step 2: Write the failing test**

Append to `internal/auth/browser/sidecar/kick_test.go` a test asserting that the watch response carries the cookies read back out of the browser. Model it on whichever existing test in that file exercises the watch path, reusing its fake/harness. The assertion:

```go
// Kick rotates session cookies during playback. The sidecar must hand the
// post-watch cookie set back to the caller the way the Twitch sidecar does,
// or a rotation that happens inside the browser is lost.
// (Wire the fake exactly as the neighbouring watch tests in this file do.)
```

Assert: after the watch returns, the response's `Cookies` field is non-empty and contains the rotated value the fake browser was primed with.

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/auth/browser/sidecar/ -run Kick -v`
Expected: FAIL — the response's `Cookies` field is empty.

- [ ] **Step 4: Implement the read-back**

In the Kick watch path in `internal/auth/browser/sidecar/kick.go`, after the watch completes and before returning, read the cookies back and attach them, mirroring `sidecar/twitch.go:1303`:

```go
	// Read cookies back out: Kick reissues session cookies during playback,
	// and the Twitch sidecar already round-trips them this way. Without this
	// a rotation that happens inside the browser is discarded.
	cookies, err := network.GetCookies().WithURLs([]string{"https://kick.com/"}).Do(ctx)
	if err != nil {
		// Best-effort: a failed read must never fail the watch.
		slog.Debug("kick sidecar: cookie read-back failed", "err", err)
	}
```

Then map each `*network.Cookie` into the response's `Cookies` field using the same `*browserv1.Cookie` construction the login path already uses in this file — find it by searching for `browserv1.Cookie{` and copy that mapping rather than writing a new one.

Do not embed any outside-controlled value into a chromedp `Evaluate` string here; `GetCookies` is a CDP command, not an eval, so there is nothing to encode.

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/auth/browser/sidecar/ -run Kick -v`
Expected: PASS.

- [ ] **Step 6: Full verification**

Run: `gofmt -l ./internal && go build ./... && go test ./... 2>&1 | tail -35`
Expected: no unformatted files; all green.

- [ ] **Step 7: Changelog + commit**

Add under the existing `## [Unreleased]` → `### Fixed`:

```markdown
- **Kick cookie refreshes during browser playback are no longer lost.** The
  Chrome sidecar pushed the account's cookies into the browser but never read
  them back, so any cookie Kick reissued while the stream played was discarded
  when the tab closed. The sidecar now hands the post-watch cookies back, the
  same way the Twitch sidecar already did.
```

```bash
gofmt -w ./internal
git add internal/auth/browser/sidecar/ docs/CHANGELOG.md
git commit -m "fix(kick sidecar): round-trip cookies back out after watch

sidecar/kick.go only ever called network.SetCookie and never read cookies
back, unlike the Twitch sidecar, so a cookie Kick reissued during IVS
playback died with the tab. Uses the existing KickSession.Cookies proto
field, so no proto regeneration.

NOT TAGGABLE until live-verified (touches the Kick watch path)."
```

---

### Task 5: Snapshot download

**Files:**
- Create: `internal/api/handlers_snapshot.go`
- Create: `internal/api/handlers_snapshot_test.go`
- Modify: `internal/api/server.go` (route table, near the other `/settings` POSTs at `:353-366`)
- Modify: `internal/web/templates/settings.html` (Health tab)
- Modify: `internal/i18n/locales/en.json`, `es.json`, `zh-CN.json`

**Interfaces:**
- Consumes: `Deps.DB *sql.DB` (`internal/api/server.go:71`). The CSRF middleware already wraps every mounted route (`server.go:379`).
- Produces: `func (d settingsDeps) postSnapshot(w http.ResponseWriter, r *http.Request)` — match the receiver type the neighbouring settings handlers in `handlers_settings.go` use; read that file's handler signatures first and follow them.

- [ ] **Step 1: Write the failing test**

Create `internal/api/handlers_snapshot_test.go`:

```go
package api

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/aalejandrofer/grubdrops/internal/store"
)

// A snapshot must be a real, openable SQLite database containing the live
// data — not a truncated or torn copy. VACUUM INTO is the reason this button
// exists instead of telling the operator to cp a live WAL database.
func TestWriteSnapshot_ProducesOpenableCopy(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "src.db"))
	if err != nil {
		t.Fatalf("open source db: %v", err)
	}
	defer db.Close()

	if _, err := db.ExecContext(ctx,
		`INSERT INTO kv (key, value) VALUES ('snapshot_probe', 'present')`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	dest := filepath.Join(t.TempDir(), "snap.db")
	if err := writeSnapshot(ctx, db, dest); err != nil {
		t.Fatalf("writeSnapshot: %v", err)
	}

	fi, err := os.Stat(dest)
	if err != nil {
		t.Fatalf("stat snapshot: %v", err)
	}
	if fi.Size() == 0 {
		t.Fatal("snapshot file is empty")
	}

	copied, err := sql.Open("sqlite", dest)
	if err != nil {
		t.Fatalf("open snapshot: %v", err)
	}
	defer copied.Close()

	var got string
	if err := copied.QueryRowContext(ctx,
		`SELECT value FROM kv WHERE key = 'snapshot_probe'`).Scan(&got); err != nil {
		t.Fatalf("read from snapshot: %v", err)
	}
	if got != "present" {
		t.Fatalf("snapshot value = %q, want present", got)
	}
}

// VACUUM INTO refuses to overwrite, so a stale temp file must not wedge the
// button permanently.
func TestWriteSnapshot_FailsOnExistingDest(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "src.db"))
	if err != nil {
		t.Fatalf("open source db: %v", err)
	}
	defer db.Close()

	dest := filepath.Join(t.TempDir(), "snap.db")
	if err := os.WriteFile(dest, []byte("stale"), 0o600); err != nil {
		t.Fatalf("write stale file: %v", err)
	}
	if err := writeSnapshot(ctx, db, dest); err == nil {
		t.Fatal("expected an error when the destination already exists")
	}
}
```

The `sql.Open("sqlite", ...)` driver name must match what `internal/store/db.go` registers — read `store.Open` at `internal/store/db.go:16` and use the identical driver string.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api/ -run TestWriteSnapshot -v`
Expected: FAIL to compile — `undefined: writeSnapshot`.

- [ ] **Step 3: Implement writeSnapshot**

Create `internal/api/handlers_snapshot.go`:

```go
package api

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// writeSnapshot writes a consistent copy of the live database to dest using
// VACUUM INTO. This is the point of the feature: copying miner.db with cp
// while the miner runs can capture a torn WAL state, whereas VACUUM INTO is
// consistent against a live database. dest must not already exist — VACUUM
// INTO refuses to overwrite.
func writeSnapshot(ctx context.Context, db *sql.DB, dest string) error {
	// dest is built by the caller from os.MkdirTemp plus a fixed name, never
	// from request input, so there is no injection surface here.
	if _, err := db.ExecContext(ctx, fmt.Sprintf("VACUUM INTO %s", quoteSQLiteString(dest))); err != nil {
		return fmt.Errorf("vacuum into: %w", err)
	}
	return nil
}

// quoteSQLiteString renders s as a SQLite string literal. VACUUM INTO takes a
// literal path, not a bound parameter, so the path must be quoted rather than
// passed as an argument.
func quoteSQLiteString(s string) string {
	out := make([]rune, 0, len(s)+2)
	out = append(out, '\'')
	for _, r := range s {
		if r == '\'' {
			out = append(out, '\'') // SQLite escapes a quote by doubling it
		}
		out = append(out, r)
	}
	out = append(out, '\'')
	return string(out)
}
```

**Known tradeoff, accepted:** `store.Open` sets `db.SetMaxOpenConns(1)`
(`internal/store/db.go:24`) to serialize writers and avoid `SQLITE_BUSY`
storms. `VACUUM INTO` therefore holds the app's only connection for its
duration, so a snapshot of a large database briefly stalls other DB work
(dashboard polls, watcher progress writes). Acceptable for a manual,
operator-initiated action on a database that is a few megabytes in normal use.
Do not add a "snapshot on a timer" on top of this without revisiting that
decision — the spec already rules scheduled backups out of scope.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/api/ -run TestWriteSnapshot -v`
Expected: PASS, two tests.

- [ ] **Step 5: Add the handler**

Append to `internal/api/handlers_snapshot.go`. Match the receiver and helper names used by the neighbouring handlers in `handlers_settings.go` — read that file's `postCanary` handler first and mirror its receiver type and its flash/redirect style for the error path.

```go
// postSnapshot streams a consistent copy of the database as a download.
// POST (not GET) so it goes through the same CSRF middleware as every other
// admin action; it is an operator data-egress action and belongs on that
// footing even though it only reads.
func (d settingsDeps) postSnapshot(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if d.db == nil {
		http.Error(w, "snapshot unavailable", http.StatusServiceUnavailable)
		return
	}

	dir, err := os.MkdirTemp("", "grubdrops-snapshot-")
	if err != nil {
		slog.Warn("snapshot: temp dir failed", "err", err)
		http.Error(w, "snapshot failed", http.StatusInternalServerError)
		return
	}
	// Remove the whole directory, which covers the snapshot plus any -wal or
	// -shm sidecar file, on every exit path including a client disconnect.
	defer func() { _ = os.RemoveAll(dir) }()

	name := "grubdrops-" + time.Now().Format("20060102-1504") + ".db"
	dest := filepath.Join(dir, name)
	if err := writeSnapshot(ctx, d.db, dest); err != nil {
		slog.Warn("snapshot: vacuum failed", "err", err)
		http.Error(w, "snapshot failed", http.StatusInternalServerError)
		return
	}

	f, err := os.Open(dest)
	if err != nil {
		slog.Warn("snapshot: open failed", "err", err)
		http.Error(w, "snapshot failed", http.StatusInternalServerError)
		return
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		slog.Warn("snapshot: stat failed", "err", err)
		http.Error(w, "snapshot failed", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/x-sqlite3")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", fi.Size()))
	if _, err := io.Copy(w, f); err != nil {
		// Headers are already sent; nothing to report to the client.
		slog.Debug("snapshot: stream interrupted", "err", err)
	}
}
```

`name` is built from `time.Now().Format` only, never from request input, so the `Content-Disposition` value cannot carry injected header bytes.

If `settingsDeps` has no `db` field, add one and populate it from `Deps.DB` wherever `settingsDeps` is constructed in `internal/api/server.go`.

- [ ] **Step 6: Register the route**

In `internal/api/server.go`, alongside the other settings POSTs (near `:361`):

```go
	r.Post("/settings/snapshot", sd.postSnapshot)
```

Use the same receiver variable the neighbouring `r.Post("/settings/canary", ...)` line uses.

- [ ] **Step 7: Add the button and its translations**

In `internal/web/templates/settings.html`, inside the Health tab (near the canary panel include), add:

```html
<form method="post" action="/settings/snapshot" class="row">
  <input type="hidden" name="csrf_token" value="{{$.CSRFToken}}">
  <span class="k">{{t "health.snapshot"}}</span>
  <span class="v grow">
    <button type="submit" class="btn">{{t "health.snapshot_download"}}</button>
    <span class="hint">{{t "health.snapshot_desc"}}</span>
  </span>
</form>
```

Match the surrounding markup in that file — read the neighbouring rows and reuse their exact class names rather than the ones above if they differ.

Add to all three locale files, keeping keys in the same alphabetical position the files already use:

`internal/i18n/locales/en.json`:
```json
  "health.snapshot": "Database snapshot",
  "health.snapshot_download": "Download snapshot",
  "health.snapshot_desc": "A consistent copy of the database, safe to take while the miner is running.",
```

`internal/i18n/locales/es.json`:
```json
  "health.snapshot": "Copia de la base de datos",
  "health.snapshot_download": "Descargar copia",
  "health.snapshot_desc": "Una copia coherente de la base de datos, segura de hacer mientras el minero está funcionando.",
```

`internal/i18n/locales/zh-CN.json`:
```json
  "health.snapshot": "数据库快照",
  "health.snapshot_download": "下载快照",
  "health.snapshot_desc": "数据库的一致性副本，可在挖掘运行期间安全获取。",
```

- [ ] **Step 8: Verify locale parity**

All three locale files must have identical key counts — they are at 741 each today and must end at 744 each.

Run:
```bash
for f in internal/i18n/locales/*.json; do echo "$f: $(grep -c '":' $f)"; done
```
Expected: three identical counts, each 3 higher than before.

- [ ] **Step 9: Full verification**

Run: `gofmt -l ./internal && go build ./... && go vet ./... && go test ./... 2>&1 | tail -35`
Expected: no unformatted files; all green. `internal/web` has a template-parse test that will catch a malformed template or a missing translation key.

- [ ] **Step 10: Changelog + commit**

Add under `## [Unreleased]`:

```markdown
### Added

- **Download a database snapshot from Settings → Health.** Copying `miner.db`
  by hand while the miner is running can capture a half-written state; the new
  button uses SQLite's `VACUUM INTO` to write a consistent copy and streams it
  to you, so a backup no longer means stopping the container.
```

```bash
gofmt -w ./internal
git add internal/api/handlers_snapshot.go internal/api/handlers_snapshot_test.go internal/api/server.go internal/web/templates/settings.html internal/i18n/locales/ docs/CHANGELOG.md
git commit -m "feat(settings): download a consistent DB snapshot

VACUUM INTO a temp dir and stream it, so an operator can back up without
stopping the miner. cp of a live WAL database can capture a torn state.
POST behind the existing CSRF middleware; temp dir removed on every exit
path including client disconnect."
```

---

### Task 6: Verification pass and release gating

**Files:**
- Modify: `docs/CHANGELOG.md` (only if entries need tidying)

- [ ] **Step 1: Confirm the whole suite is green**

Run: `go build ./... && go vet ./... && go test ./... 2>&1 | tail -35`
Expected: build and vet silent; every package `ok` or `[no test files]`.

- [ ] **Step 2: Confirm the gofmt gate will pass**

Run: `gofmt -l ./cmd ./internal`
Expected: no output. CI fails fast on this.

- [ ] **Step 3: Verify the auth notification end to end against a real webhook**

Start the miner locally with a Discord webhook configured, then force a transition. With an account that has a session, temporarily corrupt it or point the account at a bad credential, and run the manual "check auth now" button on `/accounts`.

Expected: exactly one Discord message naming the account and the reason. Press the button again with the account still broken.
Expected: **no second message** — this is the edge-trigger guarantee, and the single most important behaviour to confirm by hand, because a regression here spams the operator hourly.

- [ ] **Step 4: Verify the snapshot download by hand**

Click Settings → Health → Download snapshot.

Expected: a `grubdrops-YYYYMMDD-HHMM.db` file downloads. Then confirm it opens:
```bash
sqlite3 ~/Downloads/grubdrops-*.db "select count(*) from accounts;"
```
Expected: a row count, no "file is not a database" error.

- [ ] **Step 5: Report the Kick live-verification status**

Tasks 3 and 4 touch the Kick watch path, so per the project release rules they cannot be tagged on unit tests alone. Production Kick account `acc_89de4e08` is expired and must be re-logged-in via cookie paste before this can run.

Once a live Kick session exists, watch the logs for:
```
kick: session cookie rotated and persisted
```
Its presence proves Kick rotates cookies and that the capture path works, which is the open question the design could not settle statically. Its **absence** over a full watch session is also a valid result: it means Kick does not rotate, the merge correctly found nothing to change, and Task 1's alert is what carries the chore.

Either way, confirm accrual still credits normally during that session — the capture runs on the same responses the watch path uses.

- [ ] **Step 6: Cut the release or hold**

If the Kick live verification passed: move all `[Unreleased]` entries into a new version heading in `docs/CHANGELOG.md`, push the `v*` tag, and write the release notes in the GitHub Releases tab as a curated subset using the emoji-section format (🌱 Added / ⚙️ Changed / 🐛 Fixed / 🔥 Removed), bold subjects, no em or en dashes, title = bare version.

If the Kick live verification has not happened yet: tag **only** Tasks 1, 2 and 5 by moving just those entries out of `[Unreleased]`, and leave the two Kick entries in `[Unreleased]` until a live session confirms them. Do not tag an unverified change to the Kick watch path.

---

## Self-Review

**Spec coverage:**

| Spec section | Task |
|---|---|
| 1. Edge-triggered auth notification (transition table, `account` field for routing, best-effort send) | 1 |
| 2. One expiry signal (OR into `needs_auth`, dedup to one row, existing CTA) | 2 |
| 3. utls `Set-Cookie` capture via `onCookies` hook, no signature change | 3 |
| 3. Rotatable-name restriction (`session_token`, `XSRF-TOKEN`, `kick_session`) | 3 |
| 3. `RefreshSession` returns freshest | 3 |
| 3. `SessionPersister` seam wired to `sessions.Put`, empty `AccountID` skipped, nil is no-op | 3 |
| 3. Kick sidecar `GetCookies` round-trip over existing proto field | 4 |
| 4. Snapshot download via `VACUUM INTO`, streamed, temp removed, POST behind CSRF | 5 |
| Testing table (all four rows) | 1, 2, 3, 5 |
| Release gating (1/2/5 taggable, 3/4 held for live Kick) | 6 |

Two deviations from the spec, both deliberate and noted inline in the plan:
1. The spec says hook both `do` and `getRaw`. Task 3 Step 10 hooks only `do`: `getRaw` sends **no session cookies** and has no `platform.Session` to merge into, so a hook there could never fire usefully.
2. The spec treated the sidecar read-back as part of item 3. It is split into Task 4 because it lives in a different package with a different test harness, and a reviewer could reasonably accept the transport work while rejecting the sidecar work.

**Placeholder scan:** No TBD/TODO/"handle edge cases"/"similar to Task N". Task 4 Steps 2 and 4 deliberately direct the implementer to read and mirror the existing harness and `browserv1.Cookie` mapping in that file rather than quoting invented code, because that package's test scaffolding was not read during planning and inventing a fake would be worse than pointing at the real one. Task 5 Steps 5 and 7 likewise direct the implementer to match the neighbouring receiver type and CSS classes.

**Type consistency:** `mergeCookies(kickSession, []*http.Cookie) (kickSession, bool)` is defined in Task 3 Step 3 and used in Step 8. `captureCookies(platform.Session, []*http.Cookie)` is defined in Step 8 and referenced by the hook in Step 10. `SessionPersister func(string, platform.Session) error` is defined in Step 8 and wired in Step 12 with matching parameters. `cookieValue` is a test helper defined once in `cookies_test.go` and reused in `backend_persist_test.go` — both are package `kick`, so that resolves. `writeSnapshot(context.Context, *sql.DB, string) error` is defined in Task 5 Step 3 and used in Steps 1 and 5. `authAlertAccounts` and `buildDashAlerts` are defined in Task 2 Step 3 with the signatures the Step 1 tests call. `WithNotifier` is defined in Task 1 Step 3 and used in Step 9; `persistWithMeta` is introduced in Step 7 with the signature the Step 5 test calls.

**Facts verified against source during review** (rather than assumed): the SQLite driver name is `"sqlite"` (`internal/store/db.go:19`), so the snapshot test's `sql.Open` call is correct; `kv` is `(key TEXT PRIMARY KEY, value BLOB NOT NULL)` (`migrations/0001_init.sql`), so the test's seed insert is valid; `gen.New(db)` exists (`internal/store/gen/db.go:19`); `Account.DisplayName` exists (`internal/store/gen/models.go:14`), so Task 1 Step 7's `a.DisplayName` resolves; `authcheck.Prefix` is exported (`authcheck.go:20`), so Task 2's test can build the kv key.

One correction applied during review: Task 5 Step 3's first draft of `quoteSQLiteString` returned a slice expression rather than a string. The plan now carries only the correct version — a wrong draft left in place is something an implementer would copy.
