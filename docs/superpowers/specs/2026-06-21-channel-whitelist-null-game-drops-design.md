# Channel whitelist for null-game drops

Date: 2026-06-21
Status: approved
Related: GitHub issue #20 (Channel Whitelist)

## Problem

Some Kick drop campaigns have no game category. Live-verified 2026-06-21
against the prod endpoint (`GET https://web.kick.com/api/v1/drops/campaigns`,
status 200, 28 campaigns): the ~9 "Football Drop: *" campaigns and "ED'S DROP"
return `category: null` -> `Campaign.Game == ""`, while still carrying
`status: active`, watch-time rewards, and exactly one participating channel each
(e.g. Shifuza -> `adrianozendejas32`, Jungle -> `xqc`).

The whitelist is game-only. A null-game campaign fails the per-account game
whitelist, so it is never mined, even though everything needed to mine it
(channel + required minutes) is already in the payload.

Scraping is NOT the problem: the single global endpoint returns these campaigns,
and the watcher already persists them so they show in `/drops` (as a dead "no
game" row). The only gate is the mining filter.

## Root cause (single line)

`internal/watcher/watcher.go:818` filters every discovered campaign by
`w.cfg.AllowGame(c.Game)`. For a null-game campaign `AllowGame("")` is false, so
it is dropped before the mining loop. The Kick backend already lets null-game
campaigns through (`backend.go:345` guards the filter with `c.Game != ""`), and
they arrive with `AllowedChannels` populated.

The fix is therefore: add an OR channel-match to that filter, plus the storage
and UI to let a user opt an account into a channel.

## Decisions

- Null-game drops do NOT auto-mine. Manual opt-in **per account** (prevents
  accounts scattering across ~10 concurrent Football drops).
- Whitelist key = **channel** (account mines whatever drop that channel serves,
  now or future). Matches issue #20 wording.
- Opt-in lives on the existing `/drops` per-row WHITELIST+ control, surfaced in a
  new dedicated section; plus an editable channel list in account settings.
- Per-account only in the new section (no `★ Global` channel list).
- Flat channel list (no rank / drag reorder).
- B (force-watch queue) and C (group Discoverable by game) are out of scope.

## Design

### 1. Data model

New migration (next free number is **0013**)
`internal/store/migrations/0013_account_channels.sql`, mirroring
`account_games`:

```sql
CREATE TABLE account_channels (
  account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  channel    TEXT NOT NULL,            -- channel slug, lowercased
  rank       INTEGER NOT NULL,         -- reserved; always 0 for now (no UI rank)
  PRIMARY KEY (account_id, channel)
);
CREATE INDEX idx_account_channels_acct ON account_channels(account_id);
```

Platform is implied by the owning account (one account = one platform), so no
platform column.

**Channel persistence (no new column):** persisted campaigns currently store
`raw_json = "{}"` and drop their channels. The null-game `/drops` row needs the
campaign's channel(s) for the WHITELIST+ form. Reuse the existing unused
`raw_json` column: `internal/store/campaign_persister.go` marshals
`{"allowed_channels": [...]}` into `raw_json` instead of `"{}"`, and
`handlers_drops.go` parses it back for null-game rows. This avoids a campaigns
migration and a `UpsertCampaign` signature change. If the current/past/upcoming
campaign list queries do not already SELECT `raw_json`, add it (query-only sqlc
regen).

New `internal/store/queries/channels.sql` (mirror `games.sql`), no `?` or
parentheses in comments per the sqlc footgun rule:

- `ListAccountChannels :many` — channels for an account
- `AddAccountChannel :exec` — upsert (account_id, channel, rank)
- `RemoveAccountChannel :exec` — delete one (account_id, channel)
- `ClearAccountChannels :exec` — delete all for an account

### 2. Watcher matching

- `cmd/miner/main.go`: new `loadAccountChannels(ctx, q, accountID) (func([]string) bool, error)`
  returning an `allowChannel` closure: true if any element of a campaign's
  `AllowedChannels` (lowercased, trimmed) is in the account's channel set.
  Empty set -> returns nil closure (no-op).
- `internal/watcher/watcher.go` `Config` gains
  `AllowChannel func(channels []string) bool`.
- The filter at `watcher.go:818` becomes:

  ```go
  if w.cfg.AllowGame(c.Game) ||
     (w.cfg.AllowChannel != nil && w.cfg.AllowChannel(c.AllowedChannels)) {
      whitelisted = append(whitelisted, c)
  }
  ```

  (Guard against `AllowGame == nil` is unchanged: the existing `if w.cfg.AllowGame != nil`
  branch wraps the loop; when both predicates are nil the existing
  "mine everything" fallback applies.)
- Wire `AllowChannel: allowChannel` in `watcher.New(watcher.Config{...})` in
  `cmd/miner/main.go` next to `AllowGame`.
- Sorting: channel-matched null-game campaigns get `GameRank("")` (max), so they
  sort last among matched campaigns and fall into the restricted partition
  (they have `AllowedChannels`). Acceptable for MVP.
- No new watch path: a matched campaign flows into the existing stream-pick
  (`ListEligibleChannels` uses the campaign's channels) and watch transport.

### 3. /drops UI — new section

In `internal/web/templates/_drops_table.html`, add a new `<section>` titled
**"Discoverable — Null Game"** BETWEEN the "Whitelisted — account not linked"
section (currently ends line 105) and the "Discoverable — not whitelisted"
section (currently starts line 107). Render only `{{if .NullGameRows}}`.

Page builder in `internal/api/handlers_drops.go`: when assembling the table
rows, route **active** campaigns with empty `Game` into a new `NullGameRows`
slice instead of `UnlistedRows`. This also removes the dead "no game" label
path at `_drops_table.html:130` (those rows now live in the new section).

Each null-game row reuses the account `<select>` + **WHITELIST +** button, but
the form posts to a new endpoint:

- `POST /drops/whitelist/channel` with `account_id` and the campaign's
  channel(s). Handler `addChannelWhitelist` (mirror `addWhitelist`): for each
  channel in the campaign's `AllowedChannels`, upsert into `account_channels`
  for that account; reload scheduler; redirect to `/drops` preserving tab.
- No `★ Global` option in this section.

The section only ever has rows when active null-game campaigns exist, satisfying
"only show section if null-game drops are open."

### 4. Account settings editor

In `internal/web/templates/accounts_detail.html`, add a **Channels** block
mirroring the games block: list current whitelisted channels each with a remove
button, plus an add-by-name form. Routes:

- `POST /accounts/{id}/channels/add` — `addChannel` (upsert one channel)
- `POST /accounts/{id}/channels/remove` — `removeChannel` (delete one channel)

No drag/rank UI. Register routes in `internal/api/server.go` next to the
`/accounts/{id}/games*` routes. The page handler loads `ListAccountChannels`
into the template data.

### 5. i18n

Add en + zh strings: section header (`drops_table.null_game`), its meta/empty
text, and the account-settings channel labels (add/remove/placeholder/section
title). Follow the existing `i18n.T` key pattern.

### 6. Tests (TDD)

- `loadAccountChannels` / `allowChannel`: matches case-insensitively, handles
  empty set (nil closure), no false match on empty channel list.
- Watcher: a null-game campaign is mined iff one of its channels is whitelisted;
  a null-game campaign with no matching channel is skipped; game-whitelisted
  campaigns still pass (no regression).
- `addChannelWhitelist` handler: posting account_id + a null-game campaign's
  channel inserts the expected `account_channels` rows and reloads.
- `addChannel` / `removeChannel` account-settings handlers.
- Query test for the new channels queries.

### Release gate

This changes campaign **selection**, not how watch-time accrues or how a drop is
claimed (watch transport unchanged). Per project rules it would qualify for the
default gate, but live Football (null-game) drops exist right now, so verify
against a live null-game drop before tagging — stronger and cheap. Log every
change under `docs/CHANGELOG.md` `[Unreleased]` as work lands.

## Files touched

- `internal/store/migrations/0005_account_channels.sql` (new)
- `internal/store/queries/channels.sql` (new) + sqlc regen
- `cmd/miner/main.go` (`loadAccountChannels`, wire `AllowChannel`)
- `internal/watcher/watcher.go` (`Config.AllowChannel`, filter OR)
- `internal/api/handlers_drops.go` (`NullGameRows`, `addChannelWhitelist`)
- `internal/api/handlers_accounts.go` (`addChannel`, `removeChannel`, load channels)
- `internal/api/server.go` (route registrations)
- `internal/web/templates/_drops_table.html` (new section)
- `internal/web/templates/accounts_detail.html` (channels block)
- i18n en/zh resource files
- tests across the above
- `docs/CHANGELOG.md` `[Unreleased]`

## Out of scope (backlog)

- B: EXPERIMENTAL force-watch task queue (platform + channel + minutes +
  accounts -> queue, watch directly).
- C: group `/drops` Discoverable list by game to shorten it.
