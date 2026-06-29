# Accrual canary + CI regression tests — design

- **Date:** 2026-06-15
- **Status:** approved (pending spec review)
- **Target release:** v1.2.0

## Problem

We can't reliably answer "does the miner still accrue watch-time?" because drop
campaigns aren't always live — every release we hit the verify-mining gate with
all watchers asleep. We need to test the watch-time **transport** (Kick WS
frames / Twitch watch beacon) decoupled from whether any drop is active, both as
a fast CI regression guard and as a live in-prod check that alerts on breakage.

## Goal

1. **CI regression guard** — catch frame-format / beacon-shape regressions on
   every push, no network, no credentials.
2. **Live canary** — periodically prove the accrual transport is accepted by each
   platform against a known always-live channel, surface the result, and alert
   when it breaks.
3. **Settings restructure** — new Health tab (canary + relocated Status), tab
   reorder, and split logging out of Runtime on the General tab. Bundled because
   the canary's UI home is the new Health tab.

## Non-goals

- **Not** proving a drop was *credited* — no drop may be live during a canary
  run. The canary proves the beacon/WS frames are *accepted* (transport healthy),
  which is the strongest signal obtainable without a live campaign. This
  limitation is documented in the UI copy.
- Not replacing the manual pre-release verify; it complements it.
- Not i18n, not light-theme work (separate).

## Part B — CI regression replay (build first)

Cheap, no dependencies, immediate value; the encode/parse code it covers is then
reused by Part A.

- **Fixtures:** captured once from a real session into `testdata/`:
  - Kick: a recorded sequence of WS frames received while watching (incl. auth
    handshake, periodic `channel_handshake` ~12s, pong).
  - Twitch: a recorded watch-beacon request (URL/headers/body) + its 2xx response.
- **Tests:**
  - **Kick WS parser replay** — feed recorded frames into the WS message handler;
    assert it recognises the handshake/accrual frames and drives expected internal
    counters (e.g. counts `channel_handshake`, handles pong). Guards frame-format
    regressions.
  - **Twitch beacon golden test** — build the watch beacon for a known channel;
    assert URL + body + headers match the recorded-good shape. Guards
    beacon-shape regressions.
- Pure unit tests beside the existing `kick` / `twitch` packages. No live creds.

## Part A — In-app live canary

New `internal/canary/` package. A scheduled runner wired in `cmd/miner` alongside
discovery/authcheck, firing every `GRUB_CANARY_INTERVAL` (default 6h; 0 = off).

### Probes (standalone — NOT real watchers)

The canary opens the WS / sends the beacon **directly**, bypassing the scheduler,
so it never consumes an account's exclusive single-watch slot or disturbs live
mining. It borrows an enabled account's session read-only.

- **Twitch probe:** send the real watch beacon to the canary channel, pacing 2
  beacons ~60s apart; assert each returns 2xx. Reuses the watcher's beacon
  builder (extracted if coupled).
- **Kick probe:** open the WS watch path to the canary channel for ~75s; assert
  connect + auth handshake + at least N periodic `channel_handshake` frames at the
  expected cadence + pong received.

### Config

- `GRUB_CANARY_TWITCH_CHANNEL` — default `alveussanctuary` (genuine 24/7 stream).
- `GRUB_CANARY_KICK_CHANNEL` — default empty → Kick canary skipped until set
  (no reliably-24/7 Kick channel chosen yet).
- `GRUB_CANARY_INTERVAL` — default 6h; 0 disables.
- All three editable on the new **Settings → Health** tab (DB-backed, env
  overrides, matching the existing settings pattern).

### Reporting

- Last result per platform persisted in KV (like authcheck):
  `{ok, checkedAt, detail}`.
- **Settings → Health** shows: "Accrual canary — Twitch ✓ 5m ago · Kick — not
  configured", red on fail with the detail.
- **Discord alert** on pass→fail transition, gated by a new notify kind
  (reuses the notifier).
- **"Run canary now"** button (on-demand, returns an HTMX fragment with the
  result).

## Settings → Health tab + tab reorder

The new Health tab is the home for all "is it working?" info:

- **Accrual canary** results (above) + Run-now.
- **Status panel moved here** — version, git commit, uptime, running sidecars,
  browser URL, log level. Currently lives elsewhere in Settings; relocate the
  whole panel under Health (it's health/diagnostics, not config).

**Tab order** (subnav) becomes:

```
General · Accounts · Drop Priority · Notifications · Security · Health · Experimental
```

(Was: General · Drop Priority · Notifications · Security · Accounts · Experimental.
Accounts moves up to 2nd; Health is new, second-to-last; Experimental stays last.)

Routes: add `/settings/health`; move the Status render out of the General tab
into the Health handler/template. Keep existing tab routes otherwise.

### General tab: split Runtime / Logging

The General tab's "Runtime" section currently mixes cadence + logging. Split it:

- **Runtime** section keeps: tick interval, discovery interval.
- New **Logging** section (own card on the General tab): log level, log retention.

(Same `/settings` POST handler; just regroup the fields into two cards. Decision:
Logging stays on General as its own section — NOT a separate tab and NOT under
Health. Revisit at spec review if it should live under Health instead.)

### Section headers orange

Section subtitles (the `.section-h h3` — Runtime, Logging, Status, etc.) render
in the accent orange (`var(--accent)`). Single CSS rule; works in both themes
since `--accent` is theme-aware.

## Console row-height alignment fix

Bug: on the Console, Kick account rows are taller than Twitch rows when idle,
because the per-account WS/Chrome pill stacks under the name in the `.who` flex
column (name + 6px + pill ≈ 31px) while Twitch rows have only the name (~12px).
The two columns' rows drift out of alignment.

Fix: reserve the pill's vertical space on every row so pill-less rows match —

```css
.account-row .who { min-height: 30px; justify-content: center; }
```

Pill-less rows center the name in the same reserved height → both columns equal
total height when idle (value tuned to the with-pill height during the visual
check). Cosmetic, dashboard CSS only.

## Components / file layout

- `internal/canary/canary.go` — runner, scheduling, result store, on-demand run.
- `internal/canary/twitch.go` — Twitch beacon probe.
- `internal/canary/kick.go` — Kick WS probe.
- `internal/store/settings.go` — canary channel/interval getters+setters.
- `internal/api/handlers_settings.go` + `settings.html` — Health tab + Run-now.
- `cmd/miner/main.go` — wire the canary runner (interval from settings/env).
- Reuse: existing kick WS code + twitch beacon code (extract probe-able seams).

## Data flow

`cmd/miner` wires canary runner → on tick, per platform with a configured channel
+ an enabled session, runs the probe → stores result in KV → Settings → Health
reads it; notifier fires on pass→fail.

## Testing (TDD)

- Canary result-store round-trip (KV).
- Probe pass/fail interpretation with a fake transport (success vs failure).
- Settings round-trip for the three canary settings (default + coerce).
- Part B fixtures are themselves the regression tests.
- Health-tab render test (asserts the result + Run-now control present).

## Build order

1. Part B (CI regression replay) — no deps, immediate guard.
2. Settings (canary config) + Health tab.
3. Part A probes (reuse B-verified encode/parse) + runner + reporting.

## Open items

- Find a reliably-24/7 Kick channel for the Kick canary default (currently
  unset/skipped). Until then Kick canary is opt-in via config.
- **Kick `ProbeWS` resolves a synthetic livestream id (1), not the configured
  channel** — verifies the WS transport mechanism (dial/handshake/pong) but not
  against the real channel. Before enabling the Kick canary in prod, wire
  channel-slug → livestream-id resolution into `ProbeWS`. Default-off shields
  this for now.
- Confirm the Twitch watch-beacon endpoint returns success for a logged-in
  session on a channel with no drop attached (expected, but verify during
  Part A).
