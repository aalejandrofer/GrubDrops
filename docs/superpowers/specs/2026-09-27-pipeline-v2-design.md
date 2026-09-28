# Pipeline v2: campaign / progress / claim / inventory redesign

Issue: #50 (redesign), #49 (streamer priority, folded in).
Status: design approved in brainstorming 2026-09-27; spec pending review.

## 1. Goal

Replace the accreted watcher state machine (`internal/watcher/watcher.go`,
~2,050 LOC, ~20 layered special cases) with a pipeline built around one
authoritative per-drop state model reconciled from the platform.

### Success criteria

1. A drop claimed anywhere (in app, on the website, Kick sweep, or with a lost
   claim response) ends up `claimed` in GrubDrops and the account moves on
   without user action.
2. The inventory / drops page shows correct status without manual marking.
3. For any drop, one state + reason explains why it is or isn't being mined.
4. Streamer priority works: per-account priority streamers first, category
   fallback after (#49).
5. No drop is stuck forever: every `blocked` state carries a `retry_after` and
   a UI "retry now".

### Scope

Starting point: **the account already has a valid session.**

In scope: campaign discovery + filtering, campaign/channel selection
(incl. #49), watcher lifecycle, accrual orchestration, progress checks,
claiming, drop/claim state + reconciliation, inventory page state, the
backend interface the pipeline calls.

Out of scope: login / session acquisition, authcheck, #48, general web UI,
settings (except the streamer priority control), notifications, SSO, proxy,
sidecar internals (browser stack stays core, per CLAUDE.md).

Invariants:
- Accrual mechanics unchanged: Twitch Spade beacon at a fixed 60s
  `HeartbeatInterval`; Kick WS / IVS watch path as-is. Only orchestration
  around them changes.
- Existing DB data migrates in place. No forced reset.
- Stay Go + html/template + HTMX.

## 2. Problems being fixed (from code map, master @ efb8b39)

| # | Problem | Current cause |
|---|---|---|
| P1 | Externally claimed Twitch drop shows unclaimed forever, wastes a watch | Reconcile reads only `dropCampaignsInProgress`; finished campaigns vanish from it; ghost-skip persists a skip but never a claim row |
| P2 | Lost claim response → same as P1 | Claim error path relies on reconcile that can't see finished campaigns |
| P3 | Endless claim retry loop | No failure counter; non-success statuses loop pick → watch → claim |
| P4 | Kick link-gated claim loops in the watcher | `needsLink` guards only the sweep; Kick forces `AccountLinked=true` |
| P5 | Watch continues after drop claimed mid-watch | `tickWatch` never reads `Progress.Claimed` |
| P6 | Kick minutes never reach required | Adapter maps 0 → 120 required; progress uses `required_units` |
| P7 | Kick ghost-skip deadlock | Reward listed only after accrual; skip blocks accrual; no UI to clear `skip_override` |
| P8 | Prune flip-flop deletes genuine claim rows | Prune on transient `IsClaimed=false` |
| P9 | Precondition never unlocks after manual mark | Gate reads inventory `claimed`, not local state |
| P10 | Manual mark restarts every watcher | `addClaim` calls full `loadAndStart` reload |
| P11 | Kick sweep claims without writing state | Sweep bypasses `claims` |
| P12 | Platform logic leaks into generic code | `Platform == "twitch"` checks, `oilrats`, `Game: "Rust"`, placeholder-ID heuristic |

## 3. Architecture

```
               ┌──────────────┐  state changes   ┌────────────┐
 platform ───▶ │  Reconciler  │ ───────────────▶ │            │
 (detail/      └──────────────┘                  │            │
  progress)                                      │ Account    │──▶ planner.Plan(snapshot)
               ┌──────────────┐  Progress/       │ loop       │        │
 platform ◀──▶ │   Session    │  StreamDown/     │ (1 gorout- │◀───────┘ Decision
 (watch)       └──────────────┘  Stalled ──────▶ │  ine owns  │
               ┌──────────────┐                  │  state)    │
 platform ◀─── │   Claimer    │ ◀── claimable ── │            │
               └──────────────┘ ── result ─────▶ │            │
 UI pings (mark / skip / retry / priority) ────▶ └────────────┘
                                                       │
                                                  drop_state (DB) ──▶ /drops, dashboard
```

New packages (names indicative, final in plan):

- `internal/pipeline/dropstate`: state model, transitions, `DropStateStore`.
- `internal/pipeline/reconcile`: platform → drop_state sync.
- `internal/pipeline/planner`: pure `Plan(PlanInput) Decision`.
- `internal/pipeline/session`: one accrual run on one channel.
- `internal/pipeline/claimer`: claim policy + backoff.
- `internal/pipeline/loop`: per-account event loop wiring the above.

v1 (`internal/watcher`) is untouched and stays selectable until v2 is
live-verified; then deleted in a follow-up release.

## 4. Drop state model + reconciler

### 4.1 `drop_state` table (new migration `0016_drop_state.sql`)

One row per (account_id, drop_id).

| column | type | meaning |
|---|---|---|
| `account_id` | TEXT | FK accounts |
| `drop_id` | TEXT | platform drop / reward id (Twitch timeBasedDrop id; Kick reward id) |
| `campaign_id` | TEXT | owning campaign |
| `platform` | TEXT | `twitch` / `kick` |
| `status` | TEXT | `eligible`, `accruing`, `claimable`, `claimed`, `blocked` |
| `block_reason` | TEXT NULL | `needs_link`, `claim_failed`, `sub_only`, `no_channels`, `expired`, `not_enrolled`, `user_skip` |
| `minutes` | INTEGER | platform-reported watched minutes |
| `required` | INTEGER | platform-reported required minutes |
| `source` | TEXT | `platform` or `user` (who last set status) |
| `fail_count` | INTEGER | consecutive claim failures |
| `retry_after` | TIMESTAMP NULL | when a `blocked` row is re-evaluated |
| `synced_at` | TIMESTAMP | last platform confirmation |
| `updated_at` | TIMESTAMP | |

PK (account_id, drop_id). Index on (account_id, status).

Status meaning:
- `eligible`: mineable, no progress yet.
- `accruing`: 0 < minutes < required.
- `claimable`: minutes ≥ required (platform-reported) and not claimed.
- `claimed`: platform says claimed, or our claim call succeeded, or user mark.
- `blocked`: not mineable now; `block_reason` says why, `retry_after` says when to look again.

Multi-item Twitch drops (one timeBasedDrop, several rewards) are **one**
drop_state row keyed by the timeBasedDrop id; the reward names are display
data held on the benefits side, not separate state.

### 4.2 Reconciler

Per account, own goroutine. Emits `StateChanged` events to the loop; never
decides what to watch.

**Triggers:** start-up; every `GRUB_RECONCILE_INTERVAL` (default 15 min);
immediately after any claim attempt; on `Stalled` from a session; on UI
"retry now".

**Twitch:**
1. `ListActiveCampaigns`, filtered to whitelisted games for this account.
2. For each in-scope campaign, `DropCampaignDetails` (already wired,
   `twitch/campaigns.go` with `detailsCache`). Per `timeBasedDrop.self`:
   `isClaimed`, `currentMinutesWatched`, `dropInstanceID`. Details cache is
   bypassed on post-claim reconciles.
3. Map to status. This works for campaigns that have left
   `dropCampaignsInProgress`, which is the fix for P1/P2.

Verification item (plan task 1): confirm the persisted query response
carries `self { isClaimed currentMinutesWatched dropInstanceID }` for an
enrolled but completed campaign. If `self` is absent, fall back to
`Inventory.dropCampaignsInProgress` + per-drop claim result, and the spec is
revisited before building on it.

**Kick:**
1. `/drops/progress` (Bearer session) + `CampaignDetails`.
2. `required` always comes from the platform (`required_units`); the adapter
   no longer invents 120 (fixes P6). A reward with no platform minutes and no
   progress entry is `eligible` with `required` from campaign detail.
3. Verification item (plan task 1): does `/drops/progress` keep claimed
   rewards listed with `claimed=true`, or drop them?
   - If kept: straightforward mapping.
   - If dropped: a reward that disappears is `claimed` **only** if our own
     claim call returned success for it; otherwise `blocked:not_enrolled`
     with `retry_after` (no inference from absence).

**Rules:**
- `claimed` from the platform is monotonic: never demoted by a later sync.
  No prune (fixes P8).
- A `user` mark stands while the platform is silent about the drop; a
  definite platform answer overwrites it (`source` flips to `platform`).
- `blocked` rows are re-evaluated when `now ≥ retry_after`; there is no
  permanent skip (replaces ghost-skip + self-heal, fixes P7).
- Every transition into `claimed` also appends to `claims` (history), via
  `RecordClaimIfNew`, regardless of who claimed (fixes P11).

### 4.3 Removed by this section (v2 path)

`tracked` set + prune, ghost-skip + self-heal, vanish detection,
`skip_override:` / `collect_override:` kv reads, full reload on manual mark.

### 4.4 Migration

`0016_drop_state.sql` creates the table. A one-shot Go backfill at startup
(idempotent, guarded by a kv flag `drop_state_backfilled`):
- `claims` rows → `claimed`, `source=platform`.
- `collect_override:` → `claimed`, `source=user`.
- `skip_override:` → `blocked:not_enrolled`, `retry_after=now` (so they get
  one fresh evaluation instead of staying dead).

Old kv keys are left in place until v1 is deleted, so rollback to v1 is safe.

sqlc: no `?` or parentheses in `queries/*.sql` comments.

## 5. Planner + channel selection (#49)

### 5.1 Contract

```go
func Plan(in PlanInput) Decision

type PlanInput struct {
    Now          time.Time
    Drops        []dropstate.Row          // this account
    Campaigns    []platform.Campaign      // in-scope, with windows + preconditions
    Settings     AccountSettings          // game priority, streamer priority, force-watch
    Live         map[CampaignID][]Stream  // candidate live channels per campaign
    Cooldowns    map[ChannelKey]time.Time // stalled channels
    Current      *Decision                // for swap hysteresis
    LastSwap     time.Time
}

type Decision struct {
    Kind    DecisionKind // Mine, ForceWatch, Idle
    Channel Stream
    Serves  []DropID     // all candidate drops this channel accrues
    Reason  string       // human-readable, shown on dashboard
}
```

Pure: no I/O, no clock reads, no goroutines.

### 5.2 Algorithm

1. **Candidates:** drops in `eligible` / `accruing`, game whitelisted, now
   inside campaign window, preconditions satisfied. Preconditions read
   `drop_state` (a user-marked or platform-claimed precondition unlocks;
   fixes P9). `sub_only` (0-minute Twitch) drops are `blocked:sub_only`
   upstream and never candidates.
2. **Rank campaigns:** game priority → ends soonest → lowest-tier first.
3. **Channel for top campaign:**
   1. Streamer priority list (per account, ordered) ∩ live ∩ right category
      ∩ campaign allow-list (if restricted) ∩ not on cooldown.
   2. Fallback: other eligible live channels by viewers.
   If none, try next campaign. If no campaign yields a channel → force-watch
   or idle.
4. **Serves:** every candidate drop (any campaign) that the chosen channel
   accrues. Kick: typically several; Twitch: typically one.
5. **Hysteresis:** if `Current` is still valid (channel live, serves ≥1
   candidate) and `Now - LastSwap < 10m`, keep it. A dead/stalled current
   channel bypasses the hold.
6. **Force-watch:** only when no Mine decision exists; first **live**
   force-watch channel for the account. Idle otherwise, with reason
   (`no_games`, `no_live_channels`, `all_claimed`, ...).

### 5.3 Channel probing

New optional backend capability:

```go
type ChannelProber interface {
    ProbeChannels(ctx context.Context, s Session, c Campaign, logins []string) ([]Stream, error)
}
```

The loop calls it for priority streamers so low-viewer favourites outside
Twitch's top-30 directory are still found. Kick implements it on top of the
existing `probeLive`. Twitch implements it with the existing per-login live
+ game check, taking logins from the argument (not the cached allow-list).

### 5.4 Storage + UI

New table `account_streamer_priority(account_id, platform, login, rank)`,
created in `0016` alongside drop_state. Existing `account_channels`
(null-game whitelist) rows are copied in, keeping order; the null-game gate
reads the new table. `account_channels` is dropped when v1 is deleted.

UI: ordered streamer list per account next to game priority (drag-reorder
or up/down, reuse existing priority control pattern). Editing pings the
loop; no reload.

### 5.5 Removed

`Platform == "twitch"` check in pick, no-stream campaign set, `oilrats`
preference, scattered sorts in `pickCampaign`, force-watch without liveness.

## 6. Session, claimer, loop, backend interface

### 6.1 Session

Wraps existing `StartWatch` / `Heartbeat` / `StopWatch`. Emits:
- `Progress{DropID, Minutes, Required, Claimed}` (Twitch PubSub or Kick poll)
- `StreamDown`
- `Stalled` (no minute gain for N consecutive polls; N from current freeze
  detector)

Reports only; makes no decisions. `Progress.Claimed=true` is honoured by the
loop immediately (fixes P5).

### 6.2 Claimer

Triggered when a drop enters `claimable`.

| Outcome | Action |
|---|---|
| Success / Twitch `DROP_INSTANCE_ALREADY_CLAIMED` | → `claimed`; trigger reconcile |
| Link required (Kick connect_url, Twitch unlinked) | → `blocked:needs_link`, no retry; UI "link, then retry" (fixes P4) |
| Other error | `fail_count++`, backoff 1m → 5m → 30m; after 5 → `blocked:claim_failed`, `retry_after` 6h (fixes P3) |
| Lost response | post-claim reconcile reads `isClaimed` and corrects (fixes P2) |

Kick sweep becomes claimer policy: on each Kick progress poll, claim
every drop in `claimable` for that account. Same state writes as a normal
claim, so no unrecorded claims. Hardcoded `Game: "Rust"` removed.

### 6.3 Account loop

```go
events := merge(reconciler.Events, session.Events, ticker.C, uiPings)
for ev := range events {
    apply(ev)                    // mutate drop_state via DropStateStore
    d := planner.Plan(snapshot())
    if !d.Equal(current) { swapSession(d) }
}
```

- Single goroutine owns all transitions.
- Live re-check ticker: 5 min (feeds `Live` for the planner, including
  priority-streamer probes).
- UI actions (mark collected, skip, retry, priority edit) send a ping; no
  `Reload` (fixes P10).
- Exposes a snapshot (status, decision reason, channel, serving drops) to the
  scheduler for the dashboard, via the existing scheduler state API.

### 6.4 Backend interface

Split for the pipeline (login methods stay on the existing `Backend`, out of
the pipeline):

```go
type CampaignSource interface { ListActiveCampaigns(ctx, s) ([]Campaign, error) }
type ProgressSource interface { DropProgress(ctx, s, campaigns []Campaign) ([]DropProgress, error) }
type ChannelSource  interface { ListEligibleChannels(ctx, s, c) ([]Stream, error) }
type ChannelProber  interface { ProbeChannels(ctx, s, c, logins) ([]Stream, error) }
type WatchSource    interface { StartWatch(...); Heartbeat(...); StopWatch(...) }
type ClaimSink      interface { Claim(ctx, s, DropProgress) (ClaimResult, error) }
```

`ClaimResult` carries typed outcomes (`Claimed`, `AlreadyClaimed`,
`NeedsLink`, `Failed`) instead of string-matched errors.

A `pipeline.Platform` struct is assembled once per backend at construction,
holding whichever capabilities it has; no type assertions in the loop. Store
access through one `DropStateStore` interface (replaces anonymous
assertions `ClaimedBenefitIDs` / `RecordClaimIfNew` / `PruneClaim` /
`PersistAccountLinks`).

Platform quirks stay in adapters: Kick required-minutes normalisation,
Twitch placeholder-ID handling, Kick channel preferences.

### 6.5 Rollout

- `GRUB_PIPELINE=v1|v2` (default `v1`), overridable per account (account
  settings toggle) so v2 can run on one account first.
- `cmd/miner/main.go` `build()` branches on the selection; v1 wiring
  unchanged.
- Default flips to `v2` only after live verification on a Twitch drop and a
  Kick drop (CLAUDE.md accrual/claim gate). v1 deletion (incl.
  `watcher.go`, `skip_override`/`collect_override` loaders, `account_channels`)
  is a separate follow-up release.

## 7. Error handling

- Platform call errors inside reconcile: log, keep last known state, retry
  next tick. Never demote state on error.
- Session start failure: channel onto cooldown, re-plan.
- DB write failure: loop logs and retries the apply on next event; planner
  runs on in-memory snapshot so mining continues.
- `ErrIntegrityBlocked` and session-expiry surface to the scheduler as
  today (auth is out of scope); v2 does not change their handling.

## 8. Testing

- **Planner:** table-driven tests. Cases: priority streamer live / offline /
  not in allow-list, fallback, hysteresis hold and dead-channel bypass,
  preconditions via user mark, force-watch liveness, idle reasons, Kick
  multi-serve.
- **Claimer:** table tests over `ClaimResult` outcomes and backoff ladder.
- **Reconciler:** fake backend responses. Cases: finished Twitch campaign
  with `isClaimed=true` (P1), lost claim response (P2), Kick claimed
  retained vs. dropped, user mark overwritten by platform, `claimed` never
  demoted, blocked retry_after expiry.
- **Loop:** event-sequence tests with fake clock. Cases: claimed on website
  mid-watch (P5), stall → cooldown → swap, UI ping re-plans without reload.
- **Migration:** backfill from claims + kv overrides, idempotent on rerun.
- **Adapters:** Kick required-minutes from platform (P6); Twitch
  `DropProgress` from details `self`.
- Every regression test must fail against a deliberate revert of its fix.

Release gate: green build + unit tests + Kick WS canary, then live-drop
verification (Twitch and Kick) on staging before any tag that enables v2.

## 9. Open verification items (plan task 1, before building on them)

1. Twitch `DropCampaignDetails` returns `self` for enrolled completed
   campaigns.
2. Kick `/drops/progress` retention of claimed rewards.

Either answer may adjust §4.2 mapping; the rest of the design is unaffected.

Status: probes committed (behind the `live` build tag); results pending. Must also be run with a TV-client Twitch token once the #47 TV-client login lands.

## 10. Refinements during planning

1. Reconcile runs serially on the loop goroutine (single writer; session/PubSub events buffer meanwhile).
2. Timestamps stored as INTEGER unix seconds, 0 = unset; block_reason NOT NULL DEFAULT ''.
3. user_skip has no auto-expiry; cleared only by the user.
4. Platform interfaces named DropProgressSource, DropClaimer (ClaimDrop returns a typed ClaimResult), ChannelProber in internal/platform.
5. Every dropstate mutator leaves a platform-confirmed claim untouched; sub_only re-derives after its window like not_enrolled.
6. Session stall detection is per-drop, and the loop restarts the session on the same channel when the served-drop set changes; session events carry a generation so stale events are dropped.
7. A claimed=true from either Twitch source (details self or in-progress inventory) wins.
8. Kick rows listed without required_units fall back to campaign minutes (reporting 0 would block as sub_only).
