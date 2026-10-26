# GrubDrops v2 Plan 5: 1.x Migration and Release Prep Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A 1.x database (master at goose 15, or `feat/pipeline-v2` at 16) boots on v2 with its games, claim history and minutes carried over, and the residual UX gaps found on staging are closed, so the branch is ready for the live-drop gate.

**Architecture:** A one-time Go import (`store.ImportLegacy`) runs right after goose in `store.Open`. It copies 1.x rows into the v2 tables inside one transaction and sets a `kv` marker, so it never runs twice. The carry-over fixes are small, independent changes to the API, runner snapshot, canary and SPA.

**Tech Stack:** Go 1.x, SQLite (modernc), goose, sqlc, SvelteKit 2 + Svelte 5, vitest.

**Spec:** `docs/superpowers/specs/2026-10-25-grubdrops-v2-rebuild-design.md` (sections "Data", "Decisions" rows Claim history, Total Minutes Watched, Per-account game lists, Rollback; "Order of work" step 6).

## Global Constraints

- v2.0.0 migrations only add tables and columns. Old 1.x tables stay. 1.x must still boot on the same DB.
- Accounts and encrypted sessions are untouched by the import, including non-TV Twitch sessions.
- Migration maps 1.x global games to chosen games in their current priority order, then appends the union of `account_games` (deduped).
- 1.x `claims` are copied into `claim_log` with source `store.ClaimSourceLegacy` (`"1.x"`). The minutes counter is seeded from `progress`.
- Never put `?` or parentheses in a `queries/*.sql` comment.
- Every Go-sent and UI string goes through i18n; add each key to every file in `internal/i18n/locales/` (`TestLocaleParity`, `npm --prefix web run check`).
- UI copy states facts only. No filler lines.
- No `Client-Integrity` header. Twitch beacon stays at 60 s (`runner.DefaultTick`).
- Gate before each commit: `gofmt -l .` empty, `go build ./...`, `go test ./...`; for web changes also `npm --prefix web run check` and `npm --prefix web test`.
- Log every change in `docs/CHANGELOG.md` under `## [Unreleased]` in the same commit.
- No push, tag or PR. Prod (`drops.ryuzec.dev`) is not touched.
- Toolchain paths: Go `/home/jandro/.local/go/bin/go`, npm in `/home/linuxbrew/.linuxbrew/bin`.

## Review Focus

- **Staging DB already at v2 (17) with user-chosen games:** the import must not overwrite a non-empty `chosen_games`. Pinned by `TestImportLegacy_KeepsExistingChosen` (Task 1).
- **1.x claim whose benefit or campaign row is gone or has empty fields:** it still lands in `claim_log` with the account's platform. Pinned by `TestImportLegacy_ClaimFallbacks` (Task 1).
- **Second boot after a successful import:** no duplicate minutes, no re-seeded games after the user removed them. Pinned by `TestImportLegacy_RunsOnce` (Task 1).
- **Fresh install:** the import is a no-op that still sets the marker. Pinned by `TestImportLegacy_FreshDB` (Task 1).
- **Paused and signed-out account:** shows the "Log in again" button and a needs-login problem, so the user is not stuck. Pinned by `TestOverview_PausedSignedOutIsProblem` (Task 3) and the Accounts test in Task 4.

---

### Task 1: Import 1.x data into the v2 tables

**Files:**
- Create: `internal/store/legacy_import.go`
- Modify: `internal/store/db.go` (call the import after `goose.UpContext`)
- Test: `internal/store/legacy_import_test.go`
- Modify: `docs/CHANGELOG.md`

**Interfaces:**
- Consumes: v2 tables from `0017_v2_collection.sql`; `ClaimSourceLegacy` in `internal/store/claim_log.go`; test helpers `seedV15` (`db_v2_test.go`) and `seedV16` (`db_collision_test.go`).
- Produces: `func ImportLegacy(ctx context.Context, db *sql.DB) error` and `const legacyImportKey = "v2.legacy_import_done"`.

Rules the import follows (raw SQL on `db`, one transaction, not sqlc):

1. Skip everything when `kv` has `legacyImportKey`. Commit sets it to `"1"`.
2. **chosen_games:** only when `chosen_games` is empty. Order: every `global_games` row by `rank ASC`, then every other `game_id` in `account_games` ordered by `MIN(rank) ASC, games.name ASC`. Write rank 0..n-1. Skip game ids missing from `games`.
3. **claim_log:** `INSERT OR IGNORE` one row per 1.x `claims` row. `platform` = `campaigns.platform`, else `accounts.platform`. `game`, `campaign_id`, `campaign_name` from `campaigns`; `benefit_name`, `image_url` from `benefits`; `code` = `COALESCE(json_extract(value_meta_json,'$.code'),'')`; `source` = `'1.x'`; `claimed_at` from the claim. Use `LEFT JOIN` for benefits, campaigns and accounts. Malformed `value_meta_json` must not fail the import: wrap with `CASE WHEN json_valid(value_meta_json) ...`.
4. **watch_minutes:** per account, `INSERT OR IGNORE INTO watch_minutes (account_id, day, minutes)` with day `''` and `SUM(progress.minutes_watched)`; skip sums of 0. Day `''` is the all-time bucket `WatchMinutes.Total` already counts.
5. Log `slog.Info("store 1.x import done", "kind", "state", "games", n, "claims", n, "minutes", n)`.
6. Any error rolls back and returns `fmt.Errorf("1.x import: %w", err)`; `Open` closes the DB and returns it, the same as a goose failure.

- [ ] **Step 1: Write the failing tests** in `internal/store/legacy_import_test.go`, using `seedV15` / `seedV16` plus direct inserts, then `Open`:
  - `TestImportLegacy_V15`: globals `g_rust` rank 0, `g_tarkov` rank 1; `account_games` a1:`g_tarkov` rank 0, a1:`g_valorant` rank 2, a2:`g_apex` rank 1. Expect chosen ids `[g_rust, g_tarkov, g_apex, g_valorant]`. A claim with `value_meta_json` `{"code":"ABC"}` arrives with `Code == "ABC"`, `Source == "1.x"`, the campaign's game and name. Progress 30 + 45 minutes for a1 makes `NewWatchMinutes(q).Total` return 75.
  - `TestImportLegacy_V16`: the same data on the `feat/pipeline-v2` shape (`seedV16`) gives the same result.
  - `TestImportLegacy_ClaimFallbacks`: a claim whose campaign row was deleted (insert the claim with `foreign_keys` off) lands with `platform` = the account's platform and empty game. A claim with `value_meta_json` = `'not json'` lands with `Code == ""`.
  - `TestImportLegacy_KeepsExistingChosen`: a DB already at 17 with `chosen_games = [g_apex]` and no marker keeps `[g_apex]` after `Open`, and still gets its claims.
  - `TestImportLegacy_RunsOnce`: `Open` twice, deleting one `chosen_games` row in between. Second `Open` leaves it deleted and `Total` stays 75.
  - `TestImportLegacy_FreshDB`: `Open` on a new path gives empty `chosen_games` (aside from migration seeds), empty `claim_log`, and `kv` holds the marker.

- [ ] **Step 2: Run to confirm they fail**
  Run: `go test ./internal/store -run TestImportLegacy -v`
  Expected: FAIL, `ImportLegacy` undefined.

- [ ] **Step 3: Implement `ImportLegacy` in `internal/store/legacy_import.go` and call it from `Open`** after goose, following the rules above.

- [ ] **Step 4: Run the store tests and the full gate**
  Run: `go test ./internal/store/... && go build ./... && go test ./...`
  Expected: PASS.

- [ ] **Step 5: Changelog + commit**
  Add under `[Unreleased]` / Added: "1.x upgrade carries over chosen games (global list, then per-account games), claim history and total minutes watched."
  ```bash
  git add internal/store/legacy_import.go internal/store/legacy_import_test.go internal/store/db.go docs/CHANGELOG.md
  git commit -m "feat(store): import 1.x games, claims and minutes on first v2 boot"
  ```

---

### Task 2: Box art by Twitch game ID

Name-based box art (`ttv-boxart/<name>-144x192.jpg`) redirects to `404_boxart` for newer games (Marvel Rivals, Delta Force). The ID form `ttv-boxart/<twitch_game_id>-144x192.jpg` works.

**Files:**
- Create: `internal/store/migrations/0018_games_twitch_id.sql` (`ALTER TABLE games ADD COLUMN twitch_game_id TEXT NOT NULL DEFAULT ''`)
- Modify: `internal/store/queries/*.sql` (new query `SetGameTwitchID`: `UPDATE games SET twitch_game_id = ? WHERE id = ? AND twitch_game_id <> ?`), regenerate sqlc in `internal/store/gen`
- Modify: `internal/platform/types.go` (`Campaign.GameID string`), `internal/platform/twitch/campaigns.go` (fill it from `c.Game.ID`)
- Modify: `internal/collect/collector.go` (on a successful Twitch scan, store each campaign's game ID onto the matching chosen game via `gameslug`/`plan.Match`)
- Modify: `internal/api/games.go` (`boxArtURL(name, twitchID string) string`)
- Test: `internal/api/games_test.go`, `internal/collect/collector_test.go`, `internal/platform/twitch/campaigns_test.go`

**Interfaces:**
- Produces: `platform.Campaign.GameID`; `gen.Game.TwitchGameID`; `boxArtURL(name, twitchID string) string` returns `https://static-cdn.jtvnw.net/ttv-boxart/<twitchID>-144x192.jpg` when `twitchID != ""`, else the current name URL.

- [ ] **Step 1: Write the failing tests**
  - `TestBoxArtURL_PrefersTwitchID`: `boxArtURL("Marvel Rivals", "1264310518") == "https://static-cdn.jtvnw.net/ttv-boxart/1264310518-144x192.jpg"`; `boxArtURL("Rust", "")` equals the escaped-name URL.
  - Twitch campaigns parse test: a fixture campaign with `game.id = "263490"` yields `Campaign.GameID == "263490"`.
  - Collector test: a Twitch scan whose campaign game matches chosen `Rust` with `GameID "263490"` leaves `games.twitch_game_id == "263490"`; a Kick scan writes nothing.
  - `GET /api/games` test: a chosen game with `twitch_game_id` set returns the ID-based `box_art_url`.
- [ ] **Step 2: Run them, expect FAIL** (`go test ./internal/api ./internal/collect ./internal/platform/twitch`).
- [ ] **Step 3: Implement** the migration, query (`sqlc generate` from `internal/store`), field, collector write and API change.
- [ ] **Step 4: Gate** `gofmt -l .`, `go build ./...`, `go test ./...`; expect PASS. Confirm `TestOpen_*` migration tests still pass with 0018 present.
- [ ] **Step 5: Changelog (Fixed: "Box art shows for newer games such as Marvel Rivals and Delta Force.") + commit** `fix(games): box art by Twitch game ID`.

---

### Task 3: Overview problems for paused signed-out accounts, and next scan time

**Files:**
- Modify: `internal/runner/state.go` (`Snapshot.NextScanAt time.Time`), `internal/runner/runner.go` (fill it in `Snapshot()`)
- Modify: `internal/api/overview.go`, `internal/api/games.go`
- Test: `internal/runner/runner_test.go`, `internal/api/overview_test.go`, `internal/api/games_test.go`

**Interfaces:**
- Produces: `runner.Snapshot.NextScanAt` = `lastScanTry + rescanEvery` when `lastScanTry` is set, else zero. JSON: overview `status.next_scan_at` and games `next_scan_at`, both RFC 3339 or `null`, the earliest non-zero `NextScanAt` among snapshots of non-paused accounts.
- Produces: overview `problems` adds `{kind:"needs_login"}` for any account that has no session (`s.d.Sessions.Get` reports none), paused or not, without duplicates when the runner already reports `needs_login`. A paused account with a session is still not a problem.

- [ ] **Step 1: Write the failing tests**
  - `TestSnapshot_NextScanAt`: after one scan at time T, `Snapshot().NextScanAt == T.Add(rescanEvery)`; before any scan it is zero.
  - `TestOverview_PausedSignedOutIsProblem`: account `b2` with `enabled=0` and no session gives `problems == [{needs_login, b2, Bob}]`; `accounts_healthy` does not count it.
  - `TestOverview_PausedWithSessionNoProblem`: `enabled=0` with a session gives `problems == []`.
  - `TestOverview_NextScanAt`: two snapshots with `NextScanAt` T+5m and T+2m give `status.next_scan_at` = T+2m; a paused account's snapshot is ignored.
  - `TestGames_NextScanAt`: `GET /api/games` carries the same value.
- [ ] **Step 2: Run, expect FAIL** (`go test ./internal/runner ./internal/api`).
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Gate**, expect PASS.
- [ ] **Step 5: Changelog (Fixed: "Paused accounts that are signed out now show as needing login.") + commit** `fix(api): flag signed-out paused accounts and expose next scan time`.

---

### Task 4: SPA residuals

**Files:**
- Modify: `web/src/routes/(app)/+page.svelte` (Home offline before first load)
- Modify: `web/src/routes/(app)/settings/Accounts.svelte` (paused and signed-out row)
- Modify: `web/src/lib/home.ts` (`homeView` counts only `needs_login`)
- Modify: the page that shows "Last scan" (games page and Home stamp) to use `next_scan_at`
- Modify: `web/src/lib/api/types.ts` or wherever the overview and games types live (`next_scan_at: string | null`)
- Modify: `internal/i18n/locales/*.json` (new keys in every locale)
- Test: `web/src/lib/home.test.ts`, the existing Accounts/Games component tests or a new `*.test.ts` beside them

**Interfaces:**
- Consumes: Task 3 JSON fields `status.next_scan_at`, games `next_scan_at`, and the extra `needs_login` problems.

Decisions:
- Home with `mode === 'offline'` and no overview loaded yet renders `<div class="empty err" role="alert">` with new key `home.offline` = "GrubDrops is not responding. Retrying." and the existing retry button. Once data has loaded, offline keeps today's behaviour.
- In `Accounts.svelte`, a row that is both paused and signed out shows the "Log in again" button and the "Resume mining" button. The status pill stays the signed-out pill.
- `homeView`'s "all signed out" check counts only problems with `kind === 'needs_login'`, deduped by `account_id`.
- Scan stamp: last `null` and next set gives new key `scan.first_in` = "First scan in {time}". Last set and next set gives the existing last-scan text plus new key `scan.next_in` = "Next in {time}". Both `null` keeps today's text. `{time}` uses the existing relative-time helper.

- [ ] **Step 1: Write the failing tests**
  - `home.test.ts`: problems with one `needs_login` and one other kind, plus 2 accounts total, is not `signed_out`; two `needs_login` for the same account count once.
  - A test for the scan stamp helper (put the pure function in `web/src/lib/scan.ts` as `scanStamp(last: string|null, next: string|null, now: Date): {key: string, vars: Record<string,string>}[]`) covering the three cases above.
  - An Accounts test (or a pure helper `rowActions(a): ('login'|'resume')[]` in `web/src/lib/accounts.ts`): paused + `needs_login` gives `['login','resume']`; paused alone gives `['resume']`; signed-out alone gives `['login']`.
- [ ] **Step 2: Run, expect FAIL** (`npm --prefix web test`).
- [ ] **Step 3: Implement**, adding the i18n keys to every locale file.
- [ ] **Step 4: Gate** `npm --prefix web run check`, `npm --prefix web test`, `npm --prefix web run build`, `go test ./internal/i18n`; expect PASS.
- [ ] **Step 5: Changelog (Fixed: Home shows a message when the miner is unreachable on first load; paused signed-out accounts offer "Log in again"; scan stamp shows when the next scan runs) + commit** `fix(web): offline Home, paused signed-out login, next scan stamp`.

---

### Task 5: Canary skip result and Run-now feedback

**Files:**
- Modify: `internal/canary/canary.go` (save a skip result), the canary `Result` type (add `Skipped bool` with JSON `skipped`)
- Modify: `internal/api/health.go` (`canaryOut.Skipped bool json:"skipped"`)
- Modify: the Health section of the SPA that calls `POST /api/health/canary`, and `internal/i18n/locales/*.json`
- Test: `internal/canary/canary_test.go`, `internal/api/health_test.go`, a web test for the pure Run-now state helper

Decisions:
- No enabled session for a platform with a channel configured: `SaveResult` with `{OK:false, Skipped:true, Detail:"", CheckedAt: now}` and no notification. It must not count as an OK-to-fail transition for later notifications: when `prev.Skipped`, treat it as no previous result. No channel configured: unchanged (no save).
- UI: a skipped canary row shows new key `health.canary_skipped` = "Skipped: no signed-in {platform} account" in the neutral tone, not "OK".
- Run now: on click the button disables and reads new key `health.canary_running` = "Running…". It polls `GET /api/health` every 5 s until any canary `checked_at` is newer than the click or 90 s pass, then re-enables. At 90 s with nothing new it shows new key `health.canary_no_result` = "No result. Check that a channel is set and an account is signed in."

- [ ] **Step 1: Write the failing tests**
  - `TestRunOnce_NoSessionSavesSkip`: channel set, source returns `ok=false`; `LoadResult` gives `Skipped == true`, `CheckedAt` set, and the notifier got no call.
  - `TestRunOnce_SkipThenFailNotifiesAsFirst`: a saved skip followed by a failing probe notifies the same way a first-ever fail does.
  - `TestHealth_CanarySkipped`: `GET /api/health` returns `"skipped": true` for that platform.
  - Web: `runNowState(clickedAt, canaries, now)` returns `'running'`, `'done'` or `'no_result'` for the three cases.
- [ ] **Step 2: Run, expect FAIL.**
- [ ] **Step 3: Implement**, i18n keys in every locale.
- [ ] **Step 4: Gate** (Go and web), expect PASS.
- [ ] **Step 5: Changelog (Fixed: the canary says when it skipped for lack of a signed-in account; Run now shows progress) + commit** `fix(health): canary skip result and run-now feedback`.

---

### Task 6: GRUB-20, keep Twitch integrity tokens out of logs

**Files:**
- Modify: `internal/platform/twitch/client.go:234` and any other log line in `internal/platform/twitch` that logs a token or a raw response body
- Test: `internal/platform/twitch/client_test.go`

Decisions: log only `expiration` and `is_bad_bot` at Info. Drop `token_prefix` and `raw`. In the empty-token error, drop the `body=%q` raw body. Sweep `internal/platform/twitch` with `grep -n "token\|raw" ` on `slog.` lines and fix any other token or raw-body field the same way.

- [ ] **Step 1: Write the failing test** `TestIntegrity_LogsNoToken`: capture `slog` into a buffer handler, run the integrity fetch against an `httptest` server returning `{"token":"SECRET-TOKEN-123","expiration":...}`, assert the buffer does not contain `SECRET` and does not contain `"raw"`.
- [ ] **Step 2: Run, expect FAIL.**
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Gate**, expect PASS.
- [ ] **Step 5: Changelog (Fixed: Twitch integrity tokens and raw responses no longer appear in logs) + commit** `fix(twitch): keep integrity token out of logs (GRUB-20)`.

---

### Task 7: Upgrade test on the real staging DB, image build, staging walk (controller)

The controller runs this task itself, not a subagent.

- [ ] **Step 1: Real-DB upgrade test.** Copy the staging `miner.db.pre-v2` (goose 16) to the scratchpad through the alpine container, run the miner locally against the copy with a throwaway `GRUB_MASTER_KEY` only for schema work (sessions will not decrypt; that is expected). Expected log: `store 1.x import done` with games, claims ≈821 and minutes > 0. Query `chosen_games`, `claim_log`, `watch_minutes` counts. Delete the copy afterwards.
- [ ] **Step 2: Image build on host.** `git archive HEAD | ssh … docker build -f deploy/Dockerfile.miner -t grubdrops:staging-<sha> -`. Expected: build succeeds.
- [ ] **Step 3: Staging redeploy.** Back up the staging `miner.db` as `miner.pre-<sha>.db`, write `compose.verify-<sha>.yml`, `docker compose -f compose.yml -f compose.verify-<sha>.yml up -d`. Expected log: `migrated database to version: 18` and `store 1.x import done`.
- [ ] **Step 4: Browser walk** with `playwright-cli -s=claude`: Home, Games (box art for Marvel Rivals and Delta Force, next scan stamp), Rewards (1.x claims listed, "all time" count), Settings Accounts (paused signed-out rows show Log in again), Health (canary skip text, Run now feedback). 0 console errors. Do not Resume accounts.
- [ ] **Step 5: Docs.** Consolidate `docs/CHANGELOG.md` `[Unreleased]` (dedupe, group Added/Changed/Fixed/Removed). Add rulings to `docs/superpowers/v2-decisions.md`. Write the prod upgrade runbook in `docs/superpowers/v2-prod-upgrade.md`: prod is at goose 16, the `.pre-v2` backup and import run automatically, rollback is the 1.x image on the same DB or restoring `miner.db.pre-v2`. Do not run it. Commit `docs: v2 changelog, decisions and prod upgrade runbook`.

The live-drop check and the `v2.0.0` tag stay with Jandro (spec release gate).
