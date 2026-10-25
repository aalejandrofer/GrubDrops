# v2 Plan 1: Planner + Runner Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace `internal/watcher` + `internal/scheduler` with a pure planner (`internal/plan`) and a per-account runner plus supervisor pool (`internal/runner`). Both are unit-tested against a scriptable fake backend, and nothing is wired into `cmd/miner` yet.

**Architecture:** `plan.Build` is a pure function. It takes chosen games, discovered campaigns, inventory, the claim log and skips, and returns one account's ordered drop queue plus the drops it can't mine and why. `runner.Runner` asks the planner for a queue, then executes the head of it. It finds a channel, beacons every 60 s, polls inventory, and claims. It keeps every 1.x mining safeguard: vanish, never-enrolled, freeze and channel cooldown, claim-failure skip, Kick multi-reward sweep, the reward reaper and PubSub hooks. `runner.Pool` supervises one runner per account and owns the session-only Up next order.

**Tech Stack:** Go (module `github.com/aalejandrofer/grubdrops`), standard library only, `log/slog`.

**Spec:** `docs/superpowers/specs/2026-10-25-grubdrops-v2-rebuild-design.md`

This is plan 1 of 5 for v2. The others are: 2 discovery + collection, 3 JSON API + SSE + `cmd/miner` rewiring, 4 SvelteKit UI, 5 migration + release. `internal/watcher` and `internal/scheduler` stay in the tree, untouched, until plan 3 rewires `cmd/miner` and deletes them along with the HTMX API.

## Global Constraints

- Branch `rebuild/v2`. Run `gofmt -w .`, `go build ./...` and `go test ./...` green before every commit.
- Every account earns every drop: one queue per account. No cross-account deduplication.
- Twitch credit beacon: one `Backend.Heartbeat` per 60 s while watching (`DefaultTick = 60 * time.Second`). Never slower.
- Never add a `Client-Integrity` header. Never touch `internal/platform/twitch` or `internal/platform/kick` in this plan.
- Keep the 1.x safeguard values verbatim: `vanishThreshold = 3`, `neverEnrolledThreshold = 6` (1.x `synthSkipThreshold`), `freezeThreshold = 5`, `stalledChannelCooldown = 30 * time.Minute`, `claimFailSkipThreshold = 3`, `recheckInterval = 5 * time.Minute`, `stepErrBackoff = 5 * time.Second` doubling to `maxBackoff = 5 * time.Minute`, `liveCheckEvery = 5 * time.Minute`, `maxProbe = 5`.
- Log lines keep the 1.x `slog` attribute `"kind"` with values `state`, `discovery`, `progress`, `claim`, `heartbeat`, `error`, `auth`, plus `"account"`. Plan 3's Health log and KPI counters read them from the log ring.
- Notification events and field names stay as in 1.x: `"claim"`, `"progress"`, `"state"`. Fields: `account`, `account_label`, `platform`, `game`, `campaign`, `drop`, `image`, `channel`, `cur_min`, `req_min`.
- Dropped and not ported: force-watch, priority modes (`ending_soonest`, `low_avbl_first`), exclude-game, per-account game lists, channel whitelists (null-game Kick campaigns), manual mark-collected, claim pruning (plan 2 reconciles from inventory), campaign persistence (plan 2).
- Any change to `docs/CHANGELOG.md` goes under `## [Unreleased]`.

## Calls made in this plan (check these)

1. **Restricted-first applies within a game, not across games.** In 1.x, channel-restricted campaigns jumped ahead of the game ranking. The spec says "the top game mines first", so v2 sorts by game rank first, and restricted campaigns come first only within the same game. Kick's passive accrual on open campaigns only happens within the same category, so nothing that 1.x earned through cross-game ordering is lost.
2. **No preemption.** The runner finishes or rotates off the current drop before taking the next queue head, as 1.x did. Reordering Up next changes what comes after the current drop.
3. **"I've linked it" override is kept as planner input** (`Input.ForceLinked`). Kick `connect_url` campaigns otherwise deadlock (they return 403 on progress until something is earned). Plan 4 decides where its toggle lives.
4. **Heartbeat and inventory poll run every tick, and the tick is fixed at 60 s.** The 1.x `tick_interval_sec` setting is gone. Only the live-check cadence stays separate.
5. **The runner stops its watch on exit** (context cancel). 1.x returned without stopping, so a reload could leave a Kick presence loop running.

## Review Focus

1. **Account with no chosen games:** expect no backend calls and state `no_games`, not a scan loop. Test: `TestRunner_NoGamesDoesNotScan` (Task 3).
2. **Up next order naming a drop that is gone** (claimed or expired): expect it ignored, the rest in default order, no panic. Test: `TestApplyOrder_UnknownIDsIgnored` (Task 2).
3. **Game name case or slug differs from the chosen game** ("rust", "RUST", slug "rust"): expect it still matches. Test: `TestBuild_GameMatchLenient` (Task 2).
4. **Same benefit ID in two campaigns** (multi-tier or new-season reuse): expect it queued once, and claiming it doesn't strand the other tiers. Test: `TestBuild_DuplicateBenefitQueuedOnce` (Task 2).
5. **Reload or shutdown while watching:** expect `StopWatch` called so the Kick sidecar slot frees. Test: `TestRunner_CancelStopsWatch` (Task 4).

---

## File Structure

| File | Responsibility |
|---|---|
| `internal/platform/platformtest/fake.go` | Scriptable in-memory `platform.Backend` + optional-capability wrappers for tests |
| `internal/plan/plan.go` | Types (`Game`, `Input`, `Target`, `Blocked`, `Result`), `Build`, `ApplyOrder`, `Match` |
| `internal/plan/plan_test.go` | Planner tests |
| `internal/runner/state.go` | `State` constants |
| `internal/runner/runner.go` | `Config`, `Runner`, `New`, `Run`, `step`, `pick`, `Snapshot`, notify helpers |
| `internal/runner/watch.go` | `start` (channel pick) and `tick` (beacon, live check, inventory poll, safeguards) |
| `internal/runner/claim.go` | `claim`, Kick sweep notify, reward reaper |
| `internal/runner/pubsub.go` | PubSub hook handlers |
| `internal/runner/pool.go` | `Pool`, `Entry`, `Builder`, `Unit`, `Idle`, session order |
| `internal/runner/*_test.go` | Tests per file, `package runner` (white-box: tests call `r.step(ctx)` directly) |

---

### Task 1: Scriptable fake backend

**Files:**
- Create: `internal/platform/platformtest/fake.go`
- Test: `internal/platform/platformtest/fake_test.go`

**Interfaces:**
- Produces:
  ```go
  type Fake struct {
      Mu        sync.Mutex
      Campaigns []platform.Campaign
      Streams   map[string][]platform.Stream     // campaign ID -> live streams
      Progress  map[string]*platform.Progress    // benefit ID -> inventory row; absent = not in inventory
      OnHeartbeat func(f *Fake)                  // called with Mu held; nil = +1 minute to every unclaimed row
      ListErr, InventoryErr, StartErr, HeartbeatErr, ClaimErr error
      Calls     []string                         // "List", "Inventory", "Channels:<campID>", "Start:<channel>", "Heartbeat:<channel>", "Stop:<channel>", "Claim:<benefitID>"
  }
  func NewFake() *Fake
  func (f *Fake) CallCount(prefix string) int
  // Fake implements platform.Backend. Name() returns "fake". Claim sets Progress[id].Claimed = true (creating the row) unless ClaimErr.

  type PubSubFake struct{ *Fake; Hooks map[string]platform.PubSubHooks }   // implements platform.PubSubAware
  type SweepFake  struct{ *Fake; Sweep func() []platform.ClaimedReward }  // implements platform.CompletedSweeper
  type RewardFake struct{ *Fake; Rewards []platform.ClaimedReward; RewardCalls [][]string } // implements platform.RewardClaimer, records allowedGames per call
  type ProbeFake  struct{ *Fake; Available map[string]map[string]struct{} } // implements platform.AvailableDropsChecker keyed by channel ID
  ```

- [ ] **Step 1: Write the failing test** `TestFake_HeartbeatAccruesAndClaimMarks`: seed `Progress["d1"] = &platform.Progress{BenefitID: "d1"}`. Call `StartWatch` with `Stream{Channel: "c"}`, two `Heartbeat`s, then `Claim(DropBenefit{ID: "d1"})`. Assert `InventoryProgress` returns `MinutesWatched == 2, Claimed == true`, `CallCount("Heartbeat:") == 2`, and `Calls[0] == "Start:c"`.
- [ ] **Step 2: Run** `go test ./internal/platform/platformtest/ -run TestFake -v`. Expected: FAIL (undefined: NewFake).
- [ ] **Step 3: Implement `fake.go`** per the Interfaces block. `InventoryProgress` returns copies of rows sorted by `BenefitID` (deterministic). Every method takes `Mu`.
- [ ] **Step 4: Run** the same command. Expected: PASS.
- [ ] **Step 5: Commit** `git add internal/platform/platformtest && git commit -m "test: scriptable platformtest.Fake backend for v2 runner"`

---

### Task 2: Planner

**Files:**
- Create: `internal/plan/plan.go`
- Test: `internal/plan/plan_test.go`

**Interfaces:**
- Consumes: `platform.Campaign`, `platform.DropBenefit`, `platform.Progress`.
- Produces:
  ```go
  type Game struct{ Name, Slug string }
  type Input struct {
      Games       []Game                // chosen games, priority order (index 0 = top)
      Campaigns   []platform.Campaign
      Progress    []platform.Progress
      Claimed     map[string]bool       // benefit IDs in GrubDrops' claim log for this account
      Skipped     map[string]bool       // benefit IDs the runner gave up on
      ForceLinked map[string]bool       // campaign IDs the user asserted are linked
      Order       []string              // session Up next override, benefit IDs
  }
  type Target struct {
      Campaign platform.Campaign
      Benefit  platform.DropBenefit
      Minutes  int                      // minutes watched per inventory, 0 if absent
  }
  type Reason string
  const (
      ReasonNotLinked    Reason = "not_linked"
      ReasonPrecondition Reason = "precondition"
      ReasonSkipped      Reason = "skipped"
  )
  type Blocked struct{ Target; Reason Reason }
  type Result struct{ Queue []Target; Blocked []Blocked }
  func Build(in Input) Result
  func ApplyOrder(q []Target, order []string) []Target
  func Match(games []Game, game string) (rank int, ok bool)
  ```

**Rules for `Build`** (in this order):
1. Keep a campaign only if `Match(in.Games, c.Game)` succeeds, `c.Status` is `""` or `"active"`, and `c.Kind != "reward"`. Otherwise drop it silently.
2. Unlinked: if `(c.Platform == "twitch" || c.AccountLinkChecked) && !c.AccountLinked && !in.ForceLinked[c.ID]`, every minable benefit of `c` becomes `Blocked{ReasonNotLinked}`.
3. Per benefit:
   - Drop silently if `RequiredMinutes <= 0`, or claimed (inventory `Claimed` or `in.Claimed[id]`), or a duplicate of a benefit ID already seen.
   - If `in.Skipped[id]`, it becomes `Blocked{ReasonSkipped}`.
   - If any `Preconditions` ID is not claimed (inventory ∪ `in.Claimed`), it becomes `Blocked{ReasonPrecondition}`.
4. Sort campaigns (stable) by:
   1. game rank ascending
   2. restricted first (`len(AllowedChannels) > 0`)
   3. `campaignMinRemaining` ascending: the minimum over the campaign's benefits of `max(0, RequiredMinutes - minutes watched)`
5. Within a campaign, benefits by `RequiredMinutes` ascending (stable).
6. `Queue = ApplyOrder(sorted targets, in.Order)`.

`ApplyOrder` puts targets whose `Benefit.ID` appears in `order` first, in `order`'s sequence, then the rest in their incoming order. IDs not in `q` are ignored. `Match` compares `strings.ToLower(game)` with lowercased `Name` and with `Slug`, and returns the index.

- [ ] **Step 1: Write the failing tests:**
  ```go
  func TestBuild_GameRankThenRestrictedThenRemaining(t *testing.T)
  // games [Rust, Overwatch]. Campaigns:
  //   ow-open (Overwatch, no AllowedChannels)
  //   rust-open (Rust, d1 60 min, progress 50)
  //   rust-team (Rust, AllowedChannels ["x"], d2 120 min)
  // want Queue benefit IDs: [d2, d1, <ow benefit>]
  func TestBuild_BenefitsByRequiredMinutes(t *testing.T)     // one campaign with 120,30,60 -> 30,60,120
  func TestBuild_DropsClaimedZeroMinuteRewardAndInactive(t *testing.T)
  // claimed via inventory, claimed via in.Claimed, RequiredMinutes 0, Kind "reward",
  // and Status "upcoming" all absent from Queue and Blocked
  func TestBuild_BlockedReasons(t *testing.T)
  // twitch campaign AccountLinked=false -> ReasonNotLinked
  // same campaign with ForceLinked[id] -> in Queue
  // kick campaign AccountLinkChecked=false, AccountLinked=false -> in Queue
  // Skipped -> ReasonSkipped
  // Preconditions ["p"] unclaimed -> ReasonPrecondition; with Claimed["p"] -> in Queue
  func TestBuild_GameMatchLenient(t *testing.T)               // Game{Name:"Rust",Slug:"rust"} matches "rust", "RUST", "Rust"; "Rusty" no match
  func TestBuild_DuplicateBenefitQueuedOnce(t *testing.T)     // d1 in two campaigns of the same game -> appears once in Queue
  func TestApplyOrder_MovesNamedFirst(t *testing.T)          // q [a,b,c], order [c,a] -> [c,a,b]
  func TestApplyOrder_UnknownIDsIgnored(t *testing.T)        // q [a,b], order [zz,b] -> [b,a]
  func TestBuild_TargetMinutesFromInventory(t *testing.T)    // progress d1=17 -> Queue[0].Minutes == 17
  ```
- [ ] **Step 2: Run** `go test ./internal/plan/ -v`. Expected: FAIL (package has no non-test files / undefined: Build).
- [ ] **Step 3: Implement `plan.go`** per the rules above.
- [ ] **Step 4: Run** `go test ./internal/plan/ -v`. Expected: PASS, all 9 tests.
- [ ] **Step 5: Commit** `git add internal/plan && git commit -m "feat(plan): pure per-account drop planner for v2"`

---

### Task 3: Runner core: config, states, pick, idle, backoff

**Files:**
- Create: `internal/runner/state.go`, `internal/runner/runner.go`
- Test: `internal/runner/runner_test.go`

**Interfaces:**
- Consumes: `plan.Build`, `plan.ApplyOrder`, `plan.Match`, `plan.Game`, `plan.Target`, `plan.Blocked` (Task 2); `platformtest.Fake` (Task 1).
- Produces:
  ```go
  type State string
  const (
      StateIdle            State = "idle"
      StatePicking         State = "picking"
      StateStarting        State = "starting"   // finding a live channel
      StateWatching        State = "watching"
      StateClaiming        State = "claiming"
      StateSleeping        State = "sleeping"
      StateAwaitingConnect State = "awaiting_connect"
      StateNeedsLogin      State = "needs_login"
      StateNoGames         State = "no_games"
  )
  type Notifier interface{ Notify(ctx context.Context, event string, fields map[string]any) error }
  type ClaimLog interface {
      RecordClaim(ctx context.Context, accountID string, b platform.DropBenefit) error
      RecordClaimWithCode(ctx context.Context, accountID string, b platform.DropBenefit, code string) error
      ClaimedBenefitIDs(ctx context.Context, accountID string) (map[string]bool, error)
  } // *store.ClaimRecorder already satisfies this
  type SkipStore interface {
      Skips(accountID string) (map[string]bool, error)
      RecordSkip(accountID, benefitID string) error
      ClearSkip(accountID, benefitID string) error
  }
  type Config struct {
      AccountID, AccountLabel, Platform string
      Backend     platform.Backend
      Session     platform.Session
      Games       []plan.Game
      ForceLinked map[string]bool
      Notifier    Notifier                 // nil-safe
      Claims      ClaimLog                 // nil-safe
      Skips       SkipStore                // nil-safe
      Order       func() []string          // nil = no override
      OnInventory func(ctx context.Context, accountID string, camps []platform.Campaign, progress []platform.Progress) // nil-safe; plan 2 reconciles here
      OnChange    func(Snapshot)           // nil-safe; called after every state change and progress update
      TickInterval          time.Duration  // <=0 -> DefaultTick
      ProgressNotifyStepPct int            // 0 disables progress notifications
  }
  const DefaultTick = 60 * time.Second
  type Snapshot struct {
      AccountID, Platform string
      State           State
      Current         *plan.Target
      Channel         string
      ViewerCount     int
      MinutesWatched  int
      StartedAt, LastPollAt, LastScanAt time.Time
      UpNext          []plan.Target      // queue minus Current, with Config.Order applied at read time
      Blocked         []plan.Blocked
  }
  func New(cfg Config) *Runner
  func (r *Runner) Run(ctx context.Context) error
  func (r *Runner) Snapshot() Snapshot
  func (r *Runner) step(ctx context.Context) error   // unexported, used by tests
  var errIdle = errors.New("idle; recheck later")
  var errStop = errors.New("stopped; needs user action") // Run returns nil on it
  ```
- `New` sets `Session.AccountID`, `Session.Games` (game names, only if nil) and `Session.GameFilter` (a `plan.Match` closure, only if nil). It loads `Skips.Skips(AccountID)` into the in-memory skip set.
- `step` dispatches on state:
  - `idle`, `picking`: `pick`
  - `starting`: `start` (Task 4)
  - `watching`: `tick` (Task 4)
  - `claiming`: `claim` (Task 5)
  - `sleeping`, `awaiting_connect`: return `errIdle`
  - `needs_login`, `no_games`: return `errStop`, a sentinel that makes `Run` return nil
- `pick`, in order:
  1. If `len(Games) == 0`, set `no_games` and return.
  2. `ListActiveCampaigns`. On `platform.ErrIntegrityBlocked`, set `needs_login` with a `"kind","auth"` log.
  3. `InventoryProgress`, only when there are campaigns. On error, use empty progress and treat it as not OK.
  4. When inventory is OK, run the self-heal: clear the skip for any tracked, unclaimed benefit that is in the skip set. The exception is a claim-failure skip whose minutes are still at or above required; it stays skipped.
  5. Call `OnInventory`.
  6. Run the reward reaper (Task 5 fills it in; Task 3 leaves a no-op call).
  7. `Claims.ClaimedBenefitIDs`.
  8. `plan.Build`, storing the result and `LastScanAt`.
  9. Take the first `Queue` target whose campaign is not in `noStream`, and set `starting`.
  10. If there is none, clear `noStream`. Set `awaiting_connect` if the queue is empty and at least one `ReasonNotLinked` exists, else `sleeping`.
- `Run` copies the 1.x loop:
  - The ticker uses `TickInterval`.
  - On `errIdle` it waits `recheckInterval` (a package `var`, so tests can shrink it), then sets `picking`.
  - On any other error it stops the current watch, sets `picking`, and backs off (5 s doubling to 5 min, reset to 0 on a successful step).
  - On `ctx.Done()` it stops the current watch with a fresh `context.WithTimeout(context.Background(), 10*time.Second)` and returns `ctx.Err()`.
- Every state change logs `"kind","state"` and notifies `"state"` with `{account, state}`, as in 1.x `setState`.

- [ ] **Step 1: Write the failing tests:**
  ```go
  func TestRunner_NoGamesDoesNotScan(t *testing.T)        // Games nil -> after step: State()==StateNoGames, f.CallCount("List")==0
  func TestRunner_PickSetsStartingWithQueueHead(t *testing.T) // Fake with Rust campaign d1 -> State StateStarting, Snapshot().Current.Benefit.ID=="d1"
  func TestRunner_IntegrityBlockedNeedsLogin(t *testing.T) // ListErr = fmt.Errorf("x: %w", platform.ErrIntegrityBlocked) -> StateNeedsLogin; next step returns errStop
  func TestRunner_NothingMinableSleeps(t *testing.T)      // all claimed -> StateSleeping; step returns errIdle
  func TestRunner_OnlyUnlinkedAwaitsConnect(t *testing.T) // twitch campaign AccountLinked=false -> StateAwaitingConnect; Snapshot().Blocked[0].Reason==plan.ReasonNotLinked
  func TestRunner_ClaimLogExcludesBenefit(t *testing.T)   // Claims.ClaimedBenefitIDs returns {d1:true}; d2 next -> Current d2
  func TestRunner_GhostSkipSelfHeals(t *testing.T)        // Skips has d1; inventory has d1 unclaimed -> ClearSkip("acc","d1") called and Current d1
  func TestRunner_UpNextAppliesSessionOrder(t *testing.T) // queue d1,d2,d3; Order returns [d3] -> Snapshot().UpNext IDs == [d3, d2]
  func TestRunner_SessionGamesAndFilterPlumbed(t *testing.T) // Games [{Rust,rust}] -> cfg.Session seen by backend has Games==["Rust"], GameFilter("RUST")==true
  func TestRunner_RunBacksOffAndRecovers(t *testing.T)    // ListErr once then nil; TickInterval 1ms; stepErrBackoff=1ms; Run in goroutine until State()==StateStarting, then cancel
  ```
- [ ] **Step 2: Run** `go test ./internal/runner/ -v`. Expected: FAIL (undefined: New).
- [ ] **Step 3: Implement `state.go` and `runner.go`** per the Interfaces block. `start`, `tick` and `claim` are stubs that return `errors.New("not implemented")` until Tasks 4 and 5. The snapshot helper copies under the mutex, and `OnChange` is called outside the mutex.
- [ ] **Step 4: Run** `go test ./internal/runner/ -v`. Expected: PASS, all 10 tests.
- [ ] **Step 5: Commit** `git add internal/runner && git commit -m "feat(runner): v2 per-account runner core (pick, idle, backoff)"`

---

### Task 4: Watching: channel pick, beacon, live check, safeguards

**Files:**
- Create: `internal/runner/watch.go`
- Test: `internal/runner/watch_test.go`

**Interfaces:**
- Consumes: Task 3 `Runner` internals, `platformtest.Fake`, `platformtest.ProbeFake`.
- Produces: `func (r *Runner) start(ctx context.Context) error`, `func (r *Runner) tick(ctx context.Context) error`, `func (r *Runner) stopWatch(ctx context.Context)`. Task 5 relies on the state after a complete poll: `claiming` with `Current.Benefit.InstanceID` set from inventory.

**`start` (ported from 1.x `pickStream`):**
1. `ListEligibleChannels` for the current campaign. Drop channels still on cooldown in `stalled`, pruning expired entries first.
2. If none remain, add the campaign ID to `noStream`, log `"kind","discovery"`, and set `picking`.
3. Sort by `ViewerCount` descending (stable).
4. If the backend is an `AvailableDropsChecker`, probe at most `maxProbe = 5` candidates that have a `ChannelID`:
   - accept the first whose set is empty (unknown) or contains the benefit ID
   - on a probe error, stop probing and keep the head
5. `StartWatch`. Reset the per-watch counters (`tickN`, `noProgress`, `noAdvance`, `lastMin = 0`). Reset the progress milestone only if the benefit ID changed.
6. If the backend is a `ChannelSubscriber`, subscribe the `ChannelID`. Set `watching`.

**`tick` (ported from 1.x `tickWatch`, with heartbeat and poll on every tick):**
1. `tickN++`. `Heartbeat`; on error return it. Log `"kind","heartbeat"`.
2. Every `max(1, liveCheckEvery/TickInterval)` ticks, call `ListEligibleChannels`. If the current channel is absent, stop the watch and set `starting`. A list error is ignored.
3. `InventoryProgress` (on error return it), then set `LastPollAt`.
4. If the backend is a `CompletedSweeper`, call `SweepCompletedClaims` and run each result through `notifySwept` (Task 5).
5. If a row matches the current benefit:
   - Set `noAdvance` to 0 if minutes rose, else increment it. Set `lastMin`, set `noProgress = 0`, and copy `InstanceID`.
   - If minutes are at or above required, fire the 100 % milestone and set `claiming`.
   - Otherwise, if `noAdvance >= freezeThreshold`, stop the watch, put the channel on cooldown until `now + stalledChannelCooldown`, and set `starting`. The target is kept.
6. If no row matches:
   - Increment `noProgress`.
   - If `noProgress >= vanishThreshold && lastMin > 0`, skip the benefit (memory + `Skips.RecordSkip`), stop the watch and set `picking`.
   - If `noProgress >= neverEnrolledThreshold && lastMin == 0`, do the same.
7. Fire the progress milestone notification (`shouldNotifyProgress`, ported verbatim: 0 %, each step %, then 100 %, each once per benefit).

- [ ] **Step 1: Write the failing tests:**
  ```go
  func TestWatch_PicksHighestViewers(t *testing.T)         // streams a(10), b(50) -> Calls contains "Start:b"
  func TestWatch_NoLiveStreamTriesNextCampaign(t *testing.T) // camp1 no streams, camp2 live -> after steps Current campaign == camp2
  func TestWatch_ProbeSkipsChannelWithoutDrop(t *testing.T) // ProbeFake: top channel serves other drop, second serves d1 -> Start on second
  func TestWatch_BeaconEveryTick(t *testing.T)             // 3 tick steps -> CallCount("Heartbeat:") == 3
  func TestWatch_CompleteGoesToClaiming(t *testing.T)      // progress reaches RequiredMinutes, InstanceID "i1" -> StateClaiming, Current.Benefit.InstanceID=="i1"
  func TestWatch_LiveCheckSwapsOfflineChannel(t *testing.T) // TickInterval = liveCheckEvery/2 (every 2nd tick); channel removed from Streams -> "Stop:<ch>" and StateStarting
  func TestWatch_FreezeRotatesAndCoolsDownChannel(t *testing.T) // OnHeartbeat no-op, row present at 5 -> after 5 polls Stop + StateStarting; next start skips that channel
  func TestWatch_FreezeNotTriggeredWhenAdvancing(t *testing.T) // default accrual, 10 polls -> no "Stop:"
  func TestWatch_VanishSkipsBenefit(t *testing.T)          // row present 2 polls then deleted 3 polls -> RecordSkip("acc","d1"), StatePicking
  func TestWatch_NeverEnrolledSkipsAfterSix(t *testing.T)  // no row ever -> after 6 polls RecordSkip, StatePicking; after 5 still watching
  func TestWatch_HeartbeatErrorStopsWatchOnRetry(t *testing.T) // HeartbeatErr set -> step returns err; Run path calls stopWatch -> "Stop:<ch>"
  func TestRunner_CancelStopsWatch(t *testing.T)           // Run until watching, cancel ctx -> Calls contains "Stop:<ch>"
  func TestShouldNotifyProgress_MilestoneSteps(t *testing.T) // port 1.x test verbatim (step 25: 0,25,50,75,100 once each)
  func TestShouldNotifyProgress_StepZeroDisables(t *testing.T) // port 1.x test verbatim
  ```
- [ ] **Step 2: Run** `go test ./internal/runner/ -run 'TestWatch|TestRunner_Cancel|TestShouldNotify' -v`. Expected: FAIL (not implemented).
- [ ] **Step 3: Implement `watch.go`** per the steps above. Port the safeguard code from `internal/watcher/watcher.go` (`pickStream`, `tickWatch`, `shouldNotifyProgress`, `stopCurrentWatch`, `unsubscribeCurrentChannel`) and rename the state fields to the runner's.
- [ ] **Step 4: Run** `go test ./internal/runner/ -v`. Expected: PASS (Task 3 + Task 4 tests).
- [ ] **Step 5: Commit** `git add internal/runner && git commit -m "feat(runner): watch loop with 1.x mining safeguards"`

---

### Task 5: Claim, Kick multi-reward sweep, reward reaper

**Files:**
- Create: `internal/runner/claim.go`
- Modify: `internal/runner/runner.go` (replace the reaper no-op call in `pick`)
- Test: `internal/runner/claim_test.go`

**Interfaces:**
- Consumes: Tasks 3 and 4; `platformtest.SweepFake`, `platformtest.RewardFake`.
- Produces: `func (r *Runner) claim(ctx context.Context) error`, `func (r *Runner) notifySwept(ctx context.Context, cr platform.ClaimedReward)`, `func (r *Runner) reapRewards(ctx context.Context, camps []platform.Campaign)`.

**`claim`:**
- `Backend.Claim(Current.Benefit)`.
- On error:
  - increment `claimFailures[id]`
  - at `claimFailSkipThreshold`, skip the benefit (memory + `RecordSkip`), record `claimFailSkipped[id] = RequiredMinutes` and log `"kind","claim"`
  - return the error
- On success:
  - clear `claimFailures[id]` and stop the watch
  - `Claims.RecordClaim` (a failure is logged, never retried)
  - if the backend is a `CurrentSessionChecker`, run it as a log-only probe, as in 1.x
  - notify `"claim"` with `drop`, `channel`, `cur_min = req_min = RequiredMinutes`
  - clear `Current` and set `picking`

**`notifySwept`:** ignore an empty title, a title equal to `Current.Benefit.Name`, and a title already notified (`notifiedSwept`). Otherwise notify `"claim"` with `drop = Title` and `game = Game` when set.

**`reapRewards`:** only when the backend is a `RewardClaimer`. Collect the chosen-game campaigns where `Kind == "reward"` or `len(Benefits) == 0`, deduplicate their `Game` names, then call `ClaimRewards(session, games)` once and log each result with `"kind","claim"`.

- [ ] **Step 1: Write the failing tests:**
  ```go
  func TestRunner_MinesUntilClaim(t *testing.T)            // d1 RequiredMinutes 2; steps until StatePicking after claim -> "Claim:d1" once, RecordClaim(acc,d1) once, notifier got "claim" with drop name
  func TestClaim_RepeatedFailureSkips(t *testing.T)        // ClaimErr permanent -> after 3 claim attempts RecordSkip(acc,d1); next pick picks d2
  func TestClaim_TransientFailureNotSkipped(t *testing.T)  // ClaimErr for 2 attempts then nil -> claimed, no RecordSkip
  func TestClaim_FailSkipKeptWhileStillComplete(t *testing.T) // after fail-skip, inventory row stays >= required + unclaimed -> next pick does NOT ClearSkip
  func TestClaim_MultiTierContinuesAfterFirstClaim(t *testing.T) // campaign d1(1),d2(3) -> d1 claimed then Current d2
  func TestSweep_NotifiesSiblingsOnceExcludingCurrent(t *testing.T) // SweepFake returns [{Title:"Sib"},{Title:<current name>}] on 2 polls -> exactly one "claim" notify for "Sib"
  func TestReaper_CallsOnceWithRewardGames(t *testing.T)   // RewardFake + chosen game campaign Kind "reward" -> RewardCalls == [["Rust"]]
  func TestReaper_SkippedWithoutRewardCampaigns(t *testing.T) // no reward campaigns -> len(RewardCalls)==0
  ```
- [ ] **Step 2: Run** `go test ./internal/runner/ -run 'TestRunner_MinesUntilClaim|TestClaim|TestSweep|TestReaper' -v`. Expected: FAIL.
- [ ] **Step 3: Implement `claim.go`** and wire `reapRewards(ctx, camps)` into `pick`, after `OnInventory` and before `Build`.
- [ ] **Step 4: Run** `go test ./internal/runner/ -v`. Expected: PASS (all runner tests).
- [ ] **Step 5: Commit** `git add internal/runner && git commit -m "feat(runner): claim flow, Kick sweep notify, reward reaper"`

---

### Task 6: PubSub hooks

**Files:**
- Create: `internal/runner/pubsub.go`
- Modify: `internal/runner/runner.go` (`New` registers hooks)
- Test: `internal/runner/pubsub_test.go`

**Interfaces:**
- Consumes: `platformtest.PubSubFake`, Task 3–5 internals.
- Produces: hook handlers `onDropProgress(dropID string, curMin, reqMin int64)`, `onDropClaim(dropID, instanceID string)`, `onStreamDown(channelID string)`, `onRewardCode(notificationID, code, body string)`. These are registered in `New` through `platform.PubSubAware.SetAccountPubSubHooks(AccountID, …)` before any backend call. `OnStreamUp` is a no-op.

Behaviour (ported from 1.x `handlePubSub*`). Each handler acts only when the ID matches the current benefit or stream:
- **progress:** sets `lastMin`, resets `noProgress`, resets `noAdvance` when minutes rose, then calls `OnChange`.
- **claim:** copies `instanceID` and sets `claiming`.
- **stream-down:** stops the watch, unsubscribes and sets `starting`.
- **reward code:** calls `Claims.RecordClaimWithCode(Current.Benefit, code)` and logs `"kind","claim"`.

- [ ] **Step 1: Write the failing tests:**
  ```go
  func TestPubSub_RegisteredBeforeFirstCall(t *testing.T) // PubSubFake: Hooks["acc"] set right after New, f.Calls empty
  func TestPubSub_ProgressUpdatesSnapshot(t *testing.T)  // watching d1; Hooks.OnDropProgress("d1",7,60) -> Snapshot().MinutesWatched==7; other id ignored
  func TestPubSub_ClaimEventMovesToClaiming(t *testing.T) // OnDropClaim("d1","inst") -> StateClaiming, InstanceID=="inst"
  func TestPubSub_StreamDownRestarts(t *testing.T)       // stream ChannelID "42"; OnStreamDown("42") -> "Stop:<ch>", StateStarting; OnStreamDown("99") ignored
  func TestPubSub_RewardCodeRecorded(t *testing.T)       // OnRewardCode("n","CODE","b") -> RecordClaimWithCode(acc,d1,"CODE")
  ```
- [ ] **Step 2: Run** `go test ./internal/runner/ -run TestPubSub -v`. Expected: FAIL.
- [ ] **Step 3: Implement `pubsub.go`** and register the hooks in `New`.
- [ ] **Step 4: Run** `go test ./internal/runner/ -v`. Expected: PASS.
- [ ] **Step 5: Commit** `git add internal/runner && git commit -m "feat(runner): PubSub progress, claim, stream-down and reward-code hooks"`

---

### Task 7: Pool supervisor + session Up next order

**Files:**
- Create: `internal/runner/pool.go`
- Test: `internal/runner/pool_test.go`
- Modify: `docs/CHANGELOG.md` (`## [Unreleased]` → `### Added`)

**Interfaces:**
- Consumes: `Runner`, `Snapshot`, `Notifier` (Task 3).
- Produces (plan 3 wires these into `cmd/miner` and the API):
  ```go
  type Unit interface {
      Run(ctx context.Context) error
      Snapshot() Snapshot
  }                                       // *Runner satisfies it
  type Idle struct{ AccountID, Platform string; State State } // Run blocks until ctx done; Snapshot returns {AccountID, Platform, State}
  type Entry struct{ ID string; Unit Unit }
  type Builder func() Entry
  func NewPool(n Notifier) *Pool
  func (p *Pool) Reload(parent context.Context, builders []Builder) error // first call derives the long-lived base ctx from parent; later parents are ignored for unit lifetime
  func (p *Pool) ReloadAccount(id string, b Builder)                    // cancel that unit, wait for it to exit, start the new one; others untouched
  func (p *Pool) Stop()                                                  // cancel all, wait, drop base ctx
  func (p *Pool) Snapshots() []Snapshot                                  // entry order
  func (p *Pool) SetOrder(accountID string, benefitIDs []string)
  func (p *Pool) ResetOrder(accountID string)
  func (p *Pool) OrderFunc(accountID string) func() []string            // pass as runner Config.Order; reads the pool's current order
  ```
- Supervise each unit in its own goroutine. Recover panics, and notify `"error"` with `{account, panic}`. A non-cancel `Run` error notifies `"error"` with `{account, error}`. This ports 1.x `scheduler.supervise`.
- Order is held in the pool in memory, keyed by account. It survives `Reload` and `ReloadAccount`, and is lost on process restart (spec: session-only).

- [ ] **Step 1: Write the failing tests:**
  ```go
  func TestPool_ReloadWithCancelledParentKeepsUnits(t *testing.T) // Reload(bg, …); then Reload(alreadyCancelledCtx, …) -> new units still running after 50ms (blocking test unit reports alive)
  func TestPool_ReloadAccountRestartsOnlyThatUnit(t *testing.T)   // two units counting starts; ReloadAccount("a") -> a started 2x, b 1x
  func TestPool_PanicNotifiesError(t *testing.T)                  // unit panics -> notifier got "error" with "panic" field; pool still serves Snapshots
  func TestPool_OrderSurvivesReloadAccount(t *testing.T)          // SetOrder("a",[x]); ReloadAccount("a") -> OrderFunc("a")() == [x]
  func TestPool_ResetOrder(t *testing.T)                          // SetOrder then ResetOrder -> OrderFunc("a")() == nil
  func TestPool_SnapshotsInEntryOrder(t *testing.T)               // Idle units a(needs_login), b(no_games) -> [a,b] with those states
  ```
- [ ] **Step 2: Run** `go test ./internal/runner/ -run TestPool -v`. Expected: FAIL.
- [ ] **Step 3: Implement `pool.go`.** Port the base-context, per-unit child context and done-channel design from `internal/scheduler/scheduler.go` and `control.go`.
- [ ] **Step 4: Run** `go test ./... && gofmt -l .`. Expected: all PASS, and `gofmt -l` prints nothing.
- [ ] **Step 5: Add the changelog line** under `## [Unreleased]` → `### Added`: `- v2 planner (\`internal/plan\`) and per-account runner + pool (\`internal/runner\`), replacing the watcher and scheduler. Not wired in yet (rebuild/v2).`
- [ ] **Step 6: Commit** `git add internal/runner docs/CHANGELOG.md && git commit -m "feat(runner): pool supervisor with session-only Up next order"`

---

## Outcome and carry-overs (2026-10-25)

Plan 1 is complete on `rebuild/v2` (db7c05d..a33600d). All seven tasks passed their reviews, and the whole-branch review is clean after one fix round.

### Calls changed during execution

- **Tick:** "Tick fixed at 60 s" was wrong. In 1.x the loop pulse defaulted to 5 s (`internal/store/settings.go`), and the 60 s value is the beacon cadence. The v2 runner moves from step to step without waiting, beacons right after `StartWatch`, then beacons every `TickInterval` (60 s). This changes when watch time accrues, so it needs a live-drop check before any tag.
- **PubSub hooks:** the real Twitch backends capture an account's hooks once, when that account's PubSub client is first created. The runner registers one trampoline per (backend, account), and the trampoline routes to the current `*Runner`. `platformtest.PubSubFake` now captures hooks at bootstrap the same way.
- **Fixes over 1.x:** freeze detection compares polled minutes, which stops the push-then-poll false freeze. The Kick swept notify drops the mined drop's campaign and image. An inventory row that is already claimed is settled without calling `Claim`.

### Carry-overs

- **Plan 2:**
  - A campaign with no live stream triggers a full rescan right away. Several such campaigns rescan back to back, which risks Twitch GQL rate limits. Add a short floor between rounds, or reuse the last scan's queue.
  - `OnInventory` fires only in `pick`. Reconcile needs its own schedule during a long watch, or an `OnProgress` hook fed by the tick's poll.
  - A claim-failure test should cover the full re-pick → start → tick cycle.
  - The reaper's `len(Benefits)==0` branch has no test.
- **Plan 3:**
  - Reuse one backend per account. Never wrap a backend per Runner: the hook registry is keyed by backend identity, so a wrapper silences PubSub after the first reload. Old backends stay pinned by the registry.
  - Prefer `ReloadAccount` over `Reload` when the game list changes, because `Reload` restarts every watch.
  - Set the restart policy for panicked units and units that exited with `errStop`. A panicked runner still holds `current`, so a late push can flip its snapshot to `claiming`. Clear `current` and call `Release()` in panic cleanup.
  - `Config.OnChange` may be called from two goroutines at once.
- **Tests:** `TestRunner_BeaconCadence` measures beat gaps when the fake receives each call, not at the `lastBeatAt` stamp. It can flake under load.
- **Kept platform code:** the Kick sweep hardcodes `Game: "Rust"` (`kick/backend.go`), so swept notifies can name the wrong game.
