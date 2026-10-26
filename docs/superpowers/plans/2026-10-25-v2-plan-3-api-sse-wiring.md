# v2 Plan 3: JSON API + SSE + Miner Wiring Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The miner runs v2. `cmd/miner` drives `runner.Pool` and `collect.Collector` instead of the watcher and scheduler. `internal/api` serves JSON under `/api/*`, live updates over SSE at `/api/events`, and an embedded SPA placeholder. The HTML UI, watcher, scheduler and discovery scraper are deleted.

**Architecture:** First the old HTML package moves aside to `internal/legacyapi`, so `cmd/miner` keeps compiling while the new `internal/api` is built resource by resource. Handlers depend on narrow interfaces in `api.Deps`, so every task tests against fakes and a temp SQLite DB. A roster builder in `cmd/miner` turns each account row into a `runner.Builder` wired to the Collector and the event hub. The last task switches `main.go` over and deletes the old packages in one commit.

**Tech Stack:** Go (module `github.com/aalejandrofer/grubdrops`), chi router, `alexedwards/scs/v2` sessions, `justinas/nosurf` CSRF, SQLite via `modernc.org/sqlite`, sqlc v1.31.1, `log/slog`, `net/http` SSE.

**Spec:** `docs/superpowers/specs/2026-10-25-grubdrops-v2-rebuild-design.md`

This is plan 3 of 5. Plans 1 (planner + runner) and 2 (discovery + collection) are done. Plan 4 builds the SvelteKit app in `web/` into `internal/web/dist`. Plan 5 migrates 1.x data (chosen games, claim log, minutes) and tests the upgrade on a real DB copy. Until plan 5 runs, a 1.x DB boots with no chosen games, so every runner sits in `no_games`. That is expected.

## Global Constraints

- Branch `rebuild/v2`, local only: no push, no PR. Run `gofmt -w .`, `go build ./...` and `go test ./...` green before every commit. Go is at `/home/jandro/.local/go/bin`; call `go` and `gofmt` by full path.
- Required config stays `GRUB_MASTER_KEY` only. Every other `GRUB_*` env var keeps its current meaning or is dropped with its feature.
- Never touch accrual or claim code in `internal/platform/twitch` or `internal/platform/kick`. Twitch beacon stays one `Heartbeat` per 60 s (`runner.DefaultTick`); never add a `Client-Integrity` header.
- Keep the sidecar stack: `cmd/browser-sidecar`, `internal/auth/browser`, `internal/dockerctl`, `proto/` + buf config, and the Twitch `BrowserBackend`.
- Base64-encode outside data embedded in a chromedp `Evaluate` script. CodeQL stays clean.
- sqlc: never put `?` or parentheses in a `queries/*.sql` comment. Regenerate with `cd internal/store && PATH=/home/jandro/.local/go/bin:$PATH /home/jandro/.local/go/bin/go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1 generate` and commit `gen/`.
- All account reloads use the root context, never `r.Context()` (v1.0.1 incident).
- Log lines keep `"kind"` (`state`, `discovery`, `progress`, `claim`, `heartbeat`, `error`, `auth`) plus `"account"`.
- Add one line per task to `docs/CHANGELOG.md` under `## [Unreleased]` in the style of the existing v2 lines. Tasks that run in parallel worktrees skip the changelog; the controller adds their lines.

## API contract

Every task's handlers follow this section. JSON is snake_case. Times are RFC 3339 in UTC; an unset time is `null`. The client formats times in the configured timezone.

**Errors:** status code plus `{"error":"<code>"}`. Codes: `bad_request` 400, `unauthorized` 401, `csrf` 403, `not_found` 404, `conflict` 409, `internal` 500, `upstream` 502. Request bodies are capped at 1 MiB; a bigger or malformed body is `bad_request`.

**Auth:** every `/api/*` route needs the admin session except `GET /api/session`, `POST /api/setup`, and `POST /api/login`. A missing session is 401 JSON, never a redirect. Unsafe methods need header `X-CSRF-Token` with the token from `GET /api/session`; failure is 403 `csrf`. `GRUB_AUTHBYPASS` and `GRUB_AUTH_BYPASS_LOCAL` keep their 1.x behaviour. OIDC keeps `GET /auth/oidc/login` and `GET /auth/oidc/callback`; the callback redirects to `/` on success and `/?login_error=oidc` on failure.

**Target** (used in several resources):
`{"game","platform","campaign_id","campaign_name","benefit_id","benefit_name","image_url","required_minutes","minutes","ends_at"}`

| Method + path | Request | Response |
|---|---|---|
| `GET /api/session` | | `{"authed","setup_needed","oidc":{"enabled","name"},"csrf_token","version"}` |
| `POST /api/setup` | `{"password","confirm"}` | 204; 409 if an admin exists; 400 if they differ or the password is under 8 characters |
| `POST /api/login` | `{"password"}` | 204; 401 on a wrong password |
| `POST /api/logout` | | 204 |
| `GET /api/overview` | | `{"status":{"state","accounts_total","accounts_mining","last_scan_at"},"problems":[{"kind":"needs_login","account_id","account_name"}],"kpis":{"minutes_total","minutes_today","claimed_today","claimed_all_time","accounts_healthy"},"version":{"current","latest","update_available"}}` |
| `GET /api/runners` | | `[Runner]`, Runner = `{"account_id","account_name","platform","avatar_url","state","current":Target\|null,"channel","viewer_count","minutes","started_at","last_scan_at","up_next":[Target],"blocked":[{"target":Target,"reason"}],"order_custom"}` |
| `PUT /api/runners/{id}/order` | `{"benefit_ids":[...]}` | 204; 404 for an unknown account |
| `DELETE /api/runners/{id}/order` | | 204 |
| `GET /api/games` | | `{"games":[{"id","name","slug","priority","box_art_url","state","campaigns":[{"id","platform","name","drops","ends_at"}]}],"last_scan_at"}` |
| `POST /api/games` | `{"name"}` | 201 Game; 409 `{"error":"conflict","priority":n}` when already chosen |
| `PUT /api/games` | `{"ids":[...]}` | 204, the new full order; 400 on an unknown id |
| `GET /api/games/search?q=` | | `[{"id","name","chosen","priority"}]`, at most 10 |
| `GET /api/claims` | `?game=&account=&limit=&offset=` | `{"total","items":[{"account_id","account_name","platform","game","campaign_name","benefit_id","benefit_name","image_url","code","source","claimed_at"}],"games":[...],"accounts":[{"id","name"}]}` |
| `GET /api/accounts` | | `[{"id","platform","name","avatar_url","state","needs_login","auth":{"ok","detail","checked_at"}}]` |
| `POST /api/accounts` | `{"platform","name"}` | 201 `{"id"}` |
| `PATCH /api/accounts/{id}` | `{"name"}` | 204 |
| `DELETE /api/accounts/{id}` | | 204 |
| `POST /api/accounts/{id}/twitch/device` | | `{"verification_url","user_code","expires_at"}` |
| `GET /api/accounts/{id}/twitch/device` | | `{"status":"pending\|done\|expired\|error"}`; 404 with no flow started |
| `POST /api/accounts/{id}/kick/session` | `{"cookies_txt","channels":[...]}` | `{"verified"}` |
| `GET /api/settings` | | `{"language","time_format","theme","timezone","discord":{"configured","webhook_hint"},"notify":{"claim","auth","errors","daily_summary"},"update_auto"}` |
| `PUT /api/settings` | any subset of the GET fields, plus `"discord_webhook"` | 200 with the GET body |
| `POST /api/settings/notify-test` | | 204; 400 when no webhook; 502 when the send fails |
| `GET /api/health` | `?filter=all\|problems\|claims` | `{"miner":{"started_at","version","commit"},"kick_sidecars":{"running"},"last_scan_at","canary":[{"platform","ok","detail","checked_at"}],"events":[Event],"counts":{"all","problems","claims"}}`, Event = `{"at","level","kind","account_id","account_name","msg"}` |
| `GET /api/health/log` | | `text/plain` attachment `grubdrops-log.txt`, every ring line |
| `POST /api/health/canary` | | 202 |
| `GET /api/update` | | `{"current","latest","update_available","checked_at","notes":[...],"notes_url","command","auto"}` |
| `POST /api/update/check` | | 200 with the GET body |
| `GET /api/img/kick?u=` | | image bytes (the 1.x `/img/kick` proxy, same SSRF guard) |
| `GET /api/events` | | SSE stream, below |
| `GET /*` | | embedded SPA; unknown paths outside `/api/` and `/auth/` serve `index.html` |

**SSE** (`GET /api/events`, `Content-Type: text/event-stream`):

| Event | Data | When |
|---|---|---|
| `runner` | Runner | an account's snapshot changed; coalesced to at most one per account per second |
| `claim` | claim item | a claim was logged (runner or reconcile) |
| `games` | `{}` | a scan finished or the chosen games changed |
| `accounts` | `{}` | an account was added, removed, or logged in |
| `log` | Event | a ring line was pushed |

A comment line `: ping` goes out every 25 s. A subscriber whose 64-event buffer is full is dropped and its stream closed; the client reconnects and refetches. Publishing never blocks.

## Calls made in this plan (check these)

1. **Old package moves before it dies.** Task 1 renames `internal/api` to `internal/legacyapi`, so each new resource lands in a building tree. Task 12 deletes `legacyapi`, `watcher`, `scheduler`, `discovery` and the HTML templates together.
2. **Between plan 3 and plan 4 there is no UI.** `/` serves a placeholder `internal/web/dist/index.html` that says the v2 UI is not built yet. Testing is curl and unit tests. Staging stays on its current build until plan 4.
3. **Game edits reload each account.** Adding, removing or reordering games calls `Pool.ReloadAccount` for every account, one at a time. This restarts watches, costing at most one beacon minute per account, but chosen games feed `Session.Games` at `runner.New`, and the Twitch TV path reads that list. Making it live would change kept platform behaviour. Game edits are rare.
4. **Game search is local.** `GET /api/games/search` filters the `games` table (names seen in 1.x and in scans). `POST /api/games` adds any free-text name, as 1.x did. Neither backend has a category search, and adding one touches the kept Twitch GQL code.
5. **Box art is a URL pattern:** `https://static-cdn.jtvnw.net/ttv-boxart/<PathEscape(name)>-144x192.jpg`. The SPA shows its patterned fallback when the image fails. If wrong, the cost is the fallback everywhere.
6. **Four notification toggles map onto existing keys:** claim → `notify_claim`, auth → `notify_auth`, errors → `notify_error`, plus a new `notify_daily_summary` (default off). Progress notifications are dropped: roster builds pass `ProgressNotifyStepPct: 0`. The canary kind keeps its stored value with no UI.
7. **Daily summary** fires at 00:00 in the configured timezone for the day that just ended: minutes and claims. A miner that is down at midnight skips that day.
8. **UI settings are server-side:** `ui_language` (`en`, `es`, `zh-CN`; default `en`), `ui_time_format` (`24h`, `12h`; default `24h`), `ui_theme` (`dark`, `light`; default `dark`), `update_auto` (default true). Language must live on the server because Go sends notifications.
9. **Release notes:** the update checker also stores the release body and URL. `notes` are the body's first 5 Markdown bullet lines.
10. **Removing an account keeps history.** Claim log and minutes rows stay; `drop_states` and `account_scans` cascade. A claim from a removed account shows an empty `account_name`.
11. **Panicked units restart.** `Pool` rebuilds a unit from its Builder one minute after a panic. A unit that exits with `errStop` (`needs_login`, `no_games`) stays stopped until a reload.
12. **Dropped settings with no v2 meaning:** tick interval, discovery interval, priority mode, per-account games, channel whitelists, force-watch, mark-collected overrides, proxy settings UI, Kick watch mode UI. Their stored values stay in the DB and boot keeps reading `proxy_url`, `proxy_enabled` and `kick_watch_mode`. Link overrides keep feeding `runner.Config.ForceLinked`.
13. **`start()` stays recursive.** The plan-2 carry-over was to loop it once Skip/Stop exist. The r4 mockups have no Skip or Stop, so neither exists.

## Review Focus

1. **The session expires while the SPA is open:** expect 401 JSON from every `/api/*` route, never an HTML redirect. Test: `TestRequireAdmin_JSON401` (Task 2).
2. **A browser tab sleeps with the SSE stream open:** expect runners never to block, and the slow subscriber dropped. Test: `TestHub_SlowSubscriberDropped` (Task 3).
3. **An account is removed while its runner is watching:** expect the runner stopped, its board entry forgotten, it gone from `/api/runners`, and its claims still in `/api/claims`. Tests: `TestAccounts_DeleteStopsAndKeepsHistory` (Task 7), `TestRoster_AccountRemoved` (Task 11).
4. **An Up next order names benefits that no longer exist:** expect them ignored, no 500. Test: `TestRunners_OrderUnknownIDsIgnored` (Task 4).
5. **The same game is added twice, with different case or spacing:** expect 409 with its priority, not a second row. Test: `TestGames_AddDuplicateConflict` (Task 5).

---

## File Structure

| File | Responsibility |
|---|---|
| `internal/legacyapi/` | 1.x HTML handlers, moved unchanged (Task 1), deleted in Task 12 |
| `internal/api/server.go` | `Deps`, `NewRouter`, route table, SPA fallback |
| `internal/api/json.go` | `writeJSON`, `readJSON`, `writeErr` |
| `internal/api/auth.go` | session, setup, login, logout, `RequireAdmin`, CSRF, OIDC routes |
| `internal/api/session_store.go` | `NewKVSessionStore` (copied from legacy `session.go`) |
| `internal/api/events.go` | `Hub`, `Event`, `GET /api/events` |
| `internal/api/overview.go`, `runners.go` | home data and Up next order |
| `internal/api/games.go` | chosen games, search, box art |
| `internal/api/claims.go` | claim history |
| `internal/api/accounts.go`, `login_twitch.go`, `login_kick.go`, `cookies_netscape.go`, `avatar.go` | accounts and logins |
| `internal/api/settings.go` | settings and notify test |
| `internal/api/health.go`, `update.go`, `image.go` | Health, Updates, Kick image proxy |
| `internal/store/settings.go` | new UI, summary and update keys |
| `internal/store/queries/v2.sql`, `claim_log.go` | filtered claim page |
| `internal/update/checker.go` | release notes and URL, auto toggle |
| `internal/summary/summary.go` | daily summary sender |
| `internal/runner/pool.go` | panic restart |
| `internal/web/embed.go`, `internal/web/dist/index.html` | SPA embed and placeholder |
| `cmd/miner/roster.go` | account row → `runner.Builder`, hooks into Collector and Hub |
| `cmd/miner/main.go` | v2 wiring |

## Parallelism

Task 1 runs first. Tasks 2 and 3 run next: Task 3 depends only on `json.go`, which Task 2 creates, so start Task 3 after Task 2 commits. Tasks 4 to 9 touch disjoint files except one route line each in `server.go`; run them in parallel worktrees based on the Task 3 commit and resolve the `server.go` route lines at cherry-pick. Task 10 (runner) can run at any point after Task 1. Task 11 needs Tasks 3 and 10. Task 12 runs last.

---

### Task 1: Move the HTML package aside

**Files:**
- Move: `internal/api/` → `internal/legacyapi/` (`git mv`; package clause `package legacyapi`)
- Modify: `cmd/miner/main.go` imports, every `internal/**/*_test.go` and `internal/web` test importing `internal/api`

**Interfaces:**
- Produces: package `legacyapi` with the same exported names as today's `api`. `internal/api` no longer exists after this task.

- [ ] **Step 1:** `git mv internal/api internal/legacyapi`, rewrite the package clause in every file, and rewrite imports repo-wide (`grep -rl '"github.com/aalejandrofer/grubdrops/internal/api"'`), aliasing nothing: call sites change `api.` to `legacyapi.`.
- [ ] **Step 2:** Run `go build ./... && go test ./...`. Expected: PASS with no other change.
- [ ] **Step 3:** Commit `refactor: move HTML api package to internal/legacyapi`. No changelog line (internal move).

---

### Task 2: API core, auth and SPA fallback

**Files:**
- Create: `internal/api/server.go`, `json.go`, `auth.go`, `session_store.go`, `internal/web/dist/index.html`
- Modify: `internal/web/embed.go` (add `//go:embed all:dist` and `func Dist() fs.FS`)
- Test: `internal/api/auth_test.go`, `server_test.go`

**Interfaces:**
- Produces:
  - `type Deps struct` with, for this task: `DB *sql.DB`, `Q *gen.Queries`, `Session *scs.SessionManager`, `OIDC *oidc.Provider`, `SecureCookies bool`, `Version string`, `SPA fs.FS`. Later tasks add their own fields; every field is nil-safe or documented as required.
  - `func NewRouter(d Deps) http.Handler`. Routes: `/healthz`, `/auth/oidc/*`, `/api/*` (a chi sub-router `s.api` that later tasks extend through `func (s *server) routesX(r chi.Router)` methods called from `NewRouter`), and the SPA fallback.
  - `func writeJSON(w http.ResponseWriter, status int, v any)`, `func readJSON(w http.ResponseWriter, r *http.Request, v any) bool` (writes 400 and returns false on failure; uses `http.MaxBytesReader` 1 MiB and `DisallowUnknownFields`), `func writeErr(w http.ResponseWriter, status int, code string)`.
  - `func RequireAdmin(sm *scs.SessionManager) func(http.Handler) http.Handler` (401 JSON).
  - `func NewKVSessionStore(db *sql.DB) scs.Store` (same kv format as 1.x, so 1.x sessions stay valid).
- Consumes: `q.AdminExists`, `q.GetAdmin`, `q.UpsertAdmin`, `auth.HashPassword`, `auth.VerifyPassword`, `oidc.NewHandshakeStore` and the provider methods, as used by `internal/legacyapi/handlers_setup.go`, `handlers_auth.go` and `handlers_oidc.go`. Port their logic; do not import `legacyapi`.

- [ ] **Step 1: Write the failing tests**
  - `TestSession_SetupNeeded`: fresh DB, `GET /api/session` gives `setup_needed:true`, `authed:false`, a non-empty `csrf_token`.
  - `TestSetup_CreatesAdminAndLogsIn`: `POST /api/setup` with matching 8+ character passwords and the CSRF header gives 204, and a follow-up `GET /api/session` with the returned cookie gives `authed:true`. A second setup gives 409.
  - `TestSetup_Rejects`: mismatched passwords give 400; `"short"` gives 400.
  - `TestLogin_WrongPassword401` and `TestLogin_OK`.
  - `TestRequireAdmin_JSON401`: `GET /api/overview` (any registered protected route, stubbed in the test router) with no cookie gives 401 and body `{"error":"unauthorized"}`, `Content-Type: application/json`.
  - `TestCSRF_MissingHeader403`: an authed `POST /api/logout` without `X-CSRF-Token` gives 403 `{"error":"csrf"}`.
  - `TestAuthBypassLocal`: with `GRUB_AUTH_BYPASS_LOCAL=1` and RemoteAddr `127.0.0.1:1234`, a protected route passes with no cookie.
  - `TestSPA_Fallback`: `GET /games` serves `index.html` from a `fstest.MapFS`; `GET /api/nope` gives 404 JSON, not the SPA.
  - `TestReadJSON_TooLarge`: a 2 MiB body gives 400 `bad_request`.
- [ ] **Step 2:** Run `go test ./internal/api -v`. Expected: FAIL (undefined).
- [ ] **Step 3: Implement.** CSRF: `nosurf` with `SetIsTLSFunc` as in legacy `middleware.go`, failure handler writing 403 `csrf`, and `nosurf.Token(r)` returned by `/api/session`. Session keys stay `admin_authed` and `auth_identity`. The placeholder `dist/index.html` is a minimal page reading "GrubDrops v2: the web UI is not built yet. The API is at /api." with no external assets.
- [ ] **Step 4:** Run `go test ./internal/api && go test ./... && gofmt -l .`. Expected: PASS; no gofmt output.
- [ ] **Step 5:** Changelog: `- v2 JSON API core: session, setup, login and CSRF under /api, 401 JSON for expired sessions, and the embedded SPA fallback (rebuild/v2).` Commit `feat(api): JSON core with auth, CSRF and SPA fallback`.

---

### Task 3: Event hub and SSE

**Files:**
- Create: `internal/api/events.go`
- Modify: `internal/api/server.go` (`Deps.Hub *Hub`, route)
- Test: `internal/api/events_test.go`

**Interfaces:**
- Produces:
  - `type Event struct { Name string; Key string; Data any }`. `Key` scopes coalescing: `runner` events use the account ID; empty means no coalescing.
  - `func NewHub() *Hub`, `func (h *Hub) Publish(e Event)` (never blocks), `func (h *Hub) Subscribe() (<-chan Event, func())`, `func (h *Hub) Run(ctx context.Context)` (flushes coalesced events every second).
  - Constants `EventRunner = "runner"`, `EventClaim = "claim"`, `EventGames = "games"`, `EventAccounts = "accounts"`, `EventLog = "log"`.
  - Handler `GET /api/events` writing `event: <name>\ndata: <json>\n\n` and `: ping\n\n` every `pingEvery = 25 * time.Second`.

- [ ] **Step 1: Write the failing tests**
  - `TestHub_FanOut`: two subscribers both receive a published `games` event.
  - `TestHub_RunnerCoalesced`: five `runner` events with key `a1` inside one flush window reach the subscriber as one event carrying the last `Data`; a `runner` event for `a2` in the same window arrives separately.
  - `TestHub_SlowSubscriberDropped`: a subscriber that never reads gets its channel closed once 65 events are published, and `Publish` returns within 10 ms each time.
  - `TestEvents_StreamFormat`: an `httptest` server with `pingEvery` overridden to 20 ms. The client reads `event: claim` and `data: {...}` lines, then a `: ping` line. Closing the client unsubscribes (hub subscriber count returns to 0).
  - `TestEvents_Unauthed401`.
- [ ] **Step 2:** Run `go test ./internal/api -run 'TestHub|TestEvents' -v`. Expected: FAIL.
- [ ] **Step 3: Implement.** Per-subscriber buffered channel of 64. Coalescing keeps a `map[key]Event` flushed by `Run`; uncoalesced events go straight to subscribers. The handler sets `Cache-Control: no-cache` and `X-Accel-Buffering: no` and flushes after each write.
- [ ] **Step 4:** Run `go test -race ./internal/api && go test ./... && gofmt -l .`. Expected: PASS.
- [ ] **Step 5:** Changelog: `- v2 live updates over Server-Sent Events at /api/events (runner, claim, games, accounts, log).` Commit `feat(api): event hub and SSE stream`.

---

### Task 4: Overview and runners

**Files:**
- Create: `internal/api/overview.go`, `internal/api/runners.go`
- Modify: `internal/api/server.go` (Deps fields, route call)
- Test: `internal/api/overview_test.go`, `runners_test.go`

**Interfaces:**
- Consumes: `runner.Snapshot`, `plan.Target`, `store.WatchMinutes.Total` and `Day`, `store.ClaimLog.CountSince`, `store.AccountScans.List`, `gen.Queries.ListAllAccounts`.
- Produces:
  - `type RunnerPool interface { Snapshots() []runner.Snapshot; SetOrder(accountID string, benefitIDs []string); ResetOrder(accountID string); OrderFunc(accountID string) func() []string }` satisfied by `*runner.Pool`.
  - Deps fields: `Pool RunnerPool`, `Minutes *store.WatchMinutes`, `Claims *store.ClaimLog`, `Scans *store.AccountScans`, `Zone func() *time.Location`, `UpdateStatus func(current string) (bool, string)`.
  - `func targetJSON(t plan.Target) targetDTO` and `func runnerJSON(s runner.Snapshot, a gen.Account, custom bool) runnerDTO`, reused by Task 11 for SSE payloads. Export them as `TargetJSON` and `RunnerJSON`.
  - Status rules: no accounts → `no_accounts`; no chosen games → `no_games`; any snapshot `watching` or `claiming` → `mining`; no snapshot has a `Current` or `UpNext`, and at least one account is healthy → `nothing_live`; otherwise `idle`. Healthy = state is not `needs_login`. `accounts_mining` counts `watching` + `claiming`. `last_scan_at` is the latest `LastScanAt`. Today = midnight in `Zone()` until now; `minutes_today` reads `Day` with the `2006-01-02` date in `Zone()`.

- [ ] **Step 1: Write the failing tests** (fake `RunnerPool`, temp DB with two accounts)
  - `TestOverview_Mining`: one snapshot `watching`, one `needs_login`. `state:"mining"`, `accounts_mining:1`, `accounts_healthy:1`, one problem `needs_login` with the account's name.
  - `TestOverview_NothingLive` and `TestOverview_NoAccounts`.
  - `TestOverview_KPIsUseZone`: minutes on `2026-10-26` and `Now` 2026-10-25T23:30Z with zone `Asia/Shanghai` give `minutes_today` from `2026-10-26`.
  - `TestRunners_ShapeAndOrderCustom`: a snapshot with `Current` and two `UpNext` targets renders the Target fields exactly; `order_custom` is true once `OrderFunc` returns a non-empty order.
  - `TestRunners_OrderUnknownIDsIgnored`: `PUT /api/runners/a1/order` with `["ghost","b2"]` gives 204 and `SetOrder("a1", ["ghost","b2"])`; the next `GET` shows `UpNext` in `plan.ApplyOrder` order with no error. Unknown account gives 404.
  - `TestRunners_ResetOrder`: `DELETE` calls `ResetOrder`.
- [ ] **Step 2:** Run `go test ./internal/api -run 'TestOverview|TestRunners' -v`. Expected: FAIL.
- [ ] **Step 3:** Implement.
- [ ] **Step 4:** Run `go test ./internal/api && go test ./... && gofmt -l .`. Expected: PASS.
- [ ] **Step 5:** Commit `feat(api): overview KPIs and runners with session order`.

---

### Task 5: Games

**Files:**
- Create: `internal/api/games.go`
- Modify: `internal/api/server.go`
- Test: `internal/api/games_test.go`

**Interfaces:**
- Consumes: `store.ChosenGames.List` and `Set`, `q.UpsertGame`, `q.ListAllGames`, `gameslug.ID` and `gameslug.Slug`, `collect.Board.Games(games []plan.Game) []collect.GameStatus`, `RunnerPool.Snapshots`, `plan.Match`.
- Produces: Deps fields `Chosen *store.ChosenGames`, `Board *collect.Board`, `GamesChanged func()` (nil-safe; Task 11 passes a function that reloads every account and publishes `games`). `func boxArtURL(name string) string`.
  - Game `state`: `mining` when any snapshot's `Current.Campaign.Game` matches the game with `plan.Match`; `waiting` when the board lists campaigns; else `no_campaign`. `priority` is 1-based. `drops` = benefit count of the campaign summary.

- [ ] **Step 1: Write the failing tests**
  - `TestGames_ListStatesAndOrder`: chosen `Rust`, `Overwatch`; board has a Rust campaign; a snapshot watches Rust. Rust is `mining`, priority 1, one campaign; Overwatch is `no_campaign`. `box_art_url` for `Dead by Daylight` is `https://static-cdn.jtvnw.net/ttv-boxart/Dead%20by%20Daylight-144x192.jpg`.
  - `TestGames_AddAppendsAndNotifies`: `POST {"name":"Rust"}` gives 201, appended last, `GamesChanged` called once.
  - `TestGames_AddDuplicateConflict`: with `Rust` chosen at priority 1, `POST {"name":" rust "}` gives 409 `{"error":"conflict","priority":1}` and no new row.
  - `TestGames_AddEmpty400`.
  - `TestGames_PutReorderAndRemove`: `PUT {"ids":[b,a]}` from `[a,b,c]` gives 204, `List` is `[b,a]`, `GamesChanged` called; an unknown id gives 400 and nothing changes.
  - `TestGames_Search`: games table has `Rust`, `Rusty Lake`, `Overwatch`; `q=rus` returns both Rust names, case-insensitive, Rust marked `chosen:true` with its priority; empty `q` gives `[]`; never more than 10 results.
- [ ] **Step 2:** Run `go test ./internal/api -run TestGames -v`. Expected: FAIL.
- [ ] **Step 3:** Implement. Trim names; the game ID comes from `gameslug.ID(name)`, so case and spacing variants hit the same row.
- [ ] **Step 4:** Run `go test ./internal/api && go test ./... && gofmt -l .`. Expected: PASS.
- [ ] **Step 5:** Commit `feat(api): chosen games, local search and box art`.

---

### Task 6: Claims history

**Files:**
- Modify: `internal/store/queries/v2.sql`, `internal/store/claim_log.go`, `internal/store/gen/` (regenerated)
- Create: `internal/api/claims.go`
- Modify: `internal/api/server.go`
- Test: `internal/store/v2_test.go`, `internal/api/claims_test.go`

**Interfaces:**
- Produces:
  - Queries `ListClaimLogPage` and `CountClaimLogFiltered` with named params `game` and `account_id`, where an empty value means any, ordered `claimed_at DESC, benefit_id`, plus `limit`/`offset`; and `ListClaimLogGames` (distinct non-empty games, sorted).
  - `type ClaimFilter struct { Game, AccountID string; Limit, Offset int }`, `func (l *ClaimLog) Page(ctx context.Context, f ClaimFilter) ([]ClaimEntry, int, error)`, `func (l *ClaimLog) Games(ctx context.Context) ([]string, error)`.
  - Handler: `limit` defaults to 10 and caps at 100; `offset` defaults to 0; a non-numeric value gives 400. `accounts` lists every account row (id, name).
- [ ] **Step 1: Write the failing tests**
  - `TestClaimLog_PageFilters` (store): five rows over two games and two accounts; filters by game, by account, both, and none give the right rows and totals; limit 2 offset 2 gives rows 3 and 4 newest-first.
  - `TestClaims_HandlerShape`: item fields exactly as the contract, `account_name` from the accounts table, `total` independent of `limit`.
  - `TestClaims_RemovedAccountKept`: a claim whose account row is gone renders with `account_name:""`.
  - `TestClaims_BadLimit400`.
- [ ] **Step 2:** Run the two packages' tests. Expected: FAIL.
- [ ] **Step 3:** Implement and regenerate sqlc.
- [ ] **Step 4:** Run `go test ./internal/store ./internal/api && go test ./... && gofmt -l .`. Expected: PASS.
- [ ] **Step 5:** Commit `feat(api): filtered claim history`.

---

### Task 7: Accounts and logins

**Files:**
- Create: `internal/api/accounts.go`, `login_twitch.go`, `login_kick.go`, `cookies_netscape.go` (+ its test, copied from legacy), `avatar.go`
- Modify: `internal/api/server.go`
- Test: `internal/api/accounts_test.go`, `login_twitch_test.go`, `login_kick_test.go`

**Interfaces:**
- Consumes: `q.CreateAccount`, `q.ListAllAccounts`, `q.GetAccount`, `q.UpdateAccountDisplayName`, the legacy purge queries (`legacyapi/handlers_accounts.go` `purgeAccount`), `store.SessionStore.Put`, `platform.Registry.Get`, the backend's `StartDeviceLogin` / `PollDeviceLogin`, `authcheck.Load`, the Kick verify and `RegisterChannels` path in `legacyapi/handlers_login_kick.go`.
- Produces: Deps fields `Sessions *store.SessionStore`, `Registry *platform.Registry`, `RootCtx context.Context`, `Registrar KickChannelRegistrar`, `AccountChanged func(id string)`, `AccountRemoved func(id string)` (both nil-safe). `KickChannelRegistrar` keeps its legacy signature.
  - `state` = the account's snapshot state from `Pool`, else `idle`. `needs_login` = state `needs_login` or no stored session.
  - A missing `name` on create becomes `Twitch account` or `Kick account`. Platform must be `twitch` or `kick`, else 400.
  - Twitch device polling runs on `RootCtx` exactly like legacy (interval, expiry, `authorization_pending`), keeps one flow per account (a new start cancels the old), and on success stores the session, fetches the avatar, then calls `AccountChanged`.
  - Kick: parse `cookies_txt` with the copied Netscape parser, persist like legacy `persistKickSession`, verify, fetch the avatar if verified, `RegisterChannels`, then `AccountChanged`.
  - Delete runs the legacy purge transaction, then `AccountRemoved`. It does not touch `claim_log` or `watch_minutes`.
- [ ] **Step 1: Write the failing tests** (a `platformtest` fake backend with device login)
  - `TestAccounts_CreateDefaultsName` and `TestAccounts_CreateBadPlatform400`.
  - `TestAccounts_ListStateAndNeedsLogin`: one account with a session and a `watching` snapshot, one without a session.
  - `TestAccounts_DeleteStopsAndKeepsHistory`: delete gives 204, the account row and session are gone, a `claim_log` row for it remains, `AccountRemoved` was called with its ID.
  - `TestAccounts_Rename`.
  - `TestTwitchDevice_Flow`: start returns the fake's `user_code`; status is `pending` while the fake returns `authorization_pending`, then `done`; the session is stored and `AccountChanged` called once. A GET with no flow gives 404.
  - `TestTwitchDevice_Expired`: an `ExpiresAt` in the past ends with `expired`.
  - `TestKickSession_StoresAndRegisters`: a valid cookies.txt gives `verified:true` from a fake verifier; channels reach `RegisterChannels`; a cookies.txt without `kick_session` gives 400.
- [ ] **Step 2:** Run `go test ./internal/api -run 'TestAccounts|TestTwitchDevice|TestKickSession' -v`. Expected: FAIL.
- [ ] **Step 3:** Implement.
- [ ] **Step 4:** Run `go test -race ./internal/api && go test ./... && gofmt -l .`. Expected: PASS.
- [ ] **Step 5:** Commit `feat(api): accounts, Twitch TV device login and Kick cookie login`.

---

### Task 8: Settings, notifications and daily summary

**Files:**
- Modify: `internal/store/settings.go` (new keys), `internal/notify` (add `EventSummary = "summary"` and its Discord message)
- Create: `internal/api/settings.go`, `internal/summary/summary.go`
- Modify: `internal/api/server.go`, `internal/i18n/locales/{en,es,zh-CN}.json` (summary message keys)
- Test: `internal/store/settings_test.go`, `internal/api/settings_test.go`, `internal/summary/summary_test.go`

**Interfaces:**
- Produces:
  - `*store.Settings` methods: `UILanguage`/`SetUILanguage`, `UITimeFormat`/`SetUITimeFormat`, `UITheme`/`SetUITheme`, `NotifyDailySummary`/`SetNotifyDailySummary`, `UpdateAuto`/`SetUpdateAuto`, with keys `settings:ui_language`, `settings:ui_time_format`, `settings:ui_theme`, `settings:notify_daily_summary`, `settings:update_auto` and the defaults from Call 8.
  - Deps fields `Settings *store.Settings`, `Notifier Notifier` (legacy interface), `OnSettingsUpdate func()`, `SetZone func(name string)`.
  - PUT validation: language in `en|es|zh-CN`, time format in `24h|12h`, theme in `dark|light`, timezone loadable by `time.LoadLocation` (or `""` for the TZ env fallback), webhook `""` or prefixed `https://discord.com/api/webhooks/` or `https://discordapp.com/api/webhooks/`. Any invalid field gives 400 and saves nothing. `webhook_hint` keeps the URL up to `/webhooks/` and replaces the rest with `••••`. Notify toggles write through `SetNotifyKinds`, keeping the stored progress and canary values. After a save: `OnSettingsUpdate()`, and `SetZone` when the timezone changed.
  - `summary.Config{ Settings *store.Settings; Minutes *store.WatchMinutes; Claims *store.ClaimLog; Notifier notify.Notifier; Zone func() *time.Location; Now func() time.Time }`, `func Run(ctx context.Context, c summary.Config)`, `func NextMidnight(now time.Time, loc *time.Location) time.Time`. At each midnight, when enabled, it sends `EventSummary` with fields `day`, `minutes`, `claims` for the day that ended.
- [ ] **Step 1: Write the failing tests**
  - `TestSettings_UIDefaults` (store).
  - `TestSettings_GetMasksWebhook`: stored `https://discord.com/api/webhooks/123/abc` gives `webhook_hint:"https://discord.com/api/webhooks/••••"` and `configured:true`.
  - `TestSettings_PutPartial`: `{"theme":"light"}` changes only the theme and returns the full body; `OnSettingsUpdate` called once.
  - `TestSettings_PutInvalidSavesNothing`: `{"theme":"light","language":"fr"}` gives 400 and the theme stays `dark`.
  - `TestSettings_NotifyKeepsProgress`: stored progress true; PUT `notify.claim:false` keeps progress true.
  - `TestSettings_TimezoneCallsSetZone`.
  - `TestNotifyTest_NoWebhook400` and `TestNotifyTest_SendFails502`.
  - `TestNextMidnight_DST`: `2026-03-08T01:30` in `America/New_York` gives `2026-03-09T00:00` local.
  - `TestSummary_SendsYesterday`: fake clock at 23:59:59 local; after midnight, one `summary` notify with the day that ended, its minutes and its claim count; with the toggle off, none.
- [ ] **Step 2:** Run the three packages' tests. Expected: FAIL.
- [ ] **Step 3:** Implement. Summary text goes through `i18n.T(lang, key)` with the stored `ui_language`; add the keys to all three locale files.
- [ ] **Step 4:** Run `go test ./internal/store ./internal/api ./internal/summary ./internal/notify ./internal/i18n && go test ./... && gofmt -l .`. Expected: PASS.
- [ ] **Step 5:** Commit `feat(api): settings, notification toggles and daily summary`.

---

### Task 9: Health, updates and Kick image proxy

**Files:**
- Modify: `internal/update/checker.go`, `internal/store/settings.go` (`latest_release_notes`, `latest_release_url`)
- Create: `internal/api/health.go`, `update.go`, `image.go`
- Modify: `internal/api/server.go`
- Test: `internal/update/checker_test.go`, `internal/api/health_test.go`, `update_test.go`, `image_test.go`

**Interfaces:**
- Consumes: `mlog.Ring.Snapshot`, `canary.LoadResult`, the Kick backend's `FetchImage`, `store.AccountScans.List`.
- Produces:
  - `update.Checker`: stores the release body and `html_url` beside the tag; `func (c *Checker) Release() (latest, notesURL string, notes []string, checkedAt time.Time)` reads them without network; `Run` skips a check when `UpdateAuto` is false.
  - Deps fields `Ring *mlog.Ring`, `StartTime time.Time`, `Commit string`, `KickSidecars func() []string`, `RunCanary func(context.Context) error`, `Update *update.Checker`.
  - Health events: newest first, at most 50 after the filter. `problems` = level `WARN` or `ERROR`, or kind `error` or `auth`; `claims` = kind `claim`. `counts` covers the whole ring. `account_id` comes from the line's `account` field; `account_name` from the accounts table.
  - `command` is always `docker compose pull && docker compose up -d`. `notes` are the first 5 lines starting with `- ` or `* `, marker stripped.
  - `RunCanary` runs in a goroutine on a detached 90 s context, as in legacy.
  - Image proxy: same path guard and upstream rules as `legacyapi/handlers_image.go`.
- [ ] **Step 1: Write the failing tests**
  - `TestChecker_StoresNotes`: a fake GitHub server returning a body with three bullets and a paragraph; `Release()` gives the three bullets and the URL.
  - `TestChecker_AutoOffSkips`.
  - `TestHealth_FilterAndCounts`: a ring with one claim, one error, one info line; `filter=problems` returns only the error; counts are `all:3, problems:1, claims:1`.
  - `TestHealth_LogDownload`: `Content-Disposition: attachment; filename="grubdrops-log.txt"`, one line per ring entry.
  - `TestHealth_CanaryRuns202`.
  - `TestUpdate_ShapeAndCheck`.
  - `TestImage_RejectsOtherPaths`: `u=https://evil/x` and `u=users/x` give 400.
- [ ] **Step 2:** Run the packages' tests. Expected: FAIL.
- [ ] **Step 3:** Implement.
- [ ] **Step 4:** Run `go test ./internal/update ./internal/api && go test ./... && gofmt -l .`. Expected: PASS.
- [ ] **Step 5:** Commit `feat(api): health, updates with release notes, Kick image proxy`.

---

### Task 10: Pool restarts panicked units

**Files:**
- Modify: `internal/runner/pool.go`
- Test: `internal/runner/pool_test.go`

**Interfaces:**
- Produces: `poolUnit` keeps its `Builder`. After a unit panics, `supervise` waits `panicRestart = time.Minute` (a package var so tests shorten it), releases the old entry, builds a fresh one, and runs it in the same slot, unless the unit's context ended. An `errStop` exit (Run returns nil) is not restarted. `Snapshots` reads the current unit of each slot.
- [ ] **Step 1: Write the failing tests**
  - `TestPool_PanicRestartsFromBuilder`: a fake Unit that panics on its first Run; with `panicRestart` at 10 ms the builder is called twice and the second unit runs; `Snapshots` shows the second unit.
  - `TestPool_StopDuringPanicDelay`: `Stop()` during the delay returns promptly and the builder is not called again.
  - `TestPool_ErrStopNotRestarted`: a unit whose Run returns nil is built once.
- [ ] **Step 2:** Run `go test ./internal/runner -run TestPool -v`. Expected: FAIL.
- [ ] **Step 3:** Implement.
- [ ] **Step 4:** Run `go test -race ./internal/runner && go test ./... && gofmt -l .`. Expected: PASS.
- [ ] **Step 5:** Changelog (Changed): `- v2 pool restarts a panicked runner from its builder after one minute.` Commit `feat(runner): restart panicked units`.

---

### Task 11: Roster builder

**Files:**
- Create: `cmd/miner/roster.go`
- Test: `cmd/miner/roster_test.go`

**Interfaces:**
- Consumes: `acquireSession` and `sessionDeps` (`cmd/miner/session_load.go`), `loadLinkOverrides` (kept from `main.go`), `decodeKickChannels`, `store.ChosenGames.List`, `store.Skips`, `collect.Collector`, `api.Hub`, `api.RunnerJSON`, `runner.Pool.OrderFunc`, the Kick backend's `RegisterSidecar` and `RegisterChannels`.
- Produces:
  - `type roster struct` holding the dependencies above plus `backendFor func(gen.Account) (platform.Backend, error)` and `notifier runner.Notifier`.
  - `func (ro *roster) builder(a gen.Account) runner.Builder`:
    - Disabled account → `runner.Idle{State: runner.StateIdle}`.
    - `acquireSession` returns `needs_auth` → `runner.Idle{State: runner.StateNeedsLogin}`.
    - Otherwise `runner.New(runner.Config{...})` with `Games` from `ChosenGames.List` (mapped to `plan.Game{Name, Slug}`), `ForceLinked` from the link overrides, `Claims` = a `claimLog` adapter, `Skips` = `store.Skips`, `Order` = `Pool.OrderFunc(a.ID)`, `OnScan` = Collector then `hub.Publish(games)`, `OnProgress` and `OnBeat` = Collector, `OnChange` = `hub.Publish(runner, key a.ID, RunnerJSON(...))`, `ProgressNotifyStepPct: 0`, `TickInterval: 0` (DefaultTick).
    - Kick accounts call `RegisterSidecar(a.ID, a.DisplayName)` and `RegisterChannels(a.ID, decodeKickChannels(sess))` first, as 1.x `build` does.
  - `type claimLog struct` wrapping the Collector: `RecordClaim` records, then publishes `claim` with the item from the claim log; `ClaimedBenefitIDs` delegates.
  - `func (ro *roster) builders(ctx context.Context) ([]runner.Builder, error)` over `ListAllAccounts`.
  - `func (ro *roster) reloadAccount(id string)` (rebuild from the row, or `ReloadAccount(id, nil)` when the row is gone) and `func (ro *roster) accountRemoved(id string)` (`ReloadAccount(id, nil)`, `Collector.Forget(id)`, publish `accounts`). `func (ro *roster) gamesChanged()` reloads each account in `ListAllAccounts` order with `reloadAccount`, then publishes `games`.
- [ ] **Step 1: Write the failing tests** (temp DB, `platformtest` fakes, a real `runner.Pool` and `api.Hub`)
  - `TestRoster_NoSessionNeedsLogin`.
  - `TestRoster_DisabledIdle`.
  - `TestRoster_RunnerWiredToCollector`: one account with a session and chosen game Rust; after `Pool.Reload` and the fake's first scan, `drop_states` has rows for the account and a subscriber got a `games` event and a `runner` event keyed by the account.
  - `TestRoster_ClaimPublishes`: a completed fake drop produces a `claim_log` row with source `grubdrops` and a `claim` event.
  - `TestRoster_AccountRemoved`: after `accountRemoved`, `Pool.Snapshots` has no entry for it, `Board.Games` no longer counts its scan, and an `accounts` event went out.
  - `TestRoster_GamesChangedReloadsEach`.
- [ ] **Step 2:** Run `go test ./cmd/miner -run TestRoster -v`. Expected: FAIL.
- [ ] **Step 3:** Implement.
- [ ] **Step 4:** Run `go test -race ./cmd/miner && go test ./... && gofmt -l .`. Expected: PASS.
- [ ] **Step 5:** Commit `feat(miner): roster builds v2 runners wired to collector and hub`.

---

### Task 12: Switch the miner to v2 and delete 1.x orchestration

**Files:**
- Modify: `cmd/miner/main.go`, `cmd/miner/main_test.go`, `internal/web/embed.go`, `AGENTS.md`, `README.md` (UI and env sections only where they name removed features)
- Delete: `internal/legacyapi/`, `internal/watcher/`, `internal/scheduler/`, `internal/discovery/`, `internal/web/templates/`, `internal/web/static/` (unless the placeholder uses it), `internal/web/lang_csrf_test.go`, and the `main.go` helpers that only served them (`nopRunner`, `forceWatchStore`, `loadCollectOverrides`, `loadSkipOverrides`, `loadAccountWhitelist`, `loadAccountChannels`, `hasAnyGame`, `startDiscovery`)
- Test: `cmd/miner/main_test.go`

**Interfaces:**
- Consumes: everything above.
- Produces, in `run()`: config, logger with ring, store, cryptor, settings, sessions, scs (`api.NewKVSessionStore`, 12 h, HttpOnly, SameSite Strict, `Secure` from config), OIDC, registry, browser dial, dockerctl, proxy transport, Kick and Twitch backends (unchanged construction), notifier with the `indirectNotifier` rebuild, `runner.NewPool(notifier)`, `collect.New(db, q)` with `Loc` from the zone, `api.NewHub()` with `Run`, the roster, `pool.Reload(ctx, roster.builders(ctx))`, Kick `SweepSidecars`, authcheck, canary, update checker, `summary.Run`, a ring hook that publishes `log` events, `api.NewRouter(api.Deps{...})` with `SPA: web.Dist()`, the HTTP server, and shutdown that calls `pool.Stop()` and closes the Twitch backends.
  - `AGENTS.md`: replace the "Stay Go + `html/template` + HTMX" rule with "UI is a SvelteKit static SPA embedded in the Go binary; the Go side serves JSON + SSE." Update the Layout block to drop `internal/watcher`, `internal/discovery` and the HTML description of `internal/api` and `internal/web`, and add `internal/plan`, `internal/runner`, `internal/collect`, `internal/summary`.
- [ ] **Step 1: Write the failing test** `TestRun_BootsAndServesAPI` in `main_test.go`: run `run()` against a temp DB path with `GRUB_MASTER_KEY` set, `GRUB_UPDATE_CHECK=0`, and a free port in `GRUB_LISTEN` (or the existing listen env); `GET /api/session` returns 200 JSON with `setup_needed:true`; `GET /` returns the placeholder; cancel shuts down cleanly within 10 s.
- [ ] **Step 2:** Run `go test ./cmd/miner -run TestRun -v`. Expected: FAIL.
- [ ] **Step 3:** Rewrite `run()` and delete the packages and helpers. `go vet ./...` must show no unused code left behind.
- [ ] **Step 4:** Run `go build ./... && go test ./... && gofmt -l . && docker build -f deploy/Dockerfile.miner .`. Expected: all PASS, image builds. Then boot locally per AGENTS.md "Run locally" and check with curl: `GET /api/session`, `POST /api/setup` with the CSRF token, `GET /api/overview` gives `no_accounts`.
- [ ] **Step 5:** Changelog: Changed `- The miner runs the v2 planner, runner and collector, and serves the JSON API; the web UI is a placeholder until the SvelteKit app lands (rebuild/v2).` Removed `- HTML + HTMX web UI, watcher, scheduler and catalog scraper (replaced by v2).` Commit `feat(miner)!: run v2 and remove 1.x orchestration and HTML UI`.

---

## Left for plan 4

- Build the SvelteKit app against this contract into `internal/web/dist`, replacing the placeholder.
- Localise health event messages (the API returns raw `msg` and `kind`).
- Move the locale JSON so Go and Svelte share one set.
- `internal/web/dist/index.html` is tracked, so a Vite build into `dist` shows up as git changes. Decide how build output is handled.
- Rewrite the remaining 1.x UI text in `README.md` (line 3, the screenshot alt text, the Kick "Authorize" step), the Spanish and Chinese READMEs, `docs/DESIGN.md:24` and `docs/index.html:102`.
- Drop the 1.x keys in the locale JSON once the SPA's key set is known.
- `POST /api/logout` is public: an unauthenticated logout is a CSRF-gated no-op that returns 204.
- Clients upsert `claim` SSE events on `account_id` + `benefit_id`; a reward code re-sends the same claim.
- No v2 setter exists for proxy, canary channels and interval, Kick watch mode or notify avatar; stored values still apply. Nothing writes `link_override:` keys, which the roster still reads.

## Left for plan 5

- Seed `chosen_games`, `claim_log` and `watch_minutes` from 1.x data; boot a real 1.x DB copy and confirm every account mines with no re-login. Until then 1.x accounts boot with no games, so do not tag before this lands.
- Drop the unused 1.x tables and queries (`account_games`, `account_channels`, force channels, `campaigns`, `benefits`, `accounts.webhook_url`) and Kick `ActiveWatchPath`.
- Run `docker build -f deploy/Dockerfile.miner .`; it was not run during plan 3.
- Open minors: the games add path is not transactional, so a concurrent POST can lose an append; the account listing decrypts every session to test has-session; the daily summary skips missed days; the canary run-now goroutine is not joined on shutdown.
- The Twitch integrity-token log line is tracked as GRUB-20.
