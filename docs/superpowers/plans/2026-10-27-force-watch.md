# Force Watch Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Idle accounts (empty plan queue) watch the first live channel from a shared, ordered Force watch list. The list is managed in a panel on Games and summarised in a Home KPI tile.

**Architecture:**
- A new store table pair holds the ordered channel list and its account assignments.
- A daemon-wide `forcewatch.Checker` caches per-channel liveness. It uses a new optional backend capability `platform.ChannelLooker`.
- The runner gains a `force_watching` state. It enters it only when the plan queue is empty, and it rescans so a drop always wins.
- The API exposes `GET`/`PUT /api/force-watch` and adds a `force_watch` block to `/api/overview`. The SPA renders the Games panel and the Home tile.

**Tech Stack:** Go (chi, sqlc, goose, SQLite), SvelteKit SPA (Svelte 5 runes, vitest), i18n JSON locales.

**Spec:** `docs/superpowers/specs/2026-10-27-force-watch-design.md`. Mockups: `web/src/routes/sandbox/force-watch/` variant `a`, and `web/src/routes/sandbox/home-force-watch/` variant `a`.

## Global Constraints

- Run `gofmt -w .`, `go build ./...` and `go test ./...` green after every Go task. Run `npm --prefix web run check` and `npx vitest run` from `web/` after every web task.
- Twitch: never send a `Client-Integrity` header. Do not change `runner.DefaultTick` (60s).
- Never put `?` or parentheses in a `internal/store/queries/*.sql` comment.
- Add every user-facing string to **all** of `internal/i18n/locales/en.json`, `es.json` and `zh-CN.json`. `TestLocaleParity` enforces this, and `npm run check` rejects unused keys.
- chromedp `Evaluate`: base64-encode outside data. This plan should not touch it.
- No INFO log per tick or per live check. Log one INFO when force watch starts or switches channel. Everything else is DEBUG.
- Channel name rule: lowercase, `^[a-z0-9_]{2,25}$`. Hosts: `twitch.tv` and `kick.com`, with an optional `www.` or `m.` prefix.
- Log the change in `docs/CHANGELOG.md` under `## [Unreleased]` → Added.
- Do not commit `.dotfiles/`. Commit only when the user asks: the "Commit" steps below are **staging points** (`git add` only), unless the user says otherwise.
- Do not delete or touch `account_force_channels` or the 1.x `force_watch:*` kv rows. They are read-only inputs to the carry-over.

## Review Focus

1. **A drop appears while force watching.** The account must stop force watching within one rescan (≤ 5 min) and mine the drop. Test: Task 4 `TestForceWatch_DropWins`.
2. **The forced channel goes offline, or a higher-ranked channel goes live.** The account must switch to the right channel, or sleep with the 5-minute recheck. Test: Task 4 `TestForceWatch_SwitchesToHigherRankAndOffline`.
3. **An account has no chosen games but has force channels.** It must still force watch, not stop in `no_games`. Test: Task 4 `TestForceWatch_NoGamesStillWatches`.
4. **PUT puts an account on a channel of another platform, or repeats a channel.** Expect a 400 response with nothing written. Test: Task 5 `TestPutForceWatch_RejectsPlatformMismatchAndDuplicate`.
5. **A live lookup fails or times out.** Expect `live` = `null` and no runner crash: the runner treats the channel as not live and moves on. Test: Task 3 `TestChecker_ErrorLeavesUnknown`, and Task 4 `TestForceWatch_LookupErrorSkipsChannel`.

---

### Task 1: Store, migration and 1.x carry-over

**Files:**
- Create: `internal/store/migrations/0023_force_watch.sql`
- Create: `internal/store/queries/force_watch.sql`, plus regenerated `internal/store/gen/*` (`sqlc generate`; use the repo's existing invocation)
- Create: `internal/store/force_watch.go`, `internal/store/force_watch_test.go`

**Interfaces:**
- Produces:
  ```go
  type ForceChannel struct {
      Platform string   // "twitch" | "kick"
      Channel  string   // lowercase login/slug
      Accounts []string // account IDs, sorted
  }
  type ForceWatch struct{ DB *sql.DB; Q *gen.Queries }
  func NewForceWatch(db *sql.DB, q *gen.Queries) *ForceWatch
  func (f *ForceWatch) List(ctx context.Context) ([]ForceChannel, error)          // rank order
  func (f *ForceWatch) Set(ctx context.Context, list []ForceChannel) error        // replace all, one tx
  func (f *ForceWatch) ForAccount(ctx context.Context, accountID string) ([]string, error) // channels in rank order
  var ErrForceInvalid = errors.New("force watch: invalid list")
  ```

- [ ] **Step 1: Write failing tests** in `force_watch_test.go`. Use the existing store test DB helper from `account_games_test.go`.
  - `TestForceWatch_SetListRoundTrip`: Set `[{twitch keshaeuw [a1 a3]}, {kick xqc [a2]}]`, then List returns the same list in the same order.
  - `TestForceWatch_SetReplaces`: a second Set with one channel leaves only that channel.
  - `TestForceWatch_SetRejects`:
    - an account whose platform differs from the channel's → `ErrForceInvalid`;
    - a duplicate (platform, channel) → `ErrForceInvalid`;
    - an unknown account → error.
    - After each rejection, List is unchanged.
  - `TestForceWatch_ForAccount`: a3 gets `["keshaeuw"]`, and an unassigned account gets an empty slice.
  - `TestForceWatch_CascadeOnAccountDelete`: deleting account a1 removes it from the assignments, and the channel stays.
  - `TestMigration0023_CarriesOver1x`:
    - Seed before 0023, by migrating to 0022 with goose (see `migration_claims_test.go` for the pattern).
    - Accounts: tw1 (twitch), tw2 (twitch), kk1 (kick).
    - kv `force_watch:tw1`=`1` and `force_watch:kk1`=`1`; tw2 has no kv row.
    - `account_force_channels` rows: tw1 `keshaeuw` rank 0, tw1 `caedrel` rank 1, tw2 `xqc` rank 0, kk1 `xqc` rank 0.
    - After migrating up, List == `[{twitch keshaeuw [tw1]}, {kick xqc [kk1]}, {twitch caedrel [tw1]}]`, ordered by (rank, created_at). tw2's row is skipped because its flag is off.

- [ ] **Step 2: Run** `go test ./internal/store -run 'ForceWatch|Migration0023'`. Expected: FAIL (undefined).

- [ ] **Step 3: Implement.**
  - Migration: create the tables as the spec's "Store" section defines them, then run an `INSERT ... SELECT` carry-over.
    - Join `account_force_channels` to `accounts`, keeping rows where kv `force_watch:<id>` = `'1'`. `kv.value` is a BLOB, so compare with `CAST(value AS TEXT) = '1'`.
    - Insert channels with `INSERT OR IGNORE`, ranked by `ROW_NUMBER()` over (min rank, min created_at) per (platform, channel).
    - Then insert the assignments.
    - Down drops both new tables.
  - Queries: list channels by rank, list assignments, delete all, insert channel (returning id), insert assignment, list channels for one account. No `?` or parentheses in comments.
  - `Set` checks inside the transaction:
    - each channel name matches the global rule and each platform is twitch or kick;
    - no duplicates;
    - each account exists and its `platform` equals the channel's.
    - Any failure → `ErrForceInvalid`, wrapped with the reason.

- [ ] **Step 4: Run** `go test ./internal/store/...`. Expected: PASS.
- [ ] **Step 5: Stage** with `git add internal/store`.

---

### Task 2: Single-channel lookup on both backends

**Files:**
- Modify: `internal/platform/platform.go` (add the capability)
- Modify: `internal/platform/twitch/channels.go` and `internal/platform/twitch/backend.go`, with a test in `internal/platform/twitch/` (follow the existing httptest GQL tests for `listEligible`)
- Modify: `internal/platform/kick/backend.go`, with a test in `internal/platform/kick/` (follow the existing `ChannelLivestream` tests)
- Modify: `internal/platform/platformtest/fake.go` (fake implements it, scripted per login)
- Modify: `internal/platform/twitch/backend.go` (ready-drop capability, below)

**Interfaces:**
- Produces:
  ```go
  // ChannelLooker is optional: one channel's live stream by login (Twitch) or slug (Kick).
  type ChannelLooker interface {
      LookupChannel(ctx context.Context, s Session, login string) (stream Stream, live bool, err error)
  }
  // ReadyDropLister is optional: drops in the inventory that are fully watched and unclaimed,
  // whatever their game. Twitch only.
  type ReadyDropLister interface {
      ReadyDrops(ctx context.Context, s Session) ([]DropBenefit, error)
  }
  ```

- [ ] **Step 1: Write failing tests.**
  - Twitch `TestLookupChannel_LiveAndOffline`: the GQL `OpGetStreamInfo` fixture returns a stream. Expect `live`=true and Stream{Channel:"keshaeuw", ChannelID, BroadcastID, GameID, Game, ViewerCount} filled. With a null stream: `live`=false, err=nil. No game filter applies.
  - Twitch `TestLookupChannel_NoIntegrityHeader`: assert the request carries no `Client-Integrity` header.
  - Twitch `TestReadyDrops`: an inventory fixture holds one drop at 60/60 unclaimed with an instance id, one at 30/60 and one claimed. Only the first comes back, with `InstanceID` and `CampaignID` set.
  - Kick `TestLookupChannel`:
    - a live slug → `live`=true and Stream{Channel: slug, ChannelID, BroadcastID: livestream id, ViewerCount, Game: category};
    - offline → false;
    - HTTP 404 → `live`=false, err=nil;
    - 5xx → err.

- [ ] **Step 2: Run** `go test ./internal/platform/...`. Expected: FAIL.

- [ ] **Step 3: Implement.**
  - Twitch: factor the single-login body of `listEligible` into `func (ch *channels) lookup(ctx, sess, login) (platform.Stream, bool, error)`. `listEligible` keeps its game filter by calling `lookup`. `Backend.LookupChannel` calls `b.c.bind(s)` and then `lookup`.
  - Twitch `ReadyDrops`: reuse the inventory fetch behind `b.disc.inventory`. It must return the drop's required minutes alongside progress.
  - Kick: `Backend.LookupChannel` uses `api.ChannelLivestream` and `channelAndLivestream`.
  - `BrowserBackend` does not need either capability.

- [ ] **Step 4: Run** `go test ./internal/platform/...`. Expected: PASS.
- [ ] **Step 5: Stage** `internal/platform`.

---

### Task 3: `forcewatch.Checker` (shared live cache)

**Files:**
- Create: `internal/forcewatch/checker.go`, `internal/forcewatch/checker_test.go`

**Interfaces:**
- Consumes: `platform.Stream`
- Produces:
  ```go
  type LookupFunc func(ctx context.Context, platform, login string) (platform.Stream, bool, error)
  type Status struct { Live *bool; Stream platform.Stream; CheckedAt time.Time }
  type Checker struct { /* ttl, lookup, now, mu, cache, singleflight */ }
  func New(lookup LookupFunc, ttl time.Duration) *Checker          // ttl 2*time.Minute in prod
  func (c *Checker) Lookup(ctx context.Context, platform, login string) (platform.Stream, bool, error) // cached; for runners
  func (c *Checker) Peek(platform, login string) Status            // never blocks; Live nil = unknown
  func (c *Checker) Run(ctx context.Context, list func() []store.ForceChannel, every time.Duration) // background refresh
  ```

- [ ] **Step 1: Write failing tests** with a fake LookupFunc that counts calls and an injectable clock.
  - `TestChecker_CachesWithinTTL`: two Lookups within the TTL → 1 call. After the TTL → 2 calls.
  - `TestChecker_SharesInflight`: 10 concurrent Lookups for the same key → 1 call.
  - `TestChecker_ErrorLeavesUnknown`:
    - the lookup returns an error;
    - `Peek().Live == nil`;
    - `Lookup` returns the error;
    - nothing stale is cached as live.
  - `TestChecker_PeekAfterLookup`: after a live lookup, `*Peek().Live == true`.
  - `TestChecker_RunRefreshesAll`: `Run` with two channels and a short `every` → both get looked up. It stops when ctx is cancelled.
- [ ] **Step 2: Run** `go test ./internal/forcewatch`. Expected: FAIL.
- [ ] **Step 3: Implement.** Use `golang.org/x/sync/singleflight` if it's already in go.mod; otherwise a small per-key wait map. No new module. Each lookup gets a 10s timeout. Log failures at DEBUG.
- [ ] **Step 4: Run** `go test ./internal/forcewatch`. Expected: PASS.
- [ ] **Step 5: Stage.**

---

### Task 4: Runner `force_watching` state

**Files:**
- Modify: `internal/runner/state.go` (state const, Config fields)
- Modify: `internal/runner/runner.go` (`pick`, `step`, `Snapshot`, idle waits)
- Modify: `internal/runner/watch.go` (start/tick/stop without a plan target)
- Modify: `internal/runner/pubsub.go` (stream-down on the forced channel)
- Modify: `internal/runner/claim.go` (ready-drop claim while force watching)
- Create: `internal/runner/force_test.go`

**Interfaces:**
- Consumes: `platform.Stream`, `platform.ReadyDropLister` (Task 2)
- Produces:
  ```go
  const StateForceWatching State = "force_watching"
  // Config additions:
  ForceChannels func() []string // this account's forced logins, panel order; read live; nil = none
  ForceLookup   func(ctx context.Context, login string) (platform.Stream, bool, error) // nil = feature off
  ```
  The Snapshot for this state has `State=force_watching`, `Channel=<login>`, `Current=nil`.

- [ ] **Step 1: Write failing tests** in `force_test.go`. Use `platformtest` fakes and the helpers in `runner_test.go`. Build a fake ForceLookup from a map of login → live, with an error option.
  - `TestForceWatch_StartsWhenQueueEmpty`: empty campaigns; channels `[a, b]` with a offline and b live. The state becomes `force_watching` on `b`, and Heartbeat is called on the tick.
  - `TestForceWatch_DropWins`: while force watching b, the fake starts returning a campaign with a drop. Expect StopWatch on b and state starting/watching on the drop's channel within one rescan (`recheckInterval`).
  - `TestForceWatch_SwitchesToHigherRankAndOffline`:
    - while watching b, `a` goes live, so after the recheck the runner watches `a`;
    - then `a` and `b` both go offline, so the state is `sleeping` with NextScanAt ≈ now + 5 min.
  - `TestForceWatch_NoGamesStillWatches`: `cfg.Games` empty and ForceChannels `[b]` with b live → `force_watching`, not `no_games`. With ForceChannels empty → `no_games` as today.
  - `TestForceWatch_LookupErrorSkipsChannel`: a's lookup errors and b is live → it watches b with no panic.
  - `TestForceWatch_StreamDownSwitches`: the PubSub stream-down hook for the forced channel ID → re-pick at once.
  - `TestForceWatch_ClaimsReadyDrops`: a fake backend implementing `ReadyDropLister` returns one drop during force watch. Expect Claim called once and a ClaimLog row. Kick: `SweepCompletedClaims` still runs in the force tick (assert the fake's sweep counter).
  - `TestForceWatch_RescanEndsIt`: `Rescan()` or `MineNow` while force watching → StopWatch, then picking.
- [ ] **Step 2: Run** `go test ./internal/runner -run ForceWatch`. Expected: FAIL.
- [ ] **Step 3: Implement.**
  - `pick`:
    - Guard: if `len(cfg.Games)==0` and no force channels → `no_games`. If no games but there are force channels, skip the scan and try force watch.
    - When the queue is empty (where it now sets sleeping or awaiting), call `startForce`. It walks `ForceChannels()` in order and takes the first `ForceLookup` with live=true.
    - On success, call `Backend.StartWatch` (and `SubscribeChannel` as `start` does), then set `force_watching`. Otherwise keep today's sleeping or awaiting branch, using the `sleepNoStream` 5-minute recheck when force channels exist.
  - Force tick:
    - Send a Heartbeat every tick. Every `recheckInterval` (5 min), run `scan`. A non-empty Queue → `stopWatch` and `StatePicking`.
    - Otherwise re-walk the force channels. If the best live channel differs from the current one, switch.
    - Kick: call the completed-claim sweep the way `tick` does. If the backend is a `ReadyDropLister`, claim each returned drop through the existing claim path. Record it in Claims and notify as a normal claim does.
  - `tick` must not be reached with `current==nil` in this state. Give it a separate `forceTick`, or branch at the top.
  - Logs: INFO `runner force watching` with `account` and `channel` on start and switch. DEBUG for rechecks.
- [ ] **Step 4: Run** `go test ./internal/runner/...` (the whole package, so existing tests stay green). Expected: PASS.
- [ ] **Step 5: Stage.**

---

### Task 5: API and daemon wiring

**Files:**
- Create: `internal/api/force_watch.go`, `internal/api/force_watch_test.go`
- Modify: `internal/api/server.go` (Deps fields, route), `internal/api/events.go` (`EventForceWatch = "force_watch"`), `internal/api/overview.go` (+ test)
- Modify: `cmd/miner/main.go`, `cmd/miner/roster.go`, plus a roster test if one covers `runnerConfig`

**Interfaces:**
- Consumes: `store.ForceWatch` (Task 1), `forcewatch.Checker` (Task 3), `runner.StateForceWatching` and the Config fields (Task 4)
- Produces (JSON):
  ```
  GET /api/force-watch -> {"channels":[{"platform":"twitch","channel":"keshaeuw","live":true|false|null,
                            "accounts":["acc_1"],"watching":["acc_1"]}]}
  PUT /api/force-watch  body {"channels":[{"platform","channel","accounts"}]} -> 200 same shape as GET | 400 {"error":"bad_request"}
  GET /api/overview gains "force_watch": null | {"live":[{"platform","channel","watchers":["<account name>"]}],"more":N,"total":M}
  ```
  `watching` and `watchers` come from `Pool.Snapshots()` where State==`force_watching` and Channel matches (and the platform matches). Overview `live`:
  - live channels only;
  - those with watchers first, then panel order;
  - at most 2;
  - `more` = live count − shown;
  - `total` = list length.

  `force_watch` is `null` when the list is empty.
- Deps: `ForceWatch *store.ForceWatch`, `ForceLive func(platform, login string) *bool` (nil-safe), `ForceChanged func()` (nil-safe).

- [ ] **Step 1: Write failing tests** with the existing api test server helpers.
  - `TestGetForceWatch`: list plus live values from a stub `ForceLive`, one of them nil → JSON `null`, plus `watching` from a stub pool snapshot.
  - `TestPutForceWatch_ReplacesAndNotifies`:
    - a valid body → 200;
    - the store contains the new list;
    - `ForceChanged` is called once;
    - the hub publishes `force_watch`.
  - `TestPutForceWatch_RejectsPlatformMismatchAndDuplicate`: each case → 400 with the list unchanged. Also reject a bad name (`"a"`), an unknown platform, and a missing CSRF token (use the existing CSRF test pattern).
  - `TestOverview_ForceWatch`:
    - an empty list → `"force_watch":null`;
    - 4 live channels where the 3rd is watched → `live` = [3rd, 1st], `more`=2;
    - none live → `live`=[] and `total`=N.
- [ ] **Step 2: Run** `go test ./internal/api -run 'ForceWatch|Overview_Force'`. Expected: FAIL.
- [ ] **Step 3: Implement** the handlers and the overview block.
- [ ] **Step 4: Wire it up in `cmd/miner`.**
  - Build the `ForceWatch` store and a `forcewatch.Checker` with ttl `2*time.Minute`.
  - The Checker's LookupFunc picks any enabled account with a session on that platform, using the session the roster already loaded. It type-asserts the platform's backend to `platform.ChannelLooker`. With no account → error (Peek stays unknown).
  - Start `checker.Run(ctx, list, 2*time.Minute)`.
  - In `runnerConfig`:
    - `ForceChannels` reads a roster cache of `ForceWatch.List`, refreshed on `ForceChanged`;
    - `ForceLookup` = `func(ctx, login) { return checker.Lookup(ctx, a.Platform, login) }`.
  - `ForceChanged`:
    - refresh the cache;
    - reload each runner whose account was added or removed, plus every runner currently `force_watching` (reuse `reloadAccount`);
    - return at once, running the reload in the background like `GamesChanged`.
  - Also make sure the roster builds a real runner, not an idle `no_games` entry, for an account with no games but force channels. Check `builder`/`idle` in `roster.go`.
- [ ] **Step 5: Run** `gofmt -w . && go build ./... && go test ./...`. Expected: all PASS.
- [ ] **Step 6: Stage.**

---

### Task 6: SPA, Games panel and Home tile

**Files:**
- Modify: `web/src/lib/api/types.ts`, `web/src/lib/api/client.ts` (+ `client.test.ts`)
- Create: `web/src/lib/force-watch.ts` (+ `force-watch.test.ts`): `parseChannel`, plus the reorder and move helpers
- Create: `web/src/routes/(app)/games/ForceWatch.svelte` (the panel and dialog, ported from `sandbox/force-watch/_Mock.svelte` variant A)
- Modify: `web/src/routes/(app)/games/+page.svelte` (render `<ForceWatch>` in the rail under `AccountLists`)
- Modify: `web/src/routes/(app)/+page.svelte` (the Home KPI tile in the 3rd slot)
- Modify: the live-context/SSE handler that maps events to refreshes (refresh on `force_watch`)
- Modify: the three locale files; `docs/CHANGELOG.md`

**Interfaces:**
- Consumes: the Task 5 JSON.
- Produces:
  ```ts
  type ForceChannel = { platform: Platform; channel: string; live: boolean | null; accounts: string[]; watching: string[] };
  type OverviewForceWatch = { live: { platform: Platform; channel: string; watchers: string[] }[]; more: number; total: number } | null;
  api.forceWatch(): Promise<{ channels: ForceChannel[] }>
  api.putForceWatch(channels: { platform: Platform; channel: string; accounts: string[] }[]): Promise<{ channels: ForceChannel[] }>
  parseChannel(raw: string): { platform: Platform; name: string } | null   // same as sandbox _variants.ts
  ```
- UI copy (en). The keys are yours to name under `force.*` and `home.kpi_force*`:
  - "Force watch", "Watched when no drops.", "Add channel"
  - "Channel link (twitch.tv / kick.com)"
  - "Paste a twitch.tv or kick.com channel link.", "Already in the list."
  - "Twitch channel" / "Kick channel"
  - "Accounts that watch it when idle", "No Twitch accounts yet." / "No Kick accounts yet."
  - "Cancel", "Add", "Save", "Remove {name}?", "Remove", "No channels yet."
  - "Move up", "Move down", "Edit accounts", "Live", "Offline"
  - Home: "Force watch", "Nobody watching", "{n} more live", "None live", "{n} channels waiting" (plural via `tn`)
  - State label for `force_watching`: "Force watching".

- [ ] **Step 1: Write failing vitest tests.**
  - `force-watch.test.ts`:
    - `parseChannel('https://www.twitch.tv/keshaeuw')` → `{twitch, keshaeuw}`;
    - `'kick.com/XQC'` → `{kick, xqc}`;
    - `'m.twitch.tv/a'` → null (too short);
    - `'youtube.com/x'` → null;
    - `''` → null.
  - `client.test.ts`: `forceWatch` GETs `/api/force-watch`; `putForceWatch` PUTs the body with the CSRF header, the same way the existing PUT tests do.
- [ ] **Step 2: Run** `cd web && npx vitest run src/lib/force-watch.test.ts src/lib/api/client.test.ts`. Expected: FAIL.
- [ ] **Step 3: Implement** `force-watch.ts`, the types and the client.
- [ ] **Step 4: Build `ForceWatch.svelte`** to match sandbox variant A exactly:
  - a purple `+` in the top right of the header;
  - the "Watched when no drops." hint;
  - rows: rank, PlatformMark, name, live dot (`live===null` → no dot), accounts with watching ones in green;
  - tools: ↑ ↓ ✎ ×, with × opening the inline confirm;
  - the dialog uses `<dialog class="dlg">` like `AccountLists.svelte`, with no accounts preselected on add and Add disabled until one is picked.
  - Each change calls `putForceWatch` with the full ordered list and shows a toast on error, the way the Games page does.
  - Accounts come from the data the Games page already loads.
- [ ] **Step 5: Add the Home tile** in the 3rd KPI slot, only when `overview.force_watch` is not null. It matches the sandbox `home-force-watch` variant A layout and links to `/games`. Use a 4-column grid only when the tile shows (`1fr 1fr 1.3fr 1fr`), and 2×2 at phone width.
- [ ] **Step 6: Add the CHANGELOG entry** under `[Unreleased]` → Added: **Force watch**. Idle accounts watch a channel you pick from Games, a drop always comes first, and live channels show on Home.
- [ ] **Step 7: Run** `cd web && npm run check && npx vitest run && npm run build`, then `go test ./internal/i18n`. Expected: all PASS.
- [ ] **Step 8: Stage.**

---

### Task 7: Staging and live verification (controller, not a subagent)

- [ ] Build and deploy to staging with the staging recipe (local-build tar, `compose.verify-cd.yml`-style override with a new image tag).
- [ ] Visual check through the socat + ssh tunnel with `playwright-cli`: add a Twitch channel, assign one account, see the live dot, see the Home tile, check the confirm on remove, and check 390px width.
- [ ] Live check, required before any tag:
  - pick an account with an empty queue and a live forced channel;
  - confirm the log line `runner force watching` and minutes accruing (Spade beacon 204s at DEBUG);
  - then confirm it leaves force watch when a drop is queued, if one is available.
  - Report the result to the user. No tag without it.
- [ ] Tear staging down afterwards.
