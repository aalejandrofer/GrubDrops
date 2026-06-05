# TDM ↔ dropsminer feature audit

Source: DevilXD/TwitchDropsMiner @ master (fetched 2026-06-05 via
raw.githubusercontent.com). Compared against
`internal/platform/twitch/*.go`, `internal/watcher/watcher.go`,
`internal/discovery/twitch.go`, and `internal/auth/browser/sidecar/*`.

Status legend: yes = full parity, partial = present but degraded /
missing fields, no = not implemented. Priority: P0 = drops will not
flow end-to-end without it, P1 = nice-to-have polish, P2 = future /
GUI-only.

## Parity table

| # | TDM feature | dropsminer | Priority |
|---|---|---|---|
| 1 | `device_id` bootstrap from `unique_id` cookie via GET `https://www.twitch.tv` (`twitch.py:367-369`) | partial — `client.go:135 bootstrapIdentity` is a no-op; we send a random 32-hex `X-Device-Id`. Works for the Android client profile because it never visits web, but if/when we route through the sidecar (web client) integrity will mismatch | P1 |
| 2 | PubSub WebSocket pool — wss://pubsub-edge.twitch.tv/v1, up to 50 topics/socket, `MAX_WEBSOCKETS=8`, PING every ~4 min (`websocket.py:154,193-195,307`) | no — we have no PubSub client at all. Watcher polls inventory every tick instead | **P0** |
| 3 | `user-drop-events.<user_id>` → `drop-progress`/`drop-claim` messages drive `process_drops` (`twitch.py:1150-1208`, `constants.py:500`) | no — we discover progress by polling `InventoryProgress` each heartbeat tick. Slow + bursty + race-prone | **P0** |
| 4 | `video-playback-by-id.<channel_id>` → `stream-up`/`stream-down`/`viewcount` drives `process_stream_state` (`twitch.py:1040-1062`, `constants.py:506`) | no — when a stream goes offline mid-mine, we keep heartbeating until next inventory poll | **P0** |
| 5 | `onsite-notifications.<user_id>` → `create-notification` re-triggers INVENTORY_FETCH on `user_drop_reward_reminder_notification` (`twitch.py:1209-1220`) | no | P1 |
| 6 | `dropCampaign.self.isAccountConnected` filter — only mine campaigns the user actually linked to the game (`inventory.py:346`) | yes — `campaigns.go:142, watcher.go:341-344` skip unlinked Twitch campaigns and log count | yes |
| 7 | `ViewerDropsDashboard` (Campaigns) for ENROLLED + `Drops_Page_ContentList` for OPEN/joinable campaigns | partial — we call `ViewerDropsDashboard` only. We never enumerate campaigns the user hasn't yet enrolled in, so the dashboard's "Available to join" is invisible. (TDM also only uses Campaigns, so this is GUI sugar) | P2 |
| 8 | Channel auto-switching — when watching channel goes offline OR a higher-priority game appears, `should_switch` + `bulk_check_online` re-pick (`twitch.py:1002-1019,1570-1620`) | partial — we re-evaluate only on next `pickCampaign` tick (every TickInterval). No "stream-down → switch immediately" path because we don't subscribe to `video-playback-by-id`. Mid-stream offline = wasted minutes until tick | **P0** |
| 9 | `bulk_check_online` — 20-at-a-time batched GQL to check live status across all watchable channels (`twitch.py:1570-1620`) | no — `channels.go:24-46` loops `OpGetStreamInfo` sequentially per allowed login. O(N) RTT. Fine for small allow-lists, painful for game-wide directory queries | P1 |
| 10 | `DirectoryPage_Game` query — list live drops-enabled streams for a game when no ACL (`twitch.py:1543-1569`, `constants.py:400-425`) | partial — `OpGameDirectory` is NOT in `ops.go`; when allow-list is empty we return zero streams (`campaigns.go:194-200` comment "future revision should fan out"). For unrestricted-channel campaigns we can't pick a stream at all | **P0** |
| 11 | OAuth device-code login + automatic refresh on `expires_in` (`twitch.py:344-417`) | yes — `auth.go` handles device flow, refresh, 60-day fallback for web client's `expires_in=0` | yes |
| 12 | Cookie-jar login fallback (`twitch.py:355-415`, restoring `auth-token` from on-disk jar) | partial — sidecar/browser_backend.go consumes a pasted cookie blob from `session.Cookies["twitch"]`. There is no captcha-fallback flow; if device-code login is blocked the user must paste cookies manually | P1 |
| 13 | Multi-account session refresh + rotation — each `Twitch` instance is single-account but settings drive priority. Our model is multi-account natively via `account_id` (`watcher.go:99, browser_backend.go:151-167`) | yes — better than TDM here; sidecar tab keyed by `accountID` | yes |
| 14 | Settings: priority list ordering (game order), exclude set, `priority_mode` (PRIORITY_ONLY/ENDING_SOONEST/LOW_AVBL_FIRST) (`settings.py:18-39`, `twitch.py:651-679`) | partial — per-account whitelist + `GameRank` ordering exists (`watcher.go:43-48,353-357`). No exclude set, no priority_mode = ENDING_SOONEST or LOW_AVBL_FIRST modes | P1 |
| 15 | `enable_badges_emotes` toggle — when off, drops whose only benefit type is BADGE or EMOTE are skipped (`inventory.py:30-40`) | no — we treat all benefits identically | P2 |
| 16 | `available_drops_check` toggle — verify a channel actually has the game's drops enabled before watching (`twitch.py:1599-1610` + `DropsHighlightService_AvailableDrops`) | partial — `OpAvailableDrops` is declared but never called. The DropsEnabled flag on `platform.Stream` is hard-coded `true` (`channels.go:42`) | P1 |
| 17 | SendEvents heartbeat — `sendSpadeEvents` mutation with `input.{data:b64(gzip(minified)),repository:"twilight",encoding:"GZIP_B64"}` (`channel.py:67-96`) | partial — envelope shape is correct (`watch.go:53-99`). BUT `BroadcastID`, `ChannelID`, `UserID`, `game`, `game_id` are NEVER populated — `watch.start` (`watch.go:37-45`) stores only `Channel` + `Token`. Twitch will accept the mutation and return statusCode=204 but progress will NOT advance because the broadcast/channel/user IDs are blank | **P0** |
| 18 | Spade-URL fallback path (`channel.py:312-345`) — non-GQL minute-watched POST extracted from streamer page HTML | no — TDM marks `_send_watch_spade` as "currently unused" too, so safe to skip | P2 |
| 19 | `get_active_campaign` — picks the campaign with the lowest `remaining_minutes` so we finish drops before they expire (`twitch.py:1525-1542`) | partial — we sort by `GameRank` (whitelist position) only, never by `remaining_minutes`. Two whitelisted drops at the same priority → arbitrary order, possibly miss a near-complete one | P1 |
| 20 | Inventory dedupe — `progress[].isClaimed=true` removes the drop from candidates so we don't re-mine (`inventory.py:64-67, twitch.py`) | yes — `watcher.go:279-284, 360-364` builds `claimed` map and skips | yes |
| 21 | `Inventory.dropCampaignsInProgress[].timeBasedDrops[].self.dropInstanceID` carries the claim ID we feed to `DropsPage_ClaimDropRewards` (`inventory.py:65, twitch.py:1163`) | partial — our inventory decode pulls `dropInstanceID` (`campaigns.go:99`) but `Progress` only exposes `BenefitID`/`MinutesWatched`/`Claimed`. Claim path (`claim.go:23`) uses the benefit's `b.ID` (which is `TimedDrop.id`, not the instance ID). TDM uses instance ID. If Twitch tightens this we'll start getting `INVALID_INSTANCE_ID` claim errors | **P0** |
| 22 | `DropCurrentSessionContext` poll after claim to confirm drop advanced before resuming heartbeat (`twitch.py:1180-1190`) | no — we `StopWatch` then go back to Idle, next tick re-discovers. Works but wastes the 4-8s window TDM uses to chain drops on the same stream | P1 |
| 23 | `ClaimCommunityPoints` (bonus channel points) (`constants.py:316-326`) | no — out of scope; cosmetic | P2 |
| 24 | Game catalog cache (`cache.py`) — image thumbnails keyed by perceptual hash | no — we don't render thumbs in the dashboard for non-claimed drops | P2 |
| 25 | Game slug resolver `DirectoryGameRedirect` (`constants.py:427-433`) | no — needed only if we call `DirectoryPage_Game`. Pair with #10 | P0 (with #10) |
| 26 | Maintenance task — periodic re-pick triggered by next campaign start/end timestamp (`twitch.py:1488-1500`) | partial — fixed `TickInterval` (default 1 min). No event-driven re-pick on campaign expiry | P1 |
| 27 | Exponential backoff on transient errors (`websocket.py:21, twitch.py:466`) | yes — `watcher.go:184-211` does 5s→5m backoff | yes |
| 28 | Discord notifications (claim/progress/state) | yes — `internal/notify/discord.go` (dropsminer-only addition; TDM uses tray notifications) | yes |

## P0 implementation notes

### #2 + #3 + #4 + #8 — PubSub WebSocket pool

Single biggest gap. DevilXD does NOT poll for progress; everything comes
in over `wss://pubsub-edge.twitch.tv/v1` as JSON frames. Without this,
every watcher round-trips `Inventory` GQL every tick (currently 60s),
which (a) wastes 1/minute integrity budget, (b) misses sub-minute
progress, and (c) leaves the watcher heartbeating to a dead stream
until the next tick.

- Add `internal/platform/twitch/pubsub.go`: `gorilla/websocket` client
  that dials `wss://pubsub-edge.twitch.tv/v1`, sends `LISTEN` frames
  with `data.topics` and `data.auth_token`, PINGs every 4 min, parses
  `MESSAGE` frames where `data.message` is a JSON string.
- Subscribe per-account on watcher start: `user-drop-events.<user_id>`,
  `onsite-notifications.<user_id>`, and one
  `video-playback-by-id.<channel_id>` for each candidate channel.
- Wire two callbacks into `watcher.Watcher`: `OnDropProgress(benefitID,
  cur, req)` updates `lastProgressMin` and short-circuits to claim;
  `OnStreamDown(channelID)` flips state back to `StatePickStream` so we
  switch immediately.
- Cap at 50 topics/socket per `WS_TOPICS_LIMIT`. One socket per account
  is plenty until we mine >50 channels concurrently.

### #10 + #25 — DirectoryPage_Game for unrestricted-channel campaigns

When `dropCampaign.allow.isEnabled == false` we currently return zero
streams (`campaigns.go:194-200`). Result: any "all streams of game X
qualify" campaign cannot be mined at all.

- Add `OpGameDirectory` + `OpSlugRedirect` to `ops.go` (hashes already
  in `/tmp/tdm_constants.py:400-425, 427-433`).
- New method `channels.listGameDirectory(ctx, sess, gameSlug, limit,
  dropsEnabled=true)` returning `[]platform.Stream` sorted by
  viewersCount desc.
- In `channels.listEligible`: when `allowedLogins` is empty, resolve
  the game slug (cache it in `Backend`) and call `listGameDirectory`
  with `systemFilters=["DROPS_ENABLED"]`.

### #17 — SendEvents heartbeat needs the real IDs

Today's `watch.go:37-45` stores only `Channel` + `Token`. Twitch silently
accepts `sendSpadeEvents` with blank `broadcast_id`/`channel_id`/
`user_id` (returns 204) but progress never advances. The watcher then
spins forever until the inventory poll happens to read 0 minutes.

- `watch.start`: call `OpGetStreamInfo` (already in `ops.go`) to fetch
  `user.stream.id` (broadcast) and `user.id` (channel). Also fetch
  `stream.game.{id,name}`. Store all of these on `watchInternal`.
- Resolve `UserID` once via a new `OpCurrentUser` (or reuse the value
  from `dropCampaign.self.user.id` if we surface it). Store on the
  Session-bound client and reuse.
- `heartbeat`: populate `broadcast_id`, `channel_id`, `channel`, `game`,
  `game_id`, `user_id`. Verify the response is `statusCode == 204`
  exactly — anything else means the payload was rejected.

### #21 — Claim with dropInstanceID, not TimedDrop.id

`DropsPage_ClaimDropRewards` expects the instance ID from
`Inventory.dropCampaignsInProgress[].timeBasedDrops[].self.dropInstanceID`,
not the drop's static id. We currently pass `b.ID` (which is the drop's
id) to `claim.go:23`. TDM does this correctly via `drop.update_claim`
(`twitch.py:1163`).

- Surface `DropInstanceID` on `platform.Progress` (already decoded at
  `campaigns.go:99`, just need to plumb through).
- Add `InstanceID string` to `platform.DropBenefit` (or pass it
  separately to `Backend.Claim`).
- `watcher.go:459`: look up the instance ID for the benefit ID from the
  most recent inventory result, pass it to `Claim`.
- `claim.go`: use `dropInstanceID` from the inventory progress row.
