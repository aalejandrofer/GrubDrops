# v2 Plan 2: Discovery + Collection Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make platform inventory the truth for what each account has claimed, keep one persisted state per drop per account, keep GrubDrops' own claim log and an all-time minutes counter, and give the runner a scan schedule that does not burst. Nothing is wired into `cmd/miner` yet (plan 3).

**Architecture:** The runner already scans per chosen game: `ListActiveCampaigns` with the chosen-game filter, `InventoryProgress`, then `plan.Build`. This plan adds three runner hooks (`OnScan` after every scan, `OnProgress` after every tick poll, `OnBeat` after every credited beacon), a rescan while watching, and queue reuse when a campaign has no live stream. A new package `internal/collect` listens on those hooks: it derives each drop's state from the planner's result (pure `Derive`), writes it to new SQLite tables, adds claims found in inventory to the claim log, counts watched minutes, and keeps an in-memory per-game board for the Games page. `plan.Build` gains the reconcile rule: an unclaimed inventory row with partial progress overrides a claim-log entry.

**Tech Stack:** Go (module `github.com/aalejandrofer/grubdrops`), SQLite via `modernc.org/sqlite`, goose migrations, sqlc v1.31.1, `log/slog`.

**Spec:** `docs/superpowers/specs/2026-10-25-grubdrops-v2-rebuild-design.md`

This is plan 2 of 5. Plan 1 (planner + runner) is done; its carry-overs are at the end of `docs/superpowers/plans/2026-10-25-v2-plan-1-planner-runner.md`. Plan 3 wires `runner.Pool` and `collect.Collector` into `cmd/miner` and serves the JSON API. Plan 5 seeds the new tables from 1.x data.

## Global Constraints

- Branch `rebuild/v2`, local only: no push, no PR. Run `gofmt -w .`, `go build ./...` and `go test ./...` green before every commit.
- Migrations only add tables and columns. 1.x must still boot on the migrated DB. Before the first v2 migration runs on an existing DB, copy `miner.db` to `miner.db.pre-v2`.
- Never touch `internal/platform/twitch`, `internal/platform/kick`, `internal/watcher`, `internal/scheduler`, `internal/discovery`, `internal/api` or `cmd/miner`.
- Twitch beacon stays one `Heartbeat` per 60 s while watching (`DefaultTick`). Never add a `Client-Integrity` header.
- Every account earns every drop: state, claim log and minutes are keyed by account.
- Inventory decides state. The claim log is history. Claims that reconcile finds made outside GrubDrops are added to the log.
- Per-drop states, exact strings: `eligible`, `accruing`, `claimable`, `claimed`, `blocked`. A blocked row carries the planner reason: `not_linked`, `precondition`, `skipped`.
- sqlc: never put `?` or parentheses in a `queries/*.sql` comment. Regenerate with `cd internal/store && go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1 generate`, and commit the `gen/` output.
- Log lines keep the `"kind"` attribute (`state`, `discovery`, `progress`, `claim`, `heartbeat`, `error`, `auth`) plus `"account"`.
- Add one line per task to `docs/CHANGELOG.md` under `## [Unreleased]` → `### Added` or `### Changed`, in the style of the existing v2 line.

## Calls made in this plan (check these)

1. **Rescan while watching every 15 min** (`rescanEvery`). Idle and sleeping runners keep rescanning every 5 min (`recheckInterval`, unchanged). A watching runner only needs the rescan for Up next, blocked drops and the Games page. Drop progress already arrives every 60 s through `OnProgress`, and 15 min keeps Twitch TV-path scans (one directory call plus up to 10 `AvailableDrops` per game) off the rate limit. No preemption, as in plan 1.
2. **Claim log vs inventory.** A claim-log entry stays claimed unless inventory shows that benefit unclaimed with minutes below the requirement. An unclaimed row at full minutes right after a claim is Twitch lag, so the log wins and nothing is claimed twice. A benefit absent from inventory keeps its log state, because Twitch drops finished campaigns from the in-progress list.
3. **Total Minutes Watched counts beacons:** +1 minute per successful `Heartbeat`, because Twitch credits one minute per beacon. Kick's multi-reward sweep advances several drops per watch, so counting inventory deltas would overcount. Days are bucketed in a configurable location (plan 3 passes the timezone setting). The row with day `""` is reserved for minutes carried over from 1.x (plan 5).
4. **No-stream campaigns reuse the last scan's queue.** When the head campaign has no live channel, the runner moves to the next queue entry without rescanning. It rescans only when the whole queue is exhausted, and then sleeps `recheckInterval`.
5. **Kick sweep claims go to the claim log** under the queued target whose benefit name matches the reward title, with that target's game. This fixes the hardcoded `Game: "Rust"` in notifications without touching `internal/platform/kick`. A title that matches no target is notified without a game and left for reconcile.
6. **The pre-v2 backup lives in `store.Open`** now, not in plan 5. The first v2 migration lands in this plan, and staging runs this branch.
7. **The Games board is in memory.** Runners scan right after start, so restart costs one scan. The last scan time per account is also persisted (`account_scans`) for the Settings Health view.

## Review Focus

1. **Claim log says claimed, inventory says otherwise** (a 1.x false "collected" mark, or inventory lag right after a claim): expect partial progress to re-queue the drop and a full-minutes unclaimed row to stay claimed. Tests: `TestBuild_InventoryPartialOverridesLog`, `TestBuild_LogClaimSurvivesFullUnclaimedRow` (Task 3).
2. **Inventory read fails during a scan:** expect existing drop states kept, not wiped to `eligible`. Test: `TestCollector_InventoryFailureKeepsStates` (Task 8).
3. **Every campaign for the chosen games ends:** expect the account's drop states cleared and the Games board to show no campaigns. Tests: `TestBoard_GameWithoutCampaigns` (Task 7), `TestCollector_NoCampaignsClearsStates` (Task 8).
4. **Several queued campaigns with no live channel:** expect one `ListActiveCampaigns` per round, not one per campaign. Test: `TestRunner_NoStreamReusesQueue` (Task 5).
5. **Upgrade from a 1.x DB, then restart:** expect `miner.db.pre-v2` written once with the 1.x data and never overwritten; a fresh install writes no backup. Tests: `TestOpen_BacksUpBeforeV2`, `TestOpen_BackupNotOverwritten`, `TestOpen_FreshDBNoBackup` (Task 1).

---

## File Structure

| File | Responsibility |
|---|---|
| `internal/store/migrations/0016_v2_collection.sql` | New tables: `chosen_games`, `claim_log`, `drop_states`, `watch_minutes`, `account_scans` |
| `internal/store/db.go` | `Open` gains the pre-v2 backup |
| `internal/store/queries/v2.sql` | sqlc queries for the new tables |
| `internal/store/claim_log.go` | `ClaimLog`, `ClaimEntry`, claim sources |
| `internal/store/drop_states.go` | `DropStates`, `DropState` |
| `internal/store/watch_minutes.go` | `WatchMinutes` |
| `internal/store/account_scans.go` | `AccountScans`, `AccountScan` |
| `internal/store/chosen_games.go` | `ChosenGames`, `ChosenGame` |
| `internal/store/skips.go` | `Skips` (kv-backed, same keys as 1.x) |
| `internal/plan/plan.go` | `Result.Claimed`, claim-log override rule |
| `internal/runner/state.go` | `Scan`, new hooks, new `ClaimLog` signature |
| `internal/runner/runner.go` | `scan` split out of `pick`, rescan schedule, queue reuse |
| `internal/runner/watch.go`, `claim.go`, `pubsub.go` | Hook calls, sweep → claim log |
| `internal/collect/derive.go` | Pure `Derive` and `Found` |
| `internal/collect/collector.go` | `Collector`: hook handlers, `runner.ClaimLog`, persistence |
| `internal/collect/board.go` | `Board`: per-game campaigns and last scan |
| `*_test.go` beside each | Tests. Store and collect tests open a real DB with `store.Open(ctx, filepath.Join(t.TempDir(), "miner.db"))` |

---

### Task 1: Migration 0016 and pre-v2 backup

**Files:**
- Create: `internal/store/migrations/0016_v2_collection.sql`
- Modify: `internal/store/db.go`
- Test: `internal/store/db_v2_test.go`

**Interfaces:**
- Produces: the five tables below; `store.Open` unchanged in signature; `const firstV2Migration = 16`.

Schema (goose Up/Down with `StatementBegin`/`End`, as in `0014_account_force_channels.sql`; Down drops in reverse order):

```sql
CREATE TABLE chosen_games (
    game_id TEXT PRIMARY KEY REFERENCES games(id) ON DELETE CASCADE,
    rank    INTEGER NOT NULL
);
CREATE TABLE claim_log (
    account_id    TEXT NOT NULL,
    benefit_id    TEXT NOT NULL,
    platform      TEXT NOT NULL,
    game          TEXT NOT NULL DEFAULT '',
    campaign_id   TEXT NOT NULL DEFAULT '',
    campaign_name TEXT NOT NULL DEFAULT '',
    benefit_name  TEXT NOT NULL DEFAULT '',
    image_url     TEXT NOT NULL DEFAULT '',
    code          TEXT NOT NULL DEFAULT '',
    source        TEXT NOT NULL,
    claimed_at    INTEGER NOT NULL,
    PRIMARY KEY (account_id, benefit_id)
);
CREATE INDEX idx_claim_log_claimed_at ON claim_log(claimed_at);
CREATE TABLE drop_states (
    account_id       TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    benefit_id       TEXT NOT NULL,
    platform         TEXT NOT NULL,
    game             TEXT NOT NULL,
    campaign_id      TEXT NOT NULL,
    campaign_name    TEXT NOT NULL DEFAULT '',
    benefit_name     TEXT NOT NULL DEFAULT '',
    image_url        TEXT NOT NULL DEFAULT '',
    state            TEXT NOT NULL,
    reason           TEXT NOT NULL DEFAULT '',
    minutes          INTEGER NOT NULL DEFAULT 0,
    required_minutes INTEGER NOT NULL DEFAULT 0,
    ends_at          INTEGER NOT NULL DEFAULT 0,
    updated_at       INTEGER NOT NULL,
    PRIMARY KEY (account_id, benefit_id)
);
CREATE TABLE watch_minutes (
    account_id TEXT NOT NULL,
    day        TEXT NOT NULL,
    minutes    INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (account_id, day)
);
CREATE TABLE account_scans (
    account_id    TEXT PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
    scanned_at    INTEGER NOT NULL DEFAULT 0,
    reconciled_at INTEGER NOT NULL DEFAULT 0,
    error         TEXT NOT NULL DEFAULT ''
);
```

`claim_log` and `watch_minutes` have no account FK on purpose: history outlives a removed account. Times are Unix seconds.

- [ ] **Step 1: Write the failing tests**

  - `TestOpen_BacksUpBeforeV2`: create a DB at version 15 (`sql.Open` + `goose.SetBaseFS(migrationsFS)` + `goose.UpToContext(ctx, db, "migrations", 15)`), insert one `accounts` row (columns per `0010_drop_account_login.sql`), close it. `Open` the same path. Assert `path+".pre-v2"` exists, opening it read-only shows `goose_db_version` max 15 and the account row, and the live DB is at version 16 with table `claim_log`.
  - `TestOpen_BackupNotOverwritten`: after the test above, write a marker row to the live DB, `Open` again; the backup's mtime and contents are unchanged (the marker is absent).
  - `TestOpen_FreshDBNoBackup`: `Open` a new path; no `.pre-v2` file exists.
  - `TestMigration0016_OldTablesUntouched`: after `Open`, `claims`, `progress`, `games`, `global_games`, `account_games` still exist and keep their row counts from the version-15 fixture.

- [ ] **Step 2: Run tests to verify they fail**

Run: `PATH=$HOME/.local/go/bin:$PATH go test ./internal/store -run 'TestOpen_|TestMigration0016' -v`
Expected: FAIL (no backup file; no `claim_log`).

- [ ] **Step 3: Implement**

  Add the migration file. In `Open`, between ping and `goose.UpContext`, call `backupBeforeV2(ctx, db, path) error`. It reads `goose.GetDBVersionContext`. When the version is `> 0` and `< firstV2Migration`, and `path+".pre-v2"` does not exist, it runs `VACUUM INTO '<path>.pre-v2'` (WAL-safe; escape single quotes in the path). A backup error fails `Open`: never migrate without the copy. Log `slog.Info("store pre-v2 backup written", "kind", "state", "path", ...)`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `PATH=$HOME/.local/go/bin:$PATH go test ./internal/store -v`
Expected: PASS, including the existing store tests.

- [ ] **Step 5: Commit**

```bash
git add internal/store/migrations/0016_v2_collection.sql internal/store/db.go internal/store/db_v2_test.go docs/CHANGELOG.md
git commit -m "feat(store): v2 collection tables and pre-v2 backup"
```

---

### Task 2: Store wrappers for the new tables

**Files:**
- Create: `internal/store/queries/v2.sql`, `internal/store/claim_log.go`, `drop_states.go`, `watch_minutes.go`, `account_scans.go`, `chosen_games.go`, `skips.go`
- Regenerate: `internal/store/gen/*`
- Test: `internal/store/v2_test.go`

**Interfaces:**
- Consumes: Task 1 tables.
- Produces (all in package `store`; constructors take `(db *sql.DB, q *gen.Queries)` only where a transaction is needed, else `q`):

```go
const (
    ClaimSourceGrubDrops = "grubdrops" // claimed by the runner
    ClaimSourceInventory = "inventory" // found claimed by reconcile
    ClaimSourceLegacy    = "1.x"       // copied from 1.x claims (plan 5)
)
type ClaimEntry struct {
    AccountID, Platform, Game, CampaignID, CampaignName, BenefitID, BenefitName, ImageURL, Code, Source string
    ClaimedAt time.Time
}
type ClaimLog struct{ Q *gen.Queries }
func NewClaimLog(q *gen.Queries) *ClaimLog
func (l *ClaimLog) Record(ctx context.Context, e ClaimEntry) error
func (l *ClaimLog) ClaimedBenefitIDs(ctx context.Context, accountID string) (map[string]bool, error)
func (l *ClaimLog) Recent(ctx context.Context, limit int) ([]ClaimEntry, error) // newest first, all accounts
func (l *ClaimLog) CountSince(ctx context.Context, since time.Time) (int, error)

type DropState struct {
    AccountID, Platform, Game, CampaignID, CampaignName, BenefitID, BenefitName, ImageURL string
    State, Reason            string
    Minutes, RequiredMinutes int
    EndsAt, UpdatedAt        time.Time
}
type DropStates struct{ DB *sql.DB; Q *gen.Queries }
func NewDropStates(db *sql.DB, q *gen.Queries) *DropStates
func (s *DropStates) Replace(ctx context.Context, accountID string, rows []DropState) error // one tx: delete the account's rows, insert rows
func (s *DropStates) Upsert(ctx context.Context, row DropState) error
func (s *DropStates) List(ctx context.Context, accountID string) ([]DropState, error)   // ordered by game, campaign_id, required_minutes
func (s *DropStates) ListAll(ctx context.Context) ([]DropState, error)

type WatchMinutes struct{ Q *gen.Queries }
func NewWatchMinutes(q *gen.Queries) *WatchMinutes
func (w *WatchMinutes) Add(ctx context.Context, accountID, day string, minutes int) error
func (w *WatchMinutes) Total(ctx context.Context) (int, error)          // every account, every day, "" included
func (w *WatchMinutes) Day(ctx context.Context, day string) (int, error) // every account

type AccountScan struct {
    AccountID               string
    ScannedAt, ReconciledAt time.Time
    Error                   string
}
type AccountScans struct{ Q *gen.Queries }
func NewAccountScans(q *gen.Queries) *AccountScans
func (a *AccountScans) Put(ctx context.Context, s AccountScan) error
func (a *AccountScans) List(ctx context.Context) ([]AccountScan, error)

type ChosenGame struct{ ID, Name, Slug string }
type ChosenGames struct{ DB *sql.DB; Q *gen.Queries }
func NewChosenGames(db *sql.DB, q *gen.Queries) *ChosenGames
func (c *ChosenGames) List(ctx context.Context) ([]ChosenGame, error) // rank order, joined with games
func (c *ChosenGames) Set(ctx context.Context, gameIDs []string) error // one tx: replace all; rank = index

type Skips struct{ Q *gen.Queries }
func NewSkips(q *gen.Queries) *Skips
func (s *Skips) Skips(accountID string) (map[string]bool, error)
func (s *Skips) RecordSkip(accountID, benefitID string) error
func (s *Skips) ClearSkip(accountID, benefitID string) error
```

Semantics the tests pin:
- `ClaimLog.Record` upserts on `(account_id, benefit_id)`. It keeps the first `claimed_at` and `source`, a non-empty `Code` replaces the stored code, an empty one keeps it, and the other non-empty text fields refresh.
- `AccountScans.Put` upserts. A zero `ReconciledAt` keeps the stored value, a zero `ScannedAt` likewise, and `Error` is always written (an empty string clears it).
- `Skips` uses the 1.x kv keys `SkipOverridePrefix + benefitID + ":" + accountID` with value `"1"`, through `ListKVByPrefix`, `UpsertSettingString` and `DeleteKV`. See `loadSkipOverrides` in `cmd/miner/main.go:803` for the parsing; copy it, don't import it. Its methods take no ctx to match `runner.SkipStore`; use `context.Background()` with a 5 s timeout.
- `Set` with an unknown game ID returns the FK error and leaves the old list in place.

- [ ] **Step 1: Write the failing tests**

  - `TestClaimLog_RecordKeepsFirstClaimAndSource`: record source `grubdrops` at t1, then the same benefit with source `inventory` at t2 and code `ABC`. `Recent` returns one row with source `grubdrops`, `ClaimedAt` t1, code `ABC`. A third record with empty code keeps `ABC`.
  - `TestClaimLog_ClaimedIDsPerAccount`: rows for `a1/b1`, `a2/b2`; `ClaimedBenefitIDs(ctx,"a1")` is `{"b1":true}`.
  - `TestClaimLog_CountSince`: claims at now-25h, now-1h, now; `CountSince(now-2h)` is 2.
  - `TestDropStates_ReplaceIsPerAccount`: replace `a1` with 2 rows and `a2` with 1, then replace `a1` with 1 row; `List(a1)` has 1 row and `List(a2)` still has 1.
  - `TestDropStates_RoundTrip`: every field survives `Upsert` + `List`, times at second precision.
  - `TestWatchMinutes_TotalAndDay`: add 3 to `a1/2026-10-25`, 2 to `a2/2026-10-25`, 10 to `a1/""`, 1 to `a1/2026-10-24`. `Total` is 16 and `Day("2026-10-25")` is 5.
  - `TestAccountScans_ZeroKeepsReconciled`: Put scanned t1 / reconciled t1; Put scanned t2, zero reconciled, error `boom`; List shows t2, t1, `boom`.
  - `TestChosenGames_SetReplacesInOrder`: seed games via `UpsertGame`; `Set([g2,g1])` then `List` is `g2,g1`; `Set([g1, "nope"])` errors and `List` is still `g2,g1`.
  - `TestSkips_SameKeysAs1x`: `RecordSkip("a1","b1")` writes kv key `skip_override:b1:a1`; `Skips("a1")` is `{"b1":true}` and `Skips("a2")` is empty; `ClearSkip` removes it.

- [ ] **Step 2: Run to verify they fail**

Run: `PATH=$HOME/.local/go/bin:$PATH go test ./internal/store -run 'TestClaimLog_|TestDropStates_|TestWatchMinutes_|TestAccountScans_|TestChosenGames_|TestSkips_' -v`
Expected: FAIL (undefined types).

- [ ] **Step 3: Implement**

  Write `queries/v2.sql` (comments without `?` or parentheses), regenerate with the sqlc command from Global Constraints, then write the wrappers. Transactions use `s.Q.WithTx(tx)`.

- [ ] **Step 4: Run to verify they pass**

Run: `PATH=$HOME/.local/go/bin:$PATH go test ./internal/store -v && PATH=$HOME/.local/go/bin:$PATH go build ./...`
Expected: PASS; build clean.

- [ ] **Step 5: Commit**

```bash
git add internal/store docs/CHANGELOG.md
git commit -m "feat(store): claim log, drop states, minutes, scans, chosen games, skips"
```

---

### Task 3: Planner reconcile rule and claimed list

**Files:**
- Modify: `internal/plan/plan.go`
- Test: `internal/plan/plan_test.go`

**Interfaces:**
- Produces: `Result` gains `Claimed []Target`: benefits of chosen-game, active, non-reward campaigns that count as claimed, with `Minutes` set to the inventory minutes or, when absent, `RequiredMinutes`. Benefits with `RequiredMinutes <= 0` stay excluded everywhere. A benefit ID still appears in at most one of `Queue`, `Blocked`, `Claimed`.
- Rule: `in.Claimed[id]` (the log) counts unless `in.Progress` holds a row for `id` with `Claimed == false` and `MinutesWatched < b.RequiredMinutes`. An inventory row with `Claimed == true` always counts. Preconditions read the same effective claimed set.

- [ ] **Step 1: Write the failing tests**

  - `TestBuild_InventoryPartialOverridesLog`: log `{b1}`, progress `b1` 20/60 unclaimed. `b1` is in `Queue` with `Minutes` 20, and `Claimed` is empty.
  - `TestBuild_LogClaimSurvivesFullUnclaimedRow`: log `{b1}`, progress `b1` 60/60 unclaimed. `b1` is in `Claimed`, not in `Queue`.
  - `TestBuild_LogClaimSurvivesAbsentRow`: log `{b1}`, no progress row. `b1` is in `Claimed` with `Minutes` 60.
  - `TestBuild_InventoryClaimedListed`: no log, progress `b1` claimed. `b1` is in `Claimed`.
  - `TestBuild_ClaimedOnlyChosenActive`: a claimed benefit in a non-chosen game and one in an `upcoming` campaign are not in `Claimed`.
  - `TestBuild_PreconditionUsesOverride`: `b2` requires `b1`; log `{b1}`, progress `b1` 10/60 unclaimed. `b1` is queued and `b2` is blocked `precondition`.

- [ ] **Step 2: Run to verify they fail**

Run: `PATH=$HOME/.local/go/bin:$PATH go test ./internal/plan -v`
Expected: FAIL on the new tests; existing tests pass.

- [ ] **Step 3: Implement** the rule and `Claimed` in `Build`. Required minutes are per benefit, so resolve the override inside the campaign loop. Add a field comment on `Input.Claimed` that states the rule.

- [ ] **Step 4: Run to verify they pass**

Run: `PATH=$HOME/.local/go/bin:$PATH go test ./internal/plan ./internal/runner -v`
Expected: PASS (runner tests unaffected).

- [ ] **Step 5: Commit**

```bash
git add internal/plan docs/CHANGELOG.md
git commit -m "feat(plan): inventory overrides stale claim log; list claimed drops"
```

---

### Task 4: Runner hooks, scan split and rescan while watching

**Files:**
- Modify: `internal/runner/state.go`, `runner.go`, `watch.go`, `claim.go`, `pubsub.go`
- Test: `internal/runner/scan_test.go`; update `fakeClaims` in `runner_test.go`

**Interfaces:**
- Consumes: `plan.Result.Claimed` (Task 3).
- Produces (in `state.go`):

```go
// Scan is one completed scan of an account: campaigns, inventory and the plan.
type Scan struct {
    AccountID, Platform string
    At                  time.Time
    Err                 error // ListActiveCampaigns failed; the other fields are empty
    Campaigns           []platform.Campaign
    Progress            []platform.Progress
    InventoryOK         bool            // Progress came from a successful read
    Logged              map[string]bool // claim-log benefit IDs the plan used
    Result              plan.Result
}

type ClaimLog interface {
    // RecordClaim upserts a claim for t. A non-empty code is stored with it.
    RecordClaim(ctx context.Context, accountID string, t plan.Target, code string) error
    ClaimedBenefitIDs(ctx context.Context, accountID string) (map[string]bool, error)
}

// Config: OnInventory is removed. New nil-safe hooks; each may run on the
// Run goroutine or a PubSub goroutine and must not block for long.
OnScan     func(ctx context.Context, s Scan)
OnProgress func(ctx context.Context, accountID string, progress []platform.Progress)
OnBeat     func(ctx context.Context, accountID string) // after each successful Heartbeat
```

- `var rescanEvery = 15 * time.Minute` in `runner.go` (a var so tests shrink it).
- `func (r *Runner) scan(ctx context.Context, progress []platform.Progress, haveProgress bool) (plan.Result, error)`. It does everything `pick` did before choosing: list campaigns, read inventory unless `haveProgress`, `selfHeal`, `reapRewards`, claim log, `plan.Build`, set `queue`/`blocked`/`lastScanAt`, and call `OnScan`. On a `ListActiveCampaigns` error it calls `OnScan` with `Err` set and returns the error unchanged, so `pick` keeps its `ErrIntegrityBlocked` → `needs_login` path. `pick` calls `scan(ctx, nil, false)`, then chooses as before.
- In `tick`, after a successful inventory poll: call `OnProgress`. Then, if `time.Since(lastScanAt) >= rescanEvery`, call `scan(ctx, progress, true)`. A rescan never changes `current`, the handle or the state; on error it logs (`kind` `error`) and the watch goes on. Call `r.changed()` after a successful rescan.
- In `tick`, after a successful `Heartbeat`: call `OnBeat`.
- `finishClaim` calls `RecordClaim(rctx, account, *current, "")` before it clears `current` (keep the detached ctx). `onRewardCode` calls `RecordClaim(ctx, account, *current, code)` when there is a current target. Delete `RecordClaimWithCode` from the interface.

- [ ] **Step 1: Write the failing tests** (`scan_test.go`, white-box)

  - `TestRunner_OnScanCarriesPlan`: 1 campaign, 2 benefits, log `{b2}`. After `r.step` (pick), exactly one `Scan` arrived with `AccountID` `acc`, `InventoryOK` true, `Logged` `{b2}`, `Result.Queue[0].Benefit.ID` `b1`, `Result.Claimed[0].Benefit.ID` `b2`, `At` non-zero.
  - `TestRunner_OnScanReportsListError`: the fake's campaigns call returns an error. One `Scan` arrives with `Err` set, and the step returns the error.
  - `TestRunner_RescanWhileWatching`: with `rescanEvery = 0`, drive pick → start → tick. After the tick, `CallCount("ListActiveCampaigns")` is 2 and `CallCount("InventoryProgress")` is 2 (one for pick, one shared by the tick poll and the rescan). Two `Scan`s arrived, state is still `watching`, the current benefit is unchanged and `StopWatch` was not called. With the default `rescanEvery`, a tick makes no extra campaigns call.
  - `TestRunner_OnProgressAfterPoll`: one tick delivers the fake's progress rows to `OnProgress` once, and a failed poll delivers nothing.
  - `TestRunner_OnBeatPerHeartbeat`: three ticks give three `OnBeat` calls; a failing `Heartbeat` gives none for that tick.
  - `TestRunner_ClaimRecordsTarget`: after a successful claim, the recorded `plan.Target` has the campaign's `Game` and `Name` and the benefit ID; the code is `""`.
  - `TestRunner_RewardCodeRecordsTarget`: firing `OnRewardCode` through the PubSub fake while watching records the current target with code `XYZ`.

  Replace `fakeClaims` in `runner_test.go` with a recorder for the new interface, and update existing call sites.

- [ ] **Step 2: Run to verify they fail**

Run: `PATH=$HOME/.local/go/bin:$PATH go test ./internal/runner -v`
Expected: compile FAIL (no `Scan`, no `OnScan`).

- [ ] **Step 3: Implement** as specified in Interfaces.

- [ ] **Step 4: Run to verify they pass**

Run: `PATH=$HOME/.local/go/bin:$PATH go test -race ./internal/runner ./internal/plan`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/runner docs/CHANGELOG.md
git commit -m "feat(runner): scan, progress and beat hooks; rescan while watching"
```

---

### Task 5: Queue reuse, sweep claims, and plan 1 test gaps

**Files:**
- Modify: `internal/runner/watch.go`, `runner.go`, `claim.go`
- Test: `internal/runner/watch_test.go`, `claim_test.go`

**Interfaces:**
- Consumes: Task 4 `scan`, `ClaimLog.RecordClaim`.
- Produces: no new exported names.

Behavior:
- In `start`, when no stream is live for the current campaign, mark it in `noStream`, then take the first `r.queue` entry whose campaign is not in `noStream`. Set it as `current` (minutes from the target, channel and viewers cleared) and stay in `starting` with no rescan. When no entry is left, clear `current`, the watch view and `noStream`, and move to `sleeping`; `Run`'s existing `errIdle` wait then rescans after `recheckInterval`. Log the move with `kind` `discovery`.
- `notifySwept` resolves the reward title against `r.queue` and `r.blocked` (first target whose `Benefit.Name == Title`, skipping the current benefit as today). When found, it calls `RecordClaim(detached ctx, account, target, "")` and notifies with that target's `game` (overriding `cr.Game`). When not found, it notifies without a `game` field: the backend's value is not trusted.

- [ ] **Step 1: Write the failing tests**

  - `TestRunner_NoStreamReusesQueue`: 3 Rust campaigns, each with one benefit, and only the third with a live stream. Drive steps until `watching`. Expect `CallCount("ListActiveCampaigns")` == 1, `ListEligibleChannels` called for c1, c2 and c3 in that order, and the current benefit from c3.
  - `TestRunner_NoStreamAnywhereSleeps`: 2 campaigns, no streams. Steps end in `sleeping` with one campaigns call, `Snapshot().Current` nil and `noStream` empty.
  - `TestRunner_SweptRewardRecordedWithGame`: a Kick sweep returns `{Game: "Rust", Title: "Hoodie"}` while watching `b1`. The queue holds a target in game `Rust Console` whose benefit is named `Hoodie`. Expect `RecordClaim` with that target and the notify field `game` `Rust Console`. An unmatched title gives a notify with no `game` key and no `RecordClaim`.
  - `TestRunner_ClaimFailureRepicksAndRetries` (plan 1 carry-over): `Claim` fails once, then succeeds. Drive `Run` with small intervals: claiming → error backoff → picking → starting → watching → claiming → claimed. Expect 2 `Claim` calls, one `RecordClaim`, and the state back in `picking` or later.
  - `TestRunner_ReaperShellRowCampaign` (plan 1 carry-over): a chosen-game campaign with `Kind` `drop` and no benefits makes `ClaimRewards` get `[]string{"Rust"}`. A non-chosen shell row does not add its game.

- [ ] **Step 2: Run to verify they fail**

Run: `PATH=$HOME/.local/go/bin:$PATH go test ./internal/runner -run 'NoStream|Swept|ClaimFailureRepicks|ReaperShellRow' -v`
Expected: FAIL on `NoStreamReusesQueue` (3 campaigns calls) and `SweptRewardRecordedWithGame`. The two carry-over tests may pass at once; they pin existing behavior.

- [ ] **Step 3: Implement** the two behavior changes.

- [ ] **Step 4: Run to verify they pass**

Run: `PATH=$HOME/.local/go/bin:$PATH go test -race ./internal/runner`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/runner docs/CHANGELOG.md
git commit -m "fix(runner): reuse queue on no-stream; log swept claims under their game"
```

---

### Task 6: `collect.Derive` and `collect.Found`

**Files:**
- Create: `internal/collect/derive.go`
- Test: `internal/collect/derive_test.go`

**Interfaces:**
- Consumes: `runner.Scan` (Task 4), `plan.Result.Claimed` (Task 3), `store.DropState`, `store.ClaimEntry` (Task 2).
- Produces:

```go
// Package collect turns runner scans into persisted per-drop state, the
// claim log and the minutes counter.
package collect

const (
    StateEligible  = "eligible"
    StateAccruing  = "accruing"
    StateClaimable = "claimable"
    StateClaimed   = "claimed"
    StateBlocked   = "blocked"
)
func Derive(s runner.Scan, now time.Time) []store.DropState
func Found(s runner.Scan, now time.Time) []store.ClaimEntry
func StateFor(minutes, required int) string // eligible, accruing or claimable
```

- `Derive` makes one row per target in `Result.Queue`, `Result.Blocked` and `Result.Claimed`. Queue rows use `StateFor`: `claimable` when minutes ≥ required, `accruing` when minutes > 0, otherwise `eligible`. Blocked rows are `blocked` with `Reason` set to the plan reason. Claimed rows are `claimed` with minutes = required. Fields come from the target's campaign and benefit; `UpdatedAt` = now.
- `Found` returns a `ClaimEntry` with `Source` `inventory` for each `Result.Claimed` target whose benefit has a `Progress` row with `Claimed` true and is not in `s.Logged`.
- Both return nil when `s.Err != nil`.

- [ ] **Step 1: Write the failing tests**

  - `TestDerive_States`: a scan whose result has queue targets at 0/60, 30/60 and 60/60, one blocked `not_linked`, and one claimed. Expect states `eligible`, `accruing`, `claimable`, `blocked` with reason `not_linked`, and `claimed` with minutes 60. Every row has the account, platform, game, campaign ID and name, benefit name, image, required minutes and `EndsAt`.
  - `TestDerive_ErrScanEmpty`: `Err` set gives nil.
  - `TestFound_OnlyInventoryClaimsNotLogged`: claimed targets `b1` (inventory claimed, not logged), `b2` (inventory claimed, logged) and `b3` (logged only). `Found` returns only `b1`, with source `inventory` and `ClaimedAt` now.

- [ ] **Step 2: Run to verify they fail**

Run: `PATH=$HOME/.local/go/bin:$PATH go test ./internal/collect -v`
Expected: FAIL (package missing).

- [ ] **Step 3: Implement** `derive.go`.

- [ ] **Step 4: Run to verify they pass**

Run: `PATH=$HOME/.local/go/bin:$PATH go test ./internal/collect -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/collect docs/CHANGELOG.md
git commit -m "feat(collect): derive per-drop state and inventory-found claims"
```

---

### Task 7: `collect.Board`

**Files:**
- Create: `internal/collect/board.go`
- Test: `internal/collect/board_test.go`

**Interfaces:**
- Consumes: `runner.Scan`, `plan.Match`.
- Produces:

```go
type CampaignSummary struct {
    ID, Platform, Name string
    Drops              int // benefits with RequiredMinutes > 0
    EndsAt             time.Time
}
type GameStatus struct {
    Game       plan.Game
    Campaigns  []CampaignSummary // active, non-reward, at least one drop; deduped by platform+ID; EndsAt ascending
    LastScanAt time.Time         // latest successful scan of any account; zero = none yet
}
type Board struct{ /* mu; latest successful Scan per account */ }
func NewBoard() *Board
func (b *Board) Observe(s runner.Scan) // Err scans are ignored
func (b *Board) Forget(accountID string)
func (b *Board) Games(games []plan.Game) []GameStatus // one entry per game, in the given order
```

- [ ] **Step 1: Write the failing tests**

  - `TestBoard_MergesAccountsAndDedupes`: account a1 (Twitch) sees campaign `c1` (Rust, 3 drops, ends Oct 31); a2 (Twitch) sees `c1` and `c2` (Rust, ends Oct 30); a3 (Kick) sees `k1` (Rust). `Games([Rust])` has 3 campaigns ordered `c2`, `c1`, `k1`, with `c1.Drops` 3.
  - `TestBoard_GameWithoutCampaigns`: `Games([Rust, Overwatch])` gives Overwatch with an empty `Campaigns` and the same `LastScanAt` as Rust.
  - `TestBoard_ErrScanIgnored`: a successful scan at t1 and then an `Err` scan at t2 give `LastScanAt` t1 and keep the campaigns.
  - `TestBoard_LatestScanReplacesAccount`: a1 sees `c1`, then a1 sees nothing; Rust has no campaigns.
  - `TestBoard_SkipsRewardAndUpcoming`: `Kind` `reward`, status `upcoming`, and a campaign with no drops are all left out.
  - `TestBoard_Forget`: after `Forget("a1")`, a1's campaigns are gone.

- [ ] **Step 2: Run to verify they fail**

Run: `PATH=$HOME/.local/go/bin:$PATH go test ./internal/collect -run TestBoard -v`
Expected: FAIL.

- [ ] **Step 3: Implement** `board.go` with a mutex. `Games` builds fresh slices, so callers never share state with the board.

- [ ] **Step 4: Run to verify they pass**

Run: `PATH=$HOME/.local/go/bin:$PATH go test -race ./internal/collect`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/collect docs/CHANGELOG.md
git commit -m "feat(collect): per-game board for the Games page"
```

---

### Task 8: `collect.Collector`

**Files:**
- Create: `internal/collect/collector.go`
- Test: `internal/collect/collector_test.go`

**Interfaces:**
- Consumes: Task 2 store wrappers, Task 4 hooks and `runner.ClaimLog`, Task 6 functions, Task 7 `Board`.
- Produces:

```go
type Collector struct {
    Claims  *store.ClaimLog
    States  *store.DropStates
    Minutes *store.WatchMinutes
    Scans   *store.AccountScans
    Board   *Board
    Now     func() time.Time      // nil = time.Now
    Loc     func() *time.Location // nil = time.Local; picks the minutes day
    // unexported: mu sync.Mutex; last map[accountID]map[benefitID]store.DropState
}
func New(db *sql.DB, q *gen.Queries) *Collector // wires the four stores and a new Board
func (c *Collector) OnScan(ctx context.Context, s runner.Scan)
func (c *Collector) OnProgress(ctx context.Context, accountID string, progress []platform.Progress)
func (c *Collector) OnBeat(ctx context.Context, accountID string)
func (c *Collector) RecordClaim(ctx context.Context, accountID string, t plan.Target, code string) error
func (c *Collector) ClaimedBenefitIDs(ctx context.Context, accountID string) (map[string]bool, error)
var _ runner.ClaimLog = (*Collector)(nil)
```

Behavior. Every method is safe for concurrent use. Store errors are logged (`kind` `error`) and never panic; only `RecordClaim` and `ClaimedBenefitIDs` return them.
- `OnScan`: always `Board.Observe(s)`.
  - `Err` set: `Scans.Put` with `ScannedAt` = `s.At` and `Error` = the error text, then return.
  - Otherwise, record each `Found` entry with `Claims.Record`.
  - When `s.InventoryOK` or `len(s.Campaigns) == 0`: `States.Replace` with `Derive`, refresh the in-memory cache, then `Scans.Put` with scanned and reconciled both at `s.At` and an empty error.
  - Otherwise, only `Scans.Put` with `ScannedAt` and an empty error. States are kept.
- `OnProgress`: for each row whose benefit is in the account's cache, with state not `blocked` and not `claimed`, set minutes to the row's minutes. The state becomes `claimed` (minutes = required) if `row.Claimed`, else `StateFor`. `States.Upsert` only rows that changed. Rows not in the cache are ignored: the next scan places them.
- `OnBeat`: `Minutes.Add(accountID, day, 1)`, where day = `Now().In(Loc()).Format("2006-01-02")`.
- `RecordClaim`: `Claims.Record` with source `grubdrops`, the platform, game, campaign and benefit from `t`, the code, and `ClaimedAt` now. Then, if the benefit is cached, `Upsert` it as `claimed` so the UI flips before the next scan.
- `ClaimedBenefitIDs`: delegates to `Claims`.

- [ ] **Step 1: Write the failing tests** (real DB in `t.TempDir()`; insert an `accounts` row for the FK)

  - `TestCollector_ScanPersistsStates`: `OnScan` with a queue target at 30/60 and a claimed target. `States.List` shows `accruing` and `claimed`, and `Scans.List` shows scanned = reconciled = `s.At`.
  - `TestCollector_InventoryFailureKeepsStates`: a scan as above, then a scan with campaigns present and `InventoryOK` false whose result queues both benefits at 0. States are unchanged, `ScannedAt` moved and `ReconciledAt` did not.
  - `TestCollector_NoCampaignsClearsStates`: a scan with states, then a scan with no campaigns and `InventoryOK` false. `List` is empty.
  - `TestCollector_FoundClaimLogged`: a scan where `b1` is inventory-claimed and not logged. `Claims.Recent` has `b1` with source `inventory`. A second identical scan with `Logged` `{b1}` adds no row and keeps the source `inventory`.
  - `TestCollector_ScanErrorRecorded`: `Err` = `errors.New("boom")`. `Scans` shows error `boom`, states are untouched, and the board got the scan (`Board.Games` still shows the previous campaigns and `LastScanAt`).
  - `TestCollector_ProgressAdvancesState`: after a scan with `b1` at 0/60, `OnProgress` `b1` 30 gives `accruing` 30, then 60 gives `claimable`, then `Claimed:true` gives `claimed`. A row for an unknown benefit writes nothing.
  - `TestCollector_ProgressLeavesBlocked`: a blocked `not_linked` row stays `blocked` when progress arrives.
  - `TestCollector_BeatUsesLocationDay`: `Now` = 2026-10-25T23:30Z with `Loc` = `Asia/Shanghai` writes day `2026-10-26`; `Minutes.Day("2026-10-26")` is 1 after one beat and 3 after three.
  - `TestCollector_RecordClaimFlipsState`: after a scan with `b1` claimable, `RecordClaim` gives a `claim_log` row with source `grubdrops` and game `Rust`, and state `claimed`.
  - `TestCollector_ConcurrentHooks`: under `-race`, 4 goroutines call `OnProgress`, `OnBeat` and `RecordClaim` 50 times each after one scan, without a race report. The minutes total equals the number of beats.

- [ ] **Step 2: Run to verify they fail**

Run: `PATH=$HOME/.local/go/bin:$PATH go test ./internal/collect -run TestCollector -v`
Expected: FAIL (undefined `Collector`).

- [ ] **Step 3: Implement** `collector.go`.

- [ ] **Step 4: Run to verify they pass**

Run: `PATH=$HOME/.local/go/bin:$PATH go test -race ./internal/collect && PATH=$HOME/.local/go/bin:$PATH go test ./... && gofmt -l .`
Expected: PASS; `gofmt -l` prints nothing.

- [ ] **Step 5: Commit**

```bash
git add internal/collect docs/CHANGELOG.md
git commit -m "feat(collect): collector persists state, claims and minutes from runner hooks"
```

---

## Left for plan 3

- Wire `collect.New` into `cmd/miner`: pass `OnScan`, `OnProgress`, `OnBeat` and `Claims` to every `runner.Config`, use `store.Skips` for `Skips`, and build `Games` from `ChosenGames.List`. Call `Board.Forget` when an account is removed, and pass the timezone setting as `Loc`.
- API reads: `DropStates.ListAll`, `ClaimLog.Recent` and `CountSince`, `WatchMinutes.Total` and `Day`, `AccountScans.List`, `Board.Games`.
