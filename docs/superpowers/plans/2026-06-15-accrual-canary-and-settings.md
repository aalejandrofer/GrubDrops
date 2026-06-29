# Accrual canary + settings restructure — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an accrual canary (CI frame-replay regression + live transport probes) that proves Kick WS / Twitch watch-time still works without a live drop, plus a Settings restructure (Health tab, tab reorder, Logging split, orange headers) and a Console row-height fix. Ships as v1.2.0.

**Architecture:** A new `internal/canary` package runs standalone transport probes (not full watchers) against configured always-live channels, persists results in the existing KV settings store, and is wired into `cmd/miner` like the authcheck runner. CI regression tests replay recorded fixtures against the existing Kick WS parser and Twitch beacon builder. Settings changes are template/handler-only over the existing `html/template` + HTMX stack.

**Tech Stack:** Go, `html/template`, HTMX, sqlc/SQLite KV settings, gorilla/websocket + utls (Kick), testify.

---

## File structure

- `internal/canary/canary.go` — runner, scheduling, on-demand run, result type.
- `internal/canary/twitch.go` — Twitch beacon probe (reuses `twitch` watch).
- `internal/canary/kick.go` — Kick WS probe (reuses `kick` wswatch seams).
- `internal/canary/store.go` — result persistence over `*gen.Queries` KV.
- `internal/store/settings.go` — canary channel/interval getters+setters.
- `internal/api/handlers_settings.go` — Health tab handler, Run-now, canary settings POST.
- `internal/web/templates/settings.html` — Logging split, Health tab, tab reorder.
- `internal/web/templates/_nav.html` — (no change; nav unaffected).
- `internal/web/static/css/app.css` — orange headers, `.who` min-height.
- `cmd/miner/main.go` — wire canary runner.
- Fixtures: `internal/platform/kick/testdata/ws_frames.json`, `internal/platform/twitch/testdata/` (beacon golden).

---

## Task 1: Console row-height alignment (CSS)

**Files:** Modify `internal/web/static/css/app.css` (`.account-row .who`, ~line 881)

- [ ] **Step 1: Edit the `.who` rule** — add reserved height so pill-less rows match WS-pill rows.

Change line 881 from:
```css
  .account-row .who { display: flex; flex-direction: column; line-height: 1.2; min-width: 0; }
```
to:
```css
  .account-row .who { display: flex; flex-direction: column; justify-content: center; line-height: 1.2; min-width: 0; min-height: 30px; }
```

- [ ] **Step 2: Visually verify** — run the app locally (see Appendix A), Console with an idle Twitch + idle Kick account; confirm rows align at equal height. Tune `min-height` (28–32px) if off by a pixel.

- [ ] **Step 3: Commit**
```bash
git add internal/web/static/css/app.css
git commit -m "fix(dashboard): equal idle row height across Twitch/Kick (reserve WS-pill space)"
```

---

## Task 2: Orange section headers (CSS)

**Files:** Modify `internal/web/static/css/app.css`

- [ ] **Step 1: Add a rule** near the existing `.section-h` styles (search `.section-h`). Append:
```css
  .section-h h3 { color: var(--accent); }
```

- [ ] **Step 2: Visually verify** both themes — header text is orange on dark and light (accent is theme-aware).

- [ ] **Step 3: Commit**
```bash
git add internal/web/static/css/app.css
git commit -m "style(settings): orange section headers"
```

---

## Task 3: Split Logging out of Runtime (General tab)

**Files:** Modify `internal/web/templates/settings.html` (Runtime section ~lines 201-237)

- [ ] **Step 1: Edit the General tab** — keep the Runtime `<section>` with only tick + discovery rows; move the `log level` and `log retention` rows into a NEW `<section class="settings-card">` titled Logging, inside the same `{{if or (eq $.Active "settings") ...}}` block, after the Runtime card and before the Status card. Both sections post to the same `/settings` form OR give Logging its own form posting to `/settings` (same handler reads all fields; simplest: keep one shared `<form id="settings-form">` wrapping both cards). Concretely: move the two `<div class="row">` blocks (log_level, log_retention) out of Runtime's `.kvedit` into a new card:

```html
    <section class="settings-card">
      <header class="section-h"><h3>Logging</h3><span class="meta">verbosity + retention</span></header>
      <form method="post" action="/settings">
        <input type="hidden" name="csrf_token" value="{{$.CSRFToken}}">
        <div class="kvedit">
          <div class="row">
            <span class="k">log level</span>
            <span class="d">logging verbosity</span>
            <span class="v"><select name="log_level">
              <option value="" {{if eq .LogLevel ""}}selected{{end}}>default ({{.LogLevelEnv}})</option>
              <option value="debug" {{if eq .LogLevel "debug"}}selected{{end}}>debug</option>
              <option value="info" {{if eq .LogLevel "info"}}selected{{end}}>info</option>
              <option value="warn" {{if eq .LogLevel "warn"}}selected{{end}}>warn</option>
              <option value="error" {{if eq .LogLevel "error"}}selected{{end}}>error</option>
            </select></span>
          </div>
          <div class="row">
            <span class="k">log retention</span>
            <span class="d">how long log history is kept</span>
            <span class="v"><input type="number" name="log_retention_days" value="{{.LogRetentionDays}}" min="1" max="365"><span class="u">days</span></span>
          </div>
        </div>
        <div class="row" style="margin-top:18px;justify-content:flex-end;"><button class="btn-linear" type="submit">Save →</button></div>
      </form>
    </section>
```
Remove those two rows from the Runtime `.kvedit`. Runtime keeps tick + discovery only; its existing Save button stays.

- [ ] **Step 2: Verify parse + handler** — `postGeneral` already reads `log_level` and `log_retention_days` from the form, so no handler change. Confirm:
Run: `go test ./internal/web/ ./internal/api/ -run 'Templates|Settings' 2>/dev/null | tail`
Expected: PASS (templates parse).

- [ ] **Step 3: Commit**
```bash
git add internal/web/templates/settings.html
git commit -m "feat(settings): split Logging into its own section on General"
```

---

## Task 4: Tab reorder + Health tab shell

**Files:** Modify `internal/web/templates/settings.html` (subnav ~lines 17-22), `internal/api/handlers_settings.go` (routes + getHealth handler), wherever settings routes are registered (search `"/settings/experimental"`).

- [ ] **Step 1: Update the subnav** to the new order + add Health. Replace lines 17-22:
```html
    <a href="/settings" class="subnav-link {{if eq .Active "settings"}}on{{end}}">General</a>
    <a href="/settings/accounts" class="subnav-link {{if eq .Active "accounts"}}on{{end}}">Accounts</a>
    <a href="/settings/priority" class="subnav-link {{if eq .Active "priority"}}on{{end}}">Drop Priority</a>
    <a href="/settings/notifications" class="subnav-link {{if eq .Active "notifications"}}on{{end}}">Notifications</a>
    <a href="/settings/security" class="subnav-link {{if eq .Active "security"}}on{{end}}">Security</a>
    <a href="/settings/health" class="subnav-link {{if eq .Active "health"}}on{{end}}">Health</a>
    <a href="/settings/experimental" class="subnav-link {{if eq .Active "experimental"}}on{{end}}">Experimental</a>
```

- [ ] **Step 2: Add the getHealth handler** in `handlers_settings.go` mirroring `getExperimental`:
```go
func (d *settingsDeps) getHealth(w http.ResponseWriter, r *http.Request) {
	d.renderTab(w, r, "health")
}
```

- [ ] **Step 3: Register the route** next to the other `/settings/*` GETs (search `r.Get("/settings/experimental"`):
```go
r.Get("/settings/health", d.getHealth)
```

- [ ] **Step 4: Add the Health tab block** in `settings.html` after the Experimental block:
```html
{{if eq .Active "health"}}
<section class="settings-card">
  <header class="section-h"><h3>Accrual canary</h3><span class="meta">transport health</span></header>
  <div class="kvlist" id="canary-panel">
    {{/* filled in Task 13 */}}
    <div class="kvrow"><span class="k">canary</span><span class="v">—</span></div>
  </div>
</section>
{{end}}
```

- [ ] **Step 5: Update the subnav test** — `TestSettingsTabs_SubnavHasAllLinks` (internal/api/handlers_settings_test.go) add `href="/settings/health"` and "Health" to the wanted list.

- [ ] **Step 6: Run tests**
Run: `go test ./internal/api/ -run Settings 2>/dev/null | tail`
Expected: PASS.

- [ ] **Step 7: Commit**
```bash
git add internal/web/templates/settings.html internal/api/handlers_settings.go internal/api/handlers_settings_test.go
git commit -m "feat(settings): reorder tabs + add Health tab"
```

---

## Task 5: Move Status panel into Health tab

**Files:** Modify `internal/web/templates/settings.html` (Status section ~lines 239-249, currently in the General `{{if ... "settings"}}` block; the Health block from Task 4).

- [ ] **Step 1: Cut the Status `<section>`** (the `<h3>Status</h3>` card, lines ~239-249) out of the General block and paste it into the Health `{{if eq .Active "health"}}` block (after the canary panel). The data fields (`.Uptime`, `.GoVersion`, `.Goroutines`, `.Sidecars`, `.GitCommit`, `.Version`) are already on the page data for every tab via `renderTab`, so no handler change — but verify by checking `settingsPageData` is populated for the health tab the same as others (it is, since `renderTab` builds one struct).

- [ ] **Step 2: Run tests + visual** — render health tab, confirm Status shows.
Run: `go test ./internal/api/ -run Settings 2>/dev/null | tail`
Expected: PASS.

- [ ] **Step 3: Commit**
```bash
git add internal/web/templates/settings.html
git commit -m "feat(settings): move Status panel under Health tab"
```

---

## Task 6: Canary settings in the store

**Files:** Modify `internal/store/settings.go`; Test `internal/store/settings_test.go`

- [ ] **Step 1: Write the failing test** in `settings_test.go`:
```go
func TestSettings_Canary(t *testing.T) {
	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "t.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	s := NewSettings(gen.New(db))
	ctx := context.Background()

	// Defaults: Twitch alveussanctuary, Kick empty, interval 6h.
	tw, err := s.CanaryTwitchChannel(ctx)
	require.NoError(t, err)
	assert.Equal(t, "alveussanctuary", tw)
	kk, _ := s.CanaryKickChannel(ctx)
	assert.Equal(t, "", kk)
	iv, _ := s.CanaryIntervalSec(ctx)
	assert.Equal(t, 6*3600, iv)

	require.NoError(t, s.SetCanaryTwitchChannel(ctx, "somechannel"))
	tw, _ = s.CanaryTwitchChannel(ctx)
	assert.Equal(t, "somechannel", tw)
}
```

- [ ] **Step 2: Run, verify fail**
Run: `go test ./internal/store/ -run Canary 2>/dev/null | tail -3`
Expected: FAIL (undefined methods).

- [ ] **Step 3: Implement** in `settings.go` (mirror existing key/getter/setter patterns; add keys near the other `key*` consts):
```go
const (
	keyCanaryTwitchChannel = "settings:canary_twitch_channel"
	keyCanaryKickChannel   = "settings:canary_kick_channel"
	keyCanaryIntervalSec   = "settings:canary_interval_sec"
)

func (s *Settings) CanaryTwitchChannel(ctx context.Context) (string, error) {
	v, err := s.getString(ctx, keyCanaryTwitchChannel)
	if err != nil || v == "" {
		return "alveussanctuary", err
	}
	return v, nil
}
func (s *Settings) SetCanaryTwitchChannel(ctx context.Context, v string) error {
	return s.setString(ctx, keyCanaryTwitchChannel, strings.TrimSpace(v))
}
func (s *Settings) CanaryKickChannel(ctx context.Context) (string, error) {
	return s.getString(ctx, keyCanaryKickChannel) // "" default = skip Kick canary
}
func (s *Settings) SetCanaryKickChannel(ctx context.Context, v string) error {
	return s.setString(ctx, keyCanaryKickChannel, strings.TrimSpace(v))
}
func (s *Settings) CanaryIntervalSec(ctx context.Context) (int, error) {
	v, err := s.getInt(ctx, keyCanaryIntervalSec) // use the existing int getter helper
	if err != nil || v == 0 {
		return 6 * 3600, err
	}
	return v, nil
}
func (s *Settings) SetCanaryIntervalSec(ctx context.Context, n int) error {
	return s.setInt(ctx, keyCanaryIntervalSec, n)
}
```
(If `getInt`/`setInt` helpers don't exist, mirror `LogRetentionDays` which already does int storage — copy its strconv approach.)

- [ ] **Step 4: Run, verify pass**
Run: `go test ./internal/store/ -run Canary 2>/dev/null | tail -3`
Expected: PASS.

- [ ] **Step 5: Commit**
```bash
git add internal/store/settings.go internal/store/settings_test.go
git commit -m "feat(settings): canary channel + interval settings"
```

---

## Task 7: Canary result store

**Files:** Create `internal/canary/store.go`; Test `internal/canary/store_test.go`

- [ ] **Step 1: Write the failing test:**
```go
package canary

import (
	"context"
	"path/filepath"
	"testing"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/assert"
	"github.com/aalejandrofer/grubdrops/internal/store"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

func TestResultStore_RoundTrip(t *testing.T) {
	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "t.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	q := gen.New(db)
	ctx := context.Background()

	// No result yet.
	_, ok, err := LoadResult(ctx, q, "twitch")
	require.NoError(t, err)
	assert.False(t, ok)

	r := Result{OK: true, Detail: "2 beacons accepted"}
	require.NoError(t, SaveResult(ctx, q, "twitch", r))

	got, ok, err := LoadResult(ctx, q, "twitch")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.True(t, got.OK)
	assert.Equal(t, "2 beacons accepted", got.Detail)
	assert.False(t, got.CheckedAt.IsZero())
}
```

- [ ] **Step 2: Run, verify fail** — `go test ./internal/canary/ 2>/dev/null | tail -3` → FAIL (no package).

- [ ] **Step 3: Implement `store.go`** mirroring `internal/authcheck` persistence (it stores JSON in KV with a key prefix). Read `internal/authcheck/authcheck.go` Save/Load first and copy the JSON-in-KV pattern:
```go
package canary

import (
	"context"
	"encoding/json"
	"time"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

type Result struct {
	OK        bool      `json:"ok"`
	Detail    string    `json:"detail"`
	CheckedAt time.Time `json:"checked_at"`
}

const keyPrefix = "canary:"

func SaveResult(ctx context.Context, q *gen.Queries, platform string, r Result) error {
	r.CheckedAt = time.Now().UTC()
	b, err := json.Marshal(r)
	if err != nil { return err }
	return q.SetKV(ctx, gen.SetKVParams{Key: keyPrefix + platform, Value: string(b)}) // match authcheck's KV upsert query name
}

func LoadResult(ctx context.Context, q *gen.Queries, platform string) (Result, bool, error) {
	v, err := q.GetKV(ctx, keyPrefix+platform) // match authcheck's getter
	if err != nil { return Result{}, false, nil } // not-found = no result
	var r Result
	if err := json.Unmarshal([]byte(v), &r); err != nil { return Result{}, false, err }
	return r, true, nil
}
```
(Use the EXACT KV query names authcheck uses — read `internal/authcheck/authcheck.go` lines ~150-175 for `Save`/`Load` to copy the correct `gen` method names.)

- [ ] **Step 4: Run, verify pass** — `go test ./internal/canary/ 2>/dev/null | tail -3` → PASS.

- [ ] **Step 5: Commit**
```bash
git add internal/canary/store.go internal/canary/store_test.go
git commit -m "feat(canary): result persistence in KV"
```

---

## Task 8: Twitch beacon probe

**Files:** Create `internal/canary/twitch.go`; Test `internal/canary/twitch_test.go`. Reuse `internal/platform/twitch` watch (`watch.go` `start`/`heartbeat` = the minute-watched beacon).

- [ ] **Step 1: Read** `internal/platform/twitch/watch.go` (`newWatch`, `start`, `heartbeat`) and `watch_test.go` to learn how a beacon is built + how the test fakes the HTTP transport.

- [ ] **Step 2: Write the failing test** — probe returns OK when the beacon transport returns 2xx, fail otherwise. Use the same HTTP-faking approach as `watch_test.go` (inject a `RoundTripper` / test server):
```go
func TestTwitchProbe_OKWhenBeaconAccepted(t *testing.T) {
	// fake transport returning 204 for the watch beacon
	probe := newTwitchProbe(/* fake http client per watch_test pattern */)
	r := probe.Run(context.Background(), fakeSession, "alveussanctuary")
	if !r.OK { t.Fatalf("want OK, got %+v", r) }
}
```

- [ ] **Step 3: Run, verify fail.**

- [ ] **Step 4: Implement `twitch.go`** — resolve the channel to a stream (reuse the backend's stream lookup), call `watch.start` then `watch.heartbeat` twice ~60s apart (configurable short window for the probe; in tests use 0 delay), return `Result{OK: err==nil}`. Reuse existing twitch client; do NOT re-implement the beacon.

- [ ] **Step 5: Run, verify pass.**

- [ ] **Step 6: Commit**
```bash
git add internal/canary/twitch.go internal/canary/twitch_test.go
git commit -m "feat(canary): twitch watch-beacon probe"
```

---

## Task 9: Kick WS probe

**Files:** Create `internal/canary/kick.go`; Test `internal/canary/kick_test.go`. Reuse `internal/platform/kick/wswatch.go` (`fetchViewerToken`, `dialViewerWS`, `wsHandshake`, `wsUserEvent`).

- [ ] **Step 1: Read** `wswatch.go` (esp. `startWSWatch`, `dialViewerWS`, `fetchViewerToken`) and `wsverify_test.go` / `wswatch_test.go` to learn how the WS loop + frame send/recv is tested.

- [ ] **Step 2: Write the failing test** — probe reports OK when it observes ≥N `channel_handshake` exchanges over a short window against a fake WS server (mirror `wsverify_test.go`'s fake server if present; otherwise inject the frame source).

- [ ] **Step 3: Run, verify fail.**

- [ ] **Step 4: Implement `kick.go`** — resolve the channel to a livestream id (reuse backend lookup), open the WS via the existing dial path, send handshake/user_event, read frames for ~75s (short/0 in tests), assert connect + ≥1 periodic handshake cycle + pong; return `Result{OK, Detail}`. Reuse wswatch seams; do NOT re-implement the WS dial.

- [ ] **Step 5: Run, verify pass.**

- [ ] **Step 6: Commit**
```bash
git add internal/canary/kick.go internal/canary/kick_test.go
git commit -m "feat(canary): kick WS transport probe"
```

---

## Task 10: CI regression — Kick WS frame replay

**Files:** Create `internal/platform/kick/testdata/ws_frames.json`; Test `internal/platform/kick/wsreplay_test.go`

- [ ] **Step 1: Capture fixtures** — from one real WS session (or hand-author from the known frame shapes in `wswatch.go`: `channel_handshake`, `ping`/`pong`, `user_event`), save an ordered JSON array of received frames to `testdata/ws_frames.json`. Include several `channel_handshake` + a `pong`.

- [ ] **Step 2: Write the test** — load the fixtures, feed each frame to the WS message-handling function used by `startWSWatch` (extract the per-message handler into a testable func if it's currently an inline closure), assert it counts handshakes / recognises pong without error.
```go
func TestWSReplay_RecognisesAccrualFrames(t *testing.T) {
	frames := loadFrames(t, "testdata/ws_frames.json")
	h := newWSFrameHandler()
	for _, f := range frames { require.NoError(t, h.handle(f)) }
	assert.GreaterOrEqual(t, h.handshakeCount, 2)
}
```

- [ ] **Step 3: Run, verify fail** (handler not extracted yet).

- [ ] **Step 4: Extract** the inline message handler from `startWSWatch` into a small `wsFrameHandler` type with a `handle([]byte) error` method + counters; call it from the existing loop (behaviour unchanged). Make the replay test pass.

- [ ] **Step 5: Run, verify pass** + run existing `wswatch_test.go`/`wsverify_test.go` to confirm no regression.

- [ ] **Step 6: Commit**
```bash
git add internal/platform/kick/testdata/ws_frames.json internal/platform/kick/wsreplay_test.go internal/platform/kick/wswatch.go
git commit -m "test(kick): WS frame replay regression guard"
```

---

## Task 11: CI regression — Twitch beacon golden

**Files:** Test `internal/platform/twitch/watch_test.go` (extend)

- [ ] **Step 1: Inspect** existing `watch_test.go` (`TestWatch_HeartbeatSendsAuthHeader`, `TestWatch_HeartbeatSendsGzippedBase64Mutation`) — beacon-shape coverage may already exist.

- [ ] **Step 2: Add a golden test** asserting the full beacon request (URL + headers + decoded body) for a fixed channel/user matches a recorded-good golden string in `testdata/`. Only add what the existing tests don't already cover (DRY — don't duplicate the auth-header/gzip tests).

- [ ] **Step 3: Run** — `go test ./internal/platform/twitch/ 2>/dev/null | tail` → PASS.

- [ ] **Step 4: Commit**
```bash
git add internal/platform/twitch/watch_test.go internal/platform/twitch/testdata
git commit -m "test(twitch): beacon-shape golden guard"
```

---

## Task 12: Canary runner + wiring

**Files:** Create `internal/canary/canary.go`; Test `internal/canary/canary_test.go`; Modify `cmd/miner/main.go`

- [ ] **Step 1: Write the failing test** — a `Runner` with fake probes (one OK, one fail) runs once and persists both results:
```go
func TestRunner_RunsProbesAndStoresResults(t *testing.T) {
	// fake twitch probe -> OK, fake kick probe -> fail; empty kick channel -> skipped
	// run RunOnce, assert LoadResult reflects each
}
```

- [ ] **Step 2: Run, verify fail.**

- [ ] **Step 3: Implement `canary.go`** — a `Runner` holding the probes + `*gen.Queries` + settings; `RunOnce(ctx)` reads configured channels (skip platform if channel empty / no session), runs each probe, `SaveResult`, and returns. Add `Run(ctx, interval)` goroutine loop (mirror `authChecker.Run`). Provide a probe interface:
```go
type Probe interface { Run(ctx context.Context, channel string) Result }
```

- [ ] **Step 4: Run, verify pass.**

- [ ] **Step 5: Wire in `cmd/miner/main.go`** next to the authcheck wiring (~line 452): build the canary Runner, read interval from settings/`GRUB_CANARY_INTERVAL`, `go canaryRunner.Run(ctx, interval)` (skip if interval 0). Build the binary.
Run: `go build ./... 2>&1 | tail`
Expected: clean.

- [ ] **Step 6: Commit**
```bash
git add internal/canary/canary.go internal/canary/canary_test.go cmd/miner/main.go
git commit -m "feat(canary): scheduled runner wired into miner"
```

---

## Task 13: Health tab UI — results, Run-now, settings form

**Files:** Modify `internal/api/handlers_settings.go`, `internal/web/templates/settings.html`; route registration.

- [ ] **Step 1: Populate canary data** — in `renderTab` (or a health-specific path), load `canary.LoadResult` for twitch+kick and the three canary settings, add to `settingsPageData` (new fields: `CanaryTwitch`, `CanaryKick canaryView`, `CanaryTwitchChannel`, `CanaryKickChannel`, `CanaryIntervalSec`). Define a small view struct `{OK bool; Configured bool; Detail string; When string}`.

- [ ] **Step 2: Render** the canary panel in the Health block (replace the Task-4 placeholder): per platform show ✓/✗/“not configured” + relative time + detail; a settings form (POST `/settings/canary`) for the two channels + interval; a "Run now" button (`hx-post="/settings/canary/run"` → swaps `#canary-panel`).

- [ ] **Step 3: Add handlers** `canarySave` (POST `/settings/canary`, uses `saveErr` guard from existing code, then flash + redirect) and `canaryRun` (POST `/settings/canary/run`, calls `runner.RunOnce`, returns the `#canary-panel` partial). Register both routes.

- [ ] **Step 4: Add a render test** (api package): health tab with a fake OK twitch result + unconfigured kick shows "✓" and "not configured" and the Run-now control.

- [ ] **Step 5: Run tests + visual** — `go test ./internal/api/ 2>/dev/null | tail`; verify locally.

- [ ] **Step 6: Commit**
```bash
git add internal/api/handlers_settings.go internal/web/templates/settings.html
git commit -m "feat(health): canary results + run-now + settings UI"
```

---

## Task 14: Discord alert on pass→fail

**Files:** Modify `internal/canary/canary.go`; reuse the notifier; new notify kind.

- [ ] **Step 1: Write the failing test** — runner with a notifier spy: result transitions OK→fail fires one notify; fail→fail does not re-fire; fail→OK optionally fires a recovery.

- [ ] **Step 2: Implement** — in `RunOnce`, compare new result to the previously stored one before saving; on OK→fail transition call the notifier (gated by a new notify kind, e.g. `notify_canary`, added to `SetNotifyKinds`/settings + the Notifications tab toggle). Keep DRY with existing notify-kind plumbing.

- [ ] **Step 3: Run, verify pass.**

- [ ] **Step 4: Commit**
```bash
git add internal/canary/canary.go internal/canary/canary_test.go internal/store/settings.go internal/web/templates/settings.html
git commit -m "feat(canary): Discord alert on accrual pass→fail"
```

---

## Task 15: Changelog + release

- [ ] **Step 1: Update `docs/CHANGELOG.md`** — under a new `## [1.2.0] — <date>` (move from `[Unreleased]`): Added (accrual canary + CI regression tests, Health tab), Changed (settings tab reorder, Logging split, orange headers), Fixed (Console idle row-height alignment).

- [ ] **Step 2: Full verify** — `go vet ./... ; go test ./... 2>/dev/null | grep -iE 'FAIL|panic' || echo PASS`.

- [ ] **Step 3: Per project rule** — verify drop-mining still works before tagging (deploy to prod, confirm canary + a real watcher accrue). Only then tag `v1.2.0`, let ghcr build, deploy, and write the curated GitHub release notes.

- [ ] **Step 4: Commit + tag** (after verification).

---

## Appendix A: Run locally for visual checks

```bash
go build -o /tmp/gd ./cmd/miner
KEY=$(age-keygen 2>/dev/null | grep AGE-SECRET-KEY)
GRUB_MASTER_KEY="$KEY" GRUB_DB_PATH=/tmp/gd.db GRUB_SECURE_COOKIES=0 GRUB_HTTP_ADDR=127.0.0.1:8097 /tmp/gd
# create admin at http://127.0.0.1:8097 ; toggle theme via the ☀/☾ button
```
Note: `GRUB_MASTER_KEY` MUST be a valid age identity (`AGE-SECRET-KEY-…`), not arbitrary base64.

---

## Notes / decisions locked

- Canary proves transport acceptance, NOT drop credit (documented in UI copy).
- Probes are standalone (not real watchers) → never consume an account's exclusive watch slot.
- Kick canary channel default empty → skipped until configured; Twitch default `alveussanctuary`.
- Logging section stays on General (not Health).
- HeartbeatInterval stays 60s (do not expose); canary's beacon pacing is its own short window.
