# GrubDrops v2: ground-zero rebuild

**Date:** 2026-10-25
**Status:** Approved by Jandro 2026-10-25
**Decision owner:** Jandro
**Kaneo:** GRUB-14 (spec), GRUB-15 to GRUB-19 (rebuild tasks)
**Approved design:** `sandbox/home-rebuild/r4/` (index, games, rewards, settings, states). Product context: `PRODUCT.md`.

## Why

GrubDrops 1.x grew fix on fix. Twitch stopped listing campaigns for new logins and blocked the Android device-code flow, so the app now only works as "choose your games, mine those" on TV-token logins, but the UI and the discovery/claim pipeline still assume full discovery. Users can't tell whether it's working. Pipeline v2 (PR #52) tried to fix the pipeline inside the old shape and is dropped.

v2 keeps the code that earns watch-time and rebuilds everything around it, simpler, from the r4 mockups. Dropping 1.x features is fine.

## Goals

- Home answers "is it working?" in seconds; problems show as one amber chip with one fix.
- The user picks games; GrubDrops discovers, mines and claims only those, on Twitch and Kick, across many accounts.
- Every account earns every drop: each account mines and claims its own copy, as in 1.x.
- Platform inventory is the truth for what's claimed: a drop that an account claimed outside GrubDrops shows as claimed for that account, and that account moves on.
- In-place upgrade from 1.x: accounts and sessions keep working, no re-login.

## Anti-goals (v2 fails if any of these happen)

- **Harder to set up** than 1.x: more required env vars, more containers, or more steps for a fresh install.
- **Mining regresses**: less watch-time credited, or missed claims, compared with 1.x on the same accounts.
- **More complex** to run or reason about: v2 must have fewer moving parts and fewer settings than 1.x, not more.

## Non-goals

- New platforms beyond Twitch and Kick.
- Logged-out catalog scraping or full campaign discovery on Twitch.
- Twitch Android device-code login (blocked by Twitch).
- Reusing pipeline v2 code (PR #52 is dropped; close it unmerged in step 1, keep the branch as history).

## Decisions

| Topic | Decision | Who |
|---|---|---|
| Frontend | SvelteKit with `adapter-static`, TypeScript. Built to static files and embedded in the Go binary with `go:embed`. Node is build-time only. | Jandro |
| API | Go JSON API under `/api/*`; live updates over Server-Sent Events at `/api/events`. | Jandro |
| Branch | Long-lived branch `rebuild/v2` off master. Merged when v2.0.0 is ready. | Jandro |
| PR #52 | Dropped entirely. | Jandro |
| Scope of v2.0.0 | Full parity with the r4 mockups. | Jandro |
| Languages | English, Español, 简体中文. One set of locale JSON files shared by Go (notifications) and Svelte. | Jandro |
| Twitch login | TV-token flow only (the only one that works). | Jandro |
| Database | Same SQLite file and goose migrations; change tables only where the new model needs it. | Claude's call (Jandro: "upgrade if needed, else keep") |
| Web login | Keep current admin password + optional OIDC, server-side cookie session. The SPA calls `/api/*` with that cookie. | Claude's call (same) |
| 1.x on master | Fixes only while v2 is built: merge PR #62, Twitch watch-time fix (GRUB-2), dependency bumps. | Claude's call (same) |
| Drops per account | Every account earns every drop. The planner plans per account; Up next is one queue per account. | Jandro |
| Non-TV Twitch sessions | 1.x Android and cookie sessions keep mining after upgrade until their session fails, then show "needs login" (TV flow). The kept `twitch` backend keeps both code paths. | Jandro |
| GRUB-2 | The v2 build does not wait on GRUB-2; it runs alongside on master. v2.0.0 is not tagged until a live Twitch drop credits on a TV-token account. | Jandro |
| Claim history | GrubDrops keeps its own claim log for history (Rewards, Claimed, "all time"). Inventory decides state only. Claims that reconcile finds made outside GrubDrops are added to the log. The 1.x `claims` rows are migrated in. | Claude's call |
| Total Minutes Watched | New all-time counter, seeded on upgrade from 1.x `progress` minutes. | Claude's call |
| Per-account game lists | Dropped. Migration appends the union of 1.x `account_games` after the global list (deduped), so no account stops mining a game it mined before. | Claude's call |
| Notifications | Discord webhook in Settings; `GRUB_DISCORD_WEBHOOK` still read as the default. The four toggles and the daily summary are built. Telegram is shown disabled ("Coming in v1.5.0"). Per-account webhooks are dropped; the global one is used. | Claude's call |
| Rollback | v2.0.0 migrations only add tables and columns, so 1.x still boots on the same DB. Before the first v2 migration, the miner copies `miner.db` to `miner.db.pre-v2`. Old tables are dropped in a later release. | Claude's call |

## What is kept (mining core)

Kept as-is; v2 calls it, does not rewrite it. Behaviour changes here need live-drop verification (AGENTS.md).

- `internal/platform` (`Backend` interface, `twitch`, `kick`): login/refresh, `ListActiveCampaigns`, `ListEligibleChannels`, `InventoryProgress`, `StartWatch` / `Heartbeat` / `StopWatch`, `Claim`, `RewardClaimer`, PubSub hooks. Twitch 60s `HeartbeatInterval` beacon. Kick WS + IVS via sidecar.
- `internal/auth/browser`, `internal/dockerctl`, `cmd/browser-sidecar`, `proto/` + buf config: on-demand Chrome sidecars for Kick.
- `internal/store` session encryption (age) and the tables holding accounts and sessions.
- `internal/notify` Discord notifier, `internal/update` version check, `internal/canary` Kick WS canary, `internal/auth/oidc` + password auth, `internal/config`, `internal/log`.

## What is rebuilt

| Area | 1.x | v2 |
|---|---|---|
| Orchestration | `internal/watcher` (picks campaign, picks stream, watches, claims; 2.2k LOC) + `internal/scheduler` | New per-account **runner** that only executes a plan: watch this drop on this channel, report progress, claim when done. A single **planner** decides what every account does, from chosen games, priority, the session-only Up next order, and platform inventory. |
| Discovery | `internal/discovery` catalog scraper, whitelist filters, force-watch, channel whitelists | Per-chosen-game scan on each platform: active campaigns + live eligible channels. Open (category-wide) campaigns pick any live drop-enabled channel in the category (the behaviour PR #62 adds to 1.x). |
| Collection | Local claim rows, manual mark-collected, ghost-skip self-heal | Platform inventory is the source of truth. Reconcile after every claim attempt and on a schedule. One per-drop state: `eligible`, `accruing`, `claimable`, `claimed`, `blocked(reason)`. |
| API | `internal/api` renders HTML (6.9k LOC) | `internal/api` serves JSON + SSE only, plus the embedded SPA and its fallback route. |
| UI | `html/template` + HTMX, `app.css` | SvelteKit app in `web/` (new top-level folder), built into `internal/web/dist`. |

## Product model (from the mockups)

- **Pages:** Home, Games, Rewards, Settings. Settings sections in order: General, Accounts, Notifications, Health, Updates.
- **Chosen games** with a permanent priority order (Games page). The top game mines first.
- **Up next**: the planned queue of drops, one per account (the mockup's "first free account" line names the account instead). Reordering it on Home changes the order for this session only (until the miner restarts or the user hits "Reset to Games order").
- **Header pattern** on every page: page title, plain status text with a coloured dot, one amber chip when something needs the user (links to the fix). Red chip when the miner is unreachable.
- **Home**: KPIs (Total Minutes Watched all-time with today underneath, Drops claimed today, Accounts healthy), game cards (cards 1 and 2 large, 3 and below small; real Twitch box art with a patterned fallback), Up next, Claimed (last 10, grouped Today / Yesterday / Earlier).
- **Home states** to implement: first run, healthy, nothing live, offline, many accounts (summary row + "Show all").
- **Version** top right: white when latest, blue when an update exists; popover with release notes and `docker compose pull && docker compose up -d`.
- **Copy rule:** facts only, no reassurance filler ("Last scan 41 s ago", not "We check on every scan").
- **Dropped from 1.x** unless the builder finds a hard dependency: force-watch, per-account game lists (games are global), channel whitelists and force channels, manual mark-collected, proxy settings page, experimental settings page, priority-mode setting, canary settings UI (canary keeps running; its status shows in Health), cookie-paste and device-code Twitch login pages.

## Data and upgrade

- Same `miner.db`. v2.0.0 goose migrations only add tables and columns (chosen games + priority, per-drop state per account, claim log, minutes counter, reconcile timestamps). Old tables are dropped in a later release. Backup to `miner.db.pre-v2` before the first v2 migration.
- Migration maps 1.x global games to chosen games in their current priority order, then appends the union of `account_games` (deduped). Copies 1.x `claims` into the claim log and seeds the minutes counter from `progress`. Accounts and encrypted sessions are untouched, including non-TV Twitch sessions.
- No forced reset. Upgrade test: copy of a real 1.x DB boots on v2 with all accounts mining (non-TV Twitch included), history and minutes carried over, and no re-login.

## Constraints

- Single container plus on-demand Kick sidecars, as today. Required config stays `GRUB_MASTER_KEY` only.
- Go stays the only runtime. The SPA ships inside the binary.
- AGENTS.md rules that stay: sidecar stack is core; no `Client-Integrity` header; Twitch beacon 60s; base64 for outside data in chromedp `Evaluate`; sqlc comment rule; gofmt + CodeQL clean; changelog discipline.
- AGENTS.md rule that changes: replace "Stay Go + `html/template` + HTMX" with "UI is a SvelteKit static SPA embedded in the Go binary; the Go side serves JSON + SSE".
- Release gate: v2 touches accrual orchestration and claims, so v2.0.0 needs a live-drop check on Twitch (TV login) and Kick before tagging.

## Order of work

1. On master: merge PR #62, close PR #52 unmerged. GRUB-2 (Twitch watch-time not credited on TV tokens) starts here and runs alongside steps 2 to 6.
2. Branch `rebuild/v2`. Planner + runner on top of the kept `platform.Backend`, with unit tests against `platformtest` fakes.
3. Discovery and collection (reconcile, per-drop state).
4. JSON API + SSE event stream.
5. SvelteKit app: Home, Games, Rewards, Settings, states, i18n.
6. Migration from 1.x and upgrade test on a real DB copy.
7. Live-drop verification on Kick and on Twitch (TV token; needs GRUB-2 fixed), changelog, release notes, tag v2.0.0.

## Left to the builder

- Package names and layout for planner, runner, discovery, reconcile.
- API resource shapes and SSE event names.
- Scan and reconcile intervals (mockups say "every 5 min"; keep the Twitch beacon at 60s).
- Svelte component structure, CSS approach (port the mockup CSS tokens; fonts and polish come later with Impeccable).
- How the dev loop proxies Vite to the Go server.

## Prior art

- The 2026-06-22 SPA migration spec (since removed): an earlier Svelte + Vite strangler migration that kept the 1.x feature set. It did not land. v2 differs: ground-zero rebuild on a branch, new product model, features dropped.
- PR #52 pipeline v2 spec (`docs/superpowers/specs/2026-09-27-pipeline-v2-design.md` on `feat/pipeline-v2`): reference only for the reconcile idea.
