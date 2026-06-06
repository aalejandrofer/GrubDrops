# /drops page — plan for the FE agent (handoff)

Three changes, all in the /drops page (you own these files; the backend/Kick work
is mine and is done — Kick campaigns now flow through with `status`
active|upcoming|expired + `StartsAt`/`EndsAt`, same as Twitch).

Files: `internal/api/handlers_drops.go`, `internal/web/templates/drops.html`,
`internal/web/templates/_drops_table.html`.

---

## 1. Remove the redundant "DROP" word from rows
Both templates render `<em>{{.Platform}} · {{.Kind}}</em>` where `.Kind` is
`"drop"` | `"reward"`. "drop" is noise (everything here is a drop). Keep "reward"
(it's meaningful — one-click claim, no watch-time).

- `drops.html:80` and `_drops_table.html:25`: change `{{.Platform}} · {{.Kind}}`
  to show the kind ONLY when it's reward, e.g.:
  `{{.Platform}}{{if eq .Kind "reward"}} · reward{{end}}`
- Keep platform color consistency (purple=twitch, green=kick) if you're touching
  these rows — vars `--purple`/`--kick` (see history.html for the pattern).

## 2. BUG: drops appear in BOTH Past and Current
Root cause: `collectAll` (handlers_drops.go) builds PAST as
`ListPastCampaigns` (ended) **unioned with claim history**. A drop CLAIMED on a
still-ACTIVE campaign therefore shows up twice: the campaign row in CURRENT, and
the claim row in PAST. (See the comment ~line 280 "PAST also unions in claim
history…".)

Fix (pick one, prefer A):
- **A (recommended):** only union a claim into PAST when its campaign has ENDED
  (ends_at < now). Claims on still-current campaigns stay in CURRENT (optionally
  with a "claimed" marker on the row). This makes the tabs mutually exclusive.
- B: dedupe across tabs by (platform, campaign, benefit) — keep the row only in
  the tab matching the campaign's window.

Also confirm a campaign can't be returned by both `ListPastCampaigns` and
`ListCurrentCampaigns` (boundary: ends_at exactly == now). Use strict `<`/`>=`.

## 3. Show Kick UPCOMING campaigns
Kick supports upcoming campaigns (verified live: "Kick Off 2 - General Drops",
"Kick + Rust Wallpaper Pack", start Jun 11). The Kick backend now emits these
with `Status:"upcoming"` + `StartsAt`/`EndsAt`, and the watcher persists every
discovered campaign (whitelisted) via the CampaignPersister, so they land in the
`campaigns` table like Twitch's.

- Verify the Upcoming tab (`ListUpcomingCampaigns`, starts_at > now) picks up Kick
  rows — should "just work" once a Kick account is discovering. If Kick rows are
  missing, check that the discovery scraper / watcher persists Kick upcoming
  (status must be "upcoming", starts_at in the future).
- Kick discovery requires an enabled Kick account with a valid session; prod has
  `acc_89de4e08` enabled now.

---

## 4. NEW table: "Whitelisted — account not linked" (data is ready)
Backend is done (migration 0008): the `campaigns` table now has
`account_linked` (INTEGER 0/1) + `account_link_url` (TEXT), and the
List* queries `SELECT *` so the generated rows already expose
`AccountLinked` + `AccountLinkUrl`. Populated for both platforms:
- Twitch: `account_linked=0` when `isAccountConnected=false`.
- Kick: `account_linked=0` for `connect_url` campaigns the account isn't
  participating in (PUBG/Krafton not linked, etc.).

FE work:
- In `collectAll`, split the WHITELISTED **current** rows by `AccountLinked`:
  linked → the normal mineable list; `account_linked=0` → a NEW table below it,
  e.g. "Whitelisted · account not linked", showing the game/campaign + a
  "Connect account →" link to `AccountLinkUrl` (when non-empty).
- Carry `AccountLinked` + `AccountLinkUrl` onto `dropsRow` (add fields; the gen
  row has them now). The watcher already SKIPS these for mining, so this table
  is purely "here's what you'd mine if you linked the account."
- Keep the priority sort on the linked list only.

### Context you may want
- Whitelisted "Current" list is sorted by priority rank (gameRankUnion) — keep.
- Per-account add only (no global) — keep.
- Kick channel discovery is per-campaign (campaign.AllowedChannels) — not needed
  for the /drops page, just FYI.
