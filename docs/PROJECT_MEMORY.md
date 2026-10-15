# Portable project memory

Consolidated 2026-10-14 from 90 local assistant memory files. This is a curated
record of useful decisions, lessons and outstanding work, not a dump of sessions
or credentials. September notes describe later work than the old laptop's main
`feat/startup-preflight` checkout. Confirm implementation against the selected
branch; old plans and memory sometimes contradict newer decisions.

## Verified remote snapshot (2026-10-14)

Remote pipeline branch at `89bc010` already records v2 as default, watch-to-enroll,
parallel account reload, game-ID matching, catalog TTL eviction, reduced Kick idle
logs, block-aware claim notifications and channel flip-flop fixes in its changelog.
These supersede the old backlog items below: review and validate them, rather than
reimplementing them. Its AGENTS.md/CLAUDE.md already allow Client-Integrity only
for a web-client profile while forbidding it on Android/TV. That exception is
branch context; the user-provided session rule still forbids adding the header.

## Latest recorded direction (2026-09-30)

- Pipeline v2 core and other non-UI work target **1.4.5**. V2 should become the
  default with `GRUB_PIPELINE=v1` and per-account overrides as legacy fallback.
- UI redesign targets **2.0.0** and is parked while 1.4.5 is finished.
- Watch-to-enroll for TV-only Twitch installs belongs in 1.4.5: briefly watch a
  drops-enabled channel for each selected game so Inventory reveals campaigns.
- Web-profile browser login was research in the memory snapshot. Do not change Android/TV auth behavior based
  on that research. A renewal feasibility spike was proposed before a full port; check newer branch work.

## Pipeline v2 and verification

PR #52 / `feat/pipeline-v2` introduced campaign/progress/claim/inventory redesign,
originally opt-in via `GRUB_PIPELINE=v2` or KV `pipeline_override:<account>=v2`.
The old v1 watcher remains the fallback. Core spec and plan should be present on
that branch under `docs/superpowers/{specs,plans}/2026-09-27-pipeline-v2-*`.
An ignored SDD progress ledger lived under `.superpowers/sdd/` in the old worktree;
check any preserved ledger against code rather than assuming all tasks passed.

Recorded live proof: TV-client Twitch v2 accrual near 1:1, reconciliation and a
real claim succeeded on staging on 2026-09-28. Kick claimed-reward retention probe
passed. Android details may return `self=nil` for already-claimed campaigns;
second-stall claim probe was added, and empty claim status must mean failure.
Still owed in the latest pipeline notes: live Kick accrual and Android Twitch
accrual on v2. Unit tests alone cannot satisfy the accrual/claim release gate.
The recorded `internal/canary` `TestKickProbe_OK` race was reproduced and fixed
on 2026-10-15 by synchronizing the fake server frame log. The full canary
package passed five runs under `-race`; build, unit tests and vet also passed
after syncing the pipeline branch with master. Live verification remains open.

Latest deployment preference supersedes the older staging-only rule: the user
allowed production to double as the v2 test environment. Stop its Watchtower
container during branch-image tests and restart afterwards, otherwise it may
replace the image with registry `latest`. Coordinate runtime testing with the
user; this handoff does not perform deployment. If using staging, tear it down
when idle to avoid duplicate mining and token conflicts. Do not run concurrent
watchers for the same account across production and staging.

## Follow-up backlog (verify before editing)

1. Scheduler reload is serial and runs inline on account apply/settings save.
   Session retries plus auth verification can add several seconds per failing
   account. Consider bounded concurrent builders, capped retry budgets or a
   background reload with clear status.
2. TV AvailableDrops returns only one campaign per channel. A global badge/emote
   campaign can mask game campaigns for weeks. Shared catalog helps only if an
   Android account exists in the same install; TV-only watch-to-enroll remains.
3. V2 channel flip-flop: after a 10-minute hold, a higher-ranked channel is picked,
   then a 5-minute live refresh removes it and triggers a switch back. Retain or
   probe the current channel while it is still live.
4. V2 catalog: match game ID before slug, expire stale sources by TTL, log shared
   merges at INFO only when nonempty, and cover Publish/snapshot concurrency.
5. V2 PubSub with Required==0 marks claimable directly and bypasses user-skip or
   unlinked blocks. Route through a block-aware helper; revisit vestigial retry.
6. Kick idle logs repeatedly cycle pick-stream/pick-campaign/sleep every ~10s
   with no live channels; reduce churn.
7. UI 2.0.0: hierarchy mining status → collected items → fix problems → change
   targets. Reduce Save forms and manual Apply/Reload by automatically applying
   edits. Open question: where to put roughly 16 operational settings.
8. Older backlog includes per-account proxies, dashboard/filter cleanup and
   heartbeat locked at 60s. Reassess against current code before reviving these.

## Recorded releases and regressions

- v1.4.0: TV-client device login, per-session ClientID, channel-first discovery
  using AvailableDrops + Inventory, and null claim treated as failure (PR #53).
  Legacy Android device-client new login was rejected by Twitch in September;
  existing Android sessions could still mine. Recheck platform behavior live.
- v1.4.1: Kick expiry handled by auth health instead of a hard 7-day timestamp;
  session load/refresh retries; keep refreshed session when persistence fails
  (PR #54). Canary evidence is in database KV, not necessarily logs.
- v1.4.2: if refresh fails but VerifyAuth accepts the old session, keep mining
  rather than idle (PR #56). When that token truly dies, re-login is required.
- v1.3.12: auth notifications gained a producer; dashboard combines stored auth
  health and watcher signals while excluding disabled accounts; safe DB snapshot.
- v1.3.9–1.3.10: Twitch GQL SendEvents stopped earning credit; use Spade beacon
  POST, plain-base64 `data=`, no gzip, 204 response. Pin scraped beacon destination
  to Twitch-owned hosts before sending OAuth credentials. Persist ghost-skips
  but let them self-heal.
- v1.3.11: link-required Kick claim failures should not spam retries; valid age
  keygen command and quoted Compose environment error expressions shipped.

## Durable engineering lessons

- Browser sidecars are core. Never apply old cleanup suggestions that delete
  browser-sidecar, dockerctl, browser auth, proto/buf or Twitch BrowserBackend.
- Go + html/template + HTMX is final. React/Vite/Svelte/SPA migration plans are
  abandoned historical artifacts, even if they still exist in docs.
- Kick HTTP API detection/claims need Chrome TLS fingerprinting (utls). CDP
  browser auth API calls historically hit 403. IVS playback needs real Chromium;
  LightPanda lacks the MSE support needed for that path. Optional WS accrual
  experiments do not justify removing IVS fallback. Keep one watch per account.
- Twitch credit interval stays 60s; 120s earns roughly half the credit.
- Never delete untracked historical claims during pruning. A June incident
  deleted real claim rows; prune only tracked, in-progress, unclaimed entries.
- Drop tiers can share RewardID; sort tiers by required minutes and avoid
  treating a shared reward ID as proof that every tier was collected.
- Encode untrusted values as base64 JSON and decode with TextDecoder inside
  chromedp Evaluate scripts. Never concatenate outside-controlled JSON/raw text.
- SQL query comments must not contain question marks or parentheses (sqlc bug).
- Add user-facing strings to every locale, keeping equal key sets.
- Always build and test contributed changes before extending them.
- Docker dependency vulnerability triage must distinguish client imports from
  unused daemon/CLI code; investigate the actual reachable path.
- Check the exact release workflow run and resulting digest after tagging;
  watching an earlier successful run is not proof of the new image.
- Release titles are bare versions. Notes use concise human language with emoji
  Added/Changed/Fixed/Removed sections and bold subjects, without em/en dashes.

## References and navigation

README: configuration and deploy instructions. AGENTS.md: implementation rules.
CLAUDE.md: release gates. CHANGELOG: chronological record. Existing specs/plans
are historical context; the selected branch and newest decisions govern.
DevilXD/TwitchDropsMiner is a useful Twitch reference; upstream reports #1165 and
#1174 explain TV discovery masking. Reverify external claims before relying on
old research. No public follow-up issues or comments without explicit approval.
