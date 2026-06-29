# Manual "mark collected" — design

**Date:** 2026-06-29
**Status:** approved (brainstorm), pending implementation plan

## Problem

A drop can be collected outside GrubDrops — e.g. the user redeems a Twitch
drop reward in the Twitch UI directly, or claims a code manually. GrubDrops has
no record of that claim, so the `/drops` item panel shows the benefit as
uncollected (`—`) forever and the dashboard "claimed X/Y" undercounts. There is
already a manual **un**collect (click a COLLECTED chip → deletes the claim);
this adds the inverse: a manual **mark-collected**, per account + benefit.

The feature must coexist with the watcher's reconcile/self-heal prune, which
deletes a claim row when the platform inventory still reports that drop
in-progress and unclaimed. Without protection, a manual mark would be wiped on
the next discovery cycle.

A second, blocking problem surfaced while scoping: some campaigns' item panels
never populate ("Loading items…" → empty), so there is no benefit row to mark
against. Root cause below; fixing it is in scope.

## Goals

- Let the user mark a specific benefit as collected for a specific account from
  the `/drops` campaign-items panel.
- The mark persists: it is **not** removed by the reconcile prune.
- The mark is reversible via the existing uncollect control.
- Make item panels load reliably so a drop collected on a now-disabled account
  is still markable.

## Non-goals / known limitations

- A campaign with **genuinely zero discrete benefits** (a pure watch-time
  reward with no item row) has no `benefit_id` to attach a claim to and is
  therefore not markable per-benefit. Out of scope — would require synthesising
  a placeholder benefit. The panel shows the existing "no items" state.
- No bulk "mark all accounts" action; one account per click (the menu lists
  each eligible account).

## Existing mechanics (reference)

- **`claims` table** (`migrations/0001_init.sql`): `(id PK, account_id,
  benefit_id, claimed_at, value_meta_json)`, `UNIQUE(account_id, benefit_id)`
  (migration 0011). One row per account+benefit; `InsertClaim` upserts.
- **Manual uncollect:** `POST /drops/claim/remove` → `removeClaim`
  (`handlers_drops.go:1186`) → `DeleteClaimFor(account_id, benefit_id)` →
  re-renders the items panel. Button in `_drops_campaign_items.html:18-24`.
- **Reconcile prune:** `watcher/watcher.go` (~967-1019), runs each discovery
  cycle, only when `inventoryOK == true`. Deletes a claim when the benefit is
  `tracked` (present in in-progress inventory) AND `!claimed` (inventory reports
  it unclaimed). Drops that have left inventory entirely are never touched.
  Prune is done via `ClaimRecorder.PruneClaim` (`store/claim_recorder.go`).
- **Link override (pattern to mirror):** kv table `(key, value)`; prefix
  `link_override:` + campaignID, value `"1"`. Set via `UpsertSettingString`,
  read via `ListKVByPrefix`, cleared via `DeleteKV`. Watcher consumes it through
  the `cfg.ForceLinked func(campaignID string) bool` closure; handler loads all
  via `linkOverrides()` (`handlers_drops.go:103`).
- **Items panel:** `GET /drops/campaigns/{id}/items` → `renderCampaignItems`
  (`handlers_drops.go:1206`). `ListBenefitsForCampaign`; if empty,
  `lazyFetchBenefits` → `sessionForPlatform` (uses **`ListEnabledAccounts`**,
  line 84) → backend `CampaignDetailer`. Per-benefit COLLECTED marks from
  `ListClaimsForCampaign`. Template `_drops_campaign_items.html`.

## Design

### 1. Items always load (unblock marking)

**Root cause:** `sessionForPlatform` only iterates enabled accounts. When the
account a drop was collected on is disabled — or no enabled account on that
platform has a stored session — `lazyFetchBenefits` cannot fetch, benefits stay
empty, and the panel is unmarkable.

**Fix:** broaden `sessionForPlatform` to fall back to **any** account on the
platform that has a stored session when no enabled one does. The fetch is a
read-only public campaign-detail call; account enabled-state should not gate it.
Prefer an enabled account's session first (cheap, current behaviour), then fall
back to any with a session.

After lazy-fetch, if benefits are still empty, the existing `campaign_items.no_items`
branch renders (no perpetual "Loading…"). The handler always returns the
partial.

### 2. Mark + protect (data)

Marking a benefit collected for an account does two writes:

1. **`InsertClaim`** for `(account_id, benefit_id)`, `claimed_at = now`,
   `value_meta_json = {"manual": true}`. Idempotent (upsert on the unique
   index). This makes the existing COLLECTED chip and dashboard counts light up
   with no rendering changes.
2. **kv override** `collect_override:<benefitID>:<accountID>` = `"1"` via
   `UpsertSettingString`. New prefix constant `CollectOverridePrefix =
   "collect_override:"` in `internal/store` (alongside `LinkOverridePrefix`).
   Keyed by benefit+account so it matches the claim's identity exactly.

Why both: the claim row drives display; the override flag tells reconcile not
to prune it.

### 3. Watcher coexistence

- Add `cfg.ForceCollected func(accountID, benefitID string) bool` to the watcher
  config, twin of `ForceLinked`.
- In **both** prune sites (the in-progress branch ~977 and the inventory sweep
  ~1008), before calling `PruneClaim`, skip when
  `ForceCollected(accountID, benefitID)` is true. (If `ForceCollected` is nil,
  treat as always-false so existing call sites/tests are unaffected.)
- Wire the closure where watchers are constructed (same place `ForceLinked` is
  wired): it queries `collect_override:*` (a small `ListKVByPrefix` → set,
  built per pickCampaign or cached like link overrides) and returns membership
  for `benefitID:accountID`.

Result: an overridden mark survives even while the drop is tracked in-progress
and reported unclaimed. It is only removed by an explicit uncollect.

### 4. API

- **New** `POST /drops/claim/add` → `addClaim` (mirror of `removeClaim`):
  - form: `account_id`, `benefit_id`, `campaign_id` (all required).
  - Validate the account and benefit exist and the account's platform matches
    the campaign's platform; reject otherwise.
  - `InsertClaim` (manual meta) + `UpsertSettingString` the override key.
  - Log `slog.Info("manual claim mark", ...)`.
  - Re-render the items panel via `renderCampaignItems` (same swap as uncollect).
- **Update** `removeClaim`: after `DeleteClaimFor`, also `DeleteKV` the override
  key `collect_override:<benefitID>:<accountID>`. No-op for auto claims (no key).
  This ensures uncollecting a manual mark drops its reconcile protection so it
  doesn't silently get re-pruned-immune state left behind.
- Route registered in `server.go` next to `/drops/claim/remove`, behind the
  same auth + CSRF.

### 5. UI

In `_drops_campaign_items.html`, each benefit row's COLLECTED cell:

- Render existing claimed chips (unchanged, still click-to-uncollect).
- After them, an "add" control: a `+` button that reveals a small inline menu
  listing **all matching-platform accounts not already collected for that
  benefit**. Each menu entry `hx-post`s to `/drops/claim/add` with
  `account_id` + `benefit_id` + `campaign_id`, CSRF header,
  `hx-target="closest .drop-items" hx-swap="outerHTML"` (same target as
  uncollect).
- No confirm dialog (the action is additive and reversible).
- The `+` is hidden when there are no eligible accounts (every matching-platform
  account already collected).

The eligible-account list is computed server-side in `renderCampaignItems` and
attached to each `campaignBenefitRow` (e.g. a new `Addable []addableAccount`
field with `{Login, Platform, AccountID}`). Accounts come from
`ListAllAccounts` filtered to the campaign platform, minus those already in
that benefit's `Collected`.

New i18n keys (en/es/zh-CN): `campaign_items.mark_collected` (button/title),
`campaign_items.mark_collected_for` (menu header) — exact keys finalised in the
plan; locale-parity test must stay green.

## Testing

- `addClaim` inserts a claim with manual meta **and** sets the override key;
  returns the re-rendered panel showing the new chip.
- `removeClaim` deletes the claim **and** the override key.
- Eligible-account computation: excludes already-collected accounts and
  cross-platform accounts; includes disabled accounts.
- Reconcile guard: a benefit with a `ForceCollected` hit is **not** pruned when
  tracked + unclaimed; without the hit it is pruned (regression-protects the
  existing self-heal). Use/extend the watcher prune test harness.
- `sessionForPlatform` fallback: returns a disabled account's session when no
  enabled account on the platform has one.
- Template render: `+` menu appears with eligible accounts; hidden when none.

## Rollout

UI + claim plumbing; no accrual/claim-path (watch-time) changes beyond the
reconcile guard. Per project rules this is close to the default gate, but the
reconcile-guard touches the prune logic that already caused a prod incident
(see the claim-prune incident memory), so: cover the guard with a unit test and
verify the prune still deletes genuinely-stale unclaimed marks. Log every change
to `docs/CHANGELOG.md` under `[Unreleased]`.
