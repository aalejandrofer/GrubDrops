# Update checker — design

**Date:** 2026-06-29
**Status:** approved (brainstorm), pending implementation plan
**Ships as:** v1.3.5 (UI/plumbing feature, no accrual/claim code → default release gate: green build + unit tests).

## Problem

A self-hosted operator has no in-app signal that a newer GrubDrops release exists; they'd have to check GitHub manually. Surface "an update is available" in the UI when the latest GitHub release is newer than the running version.

## Goals

- Background-check the latest GitHub release on a schedule + at boot; cache the result so page rendering never calls GitHub.
- Show an "update" indicator in the nav when the latest release is newer than the running version: a small pill under the GitHub icon **and** recolor the existing green "pulse" dot orange.
- Never show a false positive on dev/source builds or when already current.
- Be disableable (offline / privacy).

## Non-goals

- No auto-update / self-upgrade. Indicator + link to the releases page only.
- No per-account or notification-channel alerting (Discord etc.) — in-app nav only.

## Existing mechanics (reference, file:line)

- **Running version:** `var version string` ldflag-injected (`cmd/miner/main.go:43`); plumbed as `Version: cmp.Or(version, os.Getenv("GRUB_VERSION"))` (`main.go:603`) into `api.Deps.Version` (`internal/api/server.go:109`), shown on Settings Health (`settings.html:345`).
- **Outbound HTTP:** `netutil.NewTransport(proxyURL)` (`internal/netutil/transport.go:14`) + `&http.Client{Timeout:10*time.Second, Transport:t}` (pattern in `internal/notify/discord.go:36`). Proxy transport built in `main.go:152-162`.
- **Background task pattern:** canary `go canaryRunner.Run(ctx, interval)` + one-shot `RunOnce` (`main.go:564-567,593`).
- **kv settings:** `store.Settings` wrapper with `getString`/`setString` (`internal/store/settings.go:64-86`) over `gen.GetSettingString`/`UpsertSettingString` (`internal/store/gen/settings.sql.go`).
- **Template data:** `templateData` struct (`internal/api/render.go:16-26`); `render()` (`render.go:28`) executes templates; `_layout.html:33` passes `.` to `{{template "nav" .}}`.
- **Nav markup:** `internal/web/templates/_nav.html` — `.user` div (line 16-27) has the pulse dot (`<span class="pulse" ...>`, line 19) and the GitHub link (`<a class="gh-link" ...>`, lines 20-22). Repo: `aalejandrofer/GrubDrops`.
- **i18n:** `internal/i18n/locales/{en,es,zh-CN}.json`; `Supported = {"en","zh-CN","es"}`; parity test in `internal/i18n`.

## Design

### Component 1 — `internal/update` package

A self-contained checker with one responsibility: know the latest release version.

```go
type Checker struct { /* httpClient, repo slug, atomic latest, settings store */ }

func New(client *http.Client, repo string, s *store.Settings) *Checker
func (c *Checker) RunOnce(ctx context.Context) error   // fetch + cache
func (c *Checker) Run(ctx context.Context, every time.Duration)  // boot + ticker loop
func (c *Checker) Status(current string) (updateAvailable bool, latest string)
```

- **Fetch:** `GET https://api.github.com/repos/aalejandrofer/GrubDrops/releases/latest`, header `Accept: application/vnd.github+json`, decode `{"tag_name": "..."}`. On success, store `tag_name` in kv (`settings:latest_release`) + `settings:last_release_check` (unix), and in an in-memory `atomic.Value` (so `Status` is lock-free on the hot render path). On any error: log at debug/warn, keep the last-known cached value (load from kv at construction so a restart keeps the prior answer).
- **Client:** injected `*http.Client` built from the shared proxy transport (same as Discord/Twitch), 10s timeout.
- **Run:** check once immediately at start, then every `every` (default 6h). Honors ctx cancel.

### Component 2 — version comparison (`internal/update`, pure)

```go
func compareVersions(a, b string) int   // -1 a<b, 0 equal, +1 a>b; ok=false handled by caller
func parseSemver(s string) (maj, min, patch int, ok bool)
```

- `parseSemver` strips a leading `v` and truncates at the first character that isn't a digit or `.` (so `v1.3.4-itempull` → `1.3.4`, `1.3.4+build` → `1.3.4`). Splits on `.`, needs at least MAJOR.MINOR (missing patch = 0).
- `Status(current)`:
  - parse `current` and `latest`; **if either fails to parse → `(false, latest)`** (dev/source/empty build never shows an update).
  - `updateAvailable = compareVersions(current, latest) < 0`.

### Component 3 — wiring (`cmd/miner/main.go`)

- Build the update HTTP client from the existing proxy transport.
- `updateChecker := update.New(client, "aalejandrofer/GrubDrops", settingsStore)`.
- Gate on `GRUB_UPDATE_CHECK` env (default **on**; set to `0`/`false` to disable). When disabled, do not start `Run` and `Status` always returns `(false, "")`.
- `go updateChecker.Run(ctx, interval)` (interval from `GRUB_UPDATE_CHECK_INTERVAL`, default 6h).
- Pass the checker (or a `func(current string)(bool,string)` status closure) + the version into `api.Deps`.

### Component 4 — nav surfacing

- **templateData** (`render.go`): add `UpdateAvailable bool` and `LatestVersion string`.
- **Injection (one place):** a middleware in the api server computes `available, latest := checker.Status(version)` and stores them in the request context; `render()` reads them from context and sets the two fields on every `templateData` it builds. (Single source — no per-handler edits.) When the checker is disabled/nil, both default to false/"".
- **`_nav.html`:**
  - Pulse dot (line 19): add `{{if .UpdateAvailable}} update{{end}}` to its class so CSS recolors the green dot orange; swap its `title` to the update message when available.
  - After the GitHub `</a>` (line 22): when `.UpdateAvailable`, render a small pill linking to `https://github.com/aalejandrofer/GrubDrops/releases/latest` (target _blank, rel noopener), text from i18n, `title` = "<latest> available". Hidden otherwise.
- **CSS** (`app.css`): `.pulse.update { background: var(--amber); box-shadow: 0 0 10px var(--amber); }` (amber already used elsewhere) + a `.update-pill` style (small, amber, under/next to the icon).

### Component 5 — i18n

New keys in en/es/zh-CN (parity test must stay green):
- `nav.update_available` — pill label, e.g. en "update".
- `nav.update_title` — tooltip; the rendered title appends the version in the template (`{{t "nav.update_title"}} {{.LatestVersion}}`), so the key is the static part (e.g. en "new version available").

## Error handling

- GitHub unreachable / rate-limited / non-200 / bad JSON → `RunOnce` returns an error that is logged (warn) and swallowed; the cached/last-known value stands; the badge simply reflects the last good check (or nothing on a cold first run). The render path never blocks on or fails from the checker.
- Disabled via env → no outbound calls at all, badge never shows.

## Testing

- **Version compare (pure, table test):** newer latest → available; equal → not; older latest → not; `v`-prefix + `-suffix`/`+build` parse to base; empty/garbage `current` → not available (no false positive); missing patch defaults to 0.
- **Checker fetch:** httptest server returns a sample `releases/latest` JSON → `RunOnce` caches the tag; `Status` with an older current returns `(true, tag)`. A non-200/garbage response → `RunOnce` errors, prior cached value retained.
- **Nav render:** `templateData{UpdateAvailable:true, LatestVersion:"v1.3.5"}` → output contains the pill + the releases link + `pulse update` class; with `UpdateAvailable:false` → no pill and plain `pulse` class.
- i18n parity green with the new keys.

## Rollout

Default gate (no accrual/claim code): green build + unit tests. Log under `docs/CHANGELOG.md` `[Unreleased]`; this is the headline feature of **v1.3.5**.
