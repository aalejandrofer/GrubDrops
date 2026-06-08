# Notification Engine Redesign — Design

**Date:** 2026-06-08
**Status:** Approved (approach C — minimal targeted changes)

## Goal

Make Discord notifications look good and carry human-meaningful context, and
let the operator preview/test them from the settings page. Backlog item
"DISCORD NOTIFICATIONS REDESIGN". The previously-reported progress-toggle bug
is already fixed (`cmd/miner/main.go:123-136` rebuilds the per-kind filter from
saved toggles on every save) — confirmed, no further work there.

## Scope (locked with user)

- Embed redesign to the approved mockup (author line, drop-name title,
  platform-brand accent, progress bar + minutes, footer with campaign).
- Thread real current/required minutes into the notification payload.
- Platform-brand accent color (Twitch purple / Kick green; green on claim).
- Author line + footer in the built embed.
- Webhook `username` = "GrubDrops" + optional `avatar_url`.
- Settings page: live HTML preview of the embed **and** a "send test" button.

Out of scope: Console minute display (already real — see Non-Goals), an HTML
email-style template engine (YAGNI), author/footer icon images (Discord needs a
hosted URL; text is enough for v1).

## Non-Goals / Already Done

- **Console minutes are already real.** `watcher.Snapshot()`
  (`internal/watcher/watcher.go:392-418`) exposes `MinutesWatched`
  (`= w.lastProgressMin`) and `RequiredMinutes` (`= currentBenefit.RequiredMinutes`).
  The dashboard row (`dashboard_mining_columns.html:49,77-79`), the expand panel
  (`:101`) and the modal (`dashboard_account_modal.html:36`) all render these via
  `handlers_dashboard.go:548-550`. Rows show no progress only when an account is
  not in the `watching` state. No change needed.
- Progress-toggle bug — already fixed.

## Data available at notify time

`watcher.notifyFields()` (`internal/watcher/watcher.go:425-458`) already supplies:
`account`, `account_label`, `platform`, `game`, `campaign`, `drop`, `image`,
`channel`. Missing only: current/required minutes — both live at the emit sites
(`w.lastProgressMin`, `currentBenefit.RequiredMinutes`).

## Changes

### 1. Thread minutes (`internal/watcher/watcher.go`)

At the progress emit (`:1246`) and claim emit (`:1297`), pass minute counts via
the `extra` map:

```go
// progress
w.cfg.Notifier.Notify(ctx, notify.EventProgress, w.notifyFields(map[string]any{
    "cur_min": w.lastProgressMin,
    "req_min": reqMin, // currentBenefit.RequiredMinutes, read under lock
}))
```

Read `req_min` from `currentBenefit` under the existing mutex. Switch the raw
event strings (`"progress"`, `"claim"`) to the `notify.Event*` constants while
here (type safety; behaviourally identical).

### 2. Embed builder (`internal/notify/discord.go`)

Rewrite `buildEmbed(event, fields)` to the approved layout:

- **author.name** = `"{game} · {Platform}"` when `game` present (Platform
  title-cased: Twitch/Kick). Omit author block when no game.
- **title** = `drop` when present; else fall back to `titleFor(event)` (keeps
  auth/error/state events working).
- **color** = `colorFor(event, fields)`:
  - `EventClaim` → green `0x23A55A`
  - `EventError` → red `0xE74C3C`
  - otherwise by platform: `twitch` → `0x9146FF`, `kick` → `0x53FC18`
  - fallback gray `0x95A5A6`
- **fields** = `Account` + `Channel` (both inline, 2 columns). Channel keeps the
  existing platform deep-link markdown.
- **progress line** = a non-inline field rendered when `req_min` > 0:
  - name: `EventClaim` → `"✅ Claimed"`, else `"⏳ Mining"`
  - value: `"{cur}/{req} min  {bar}"` where `bar` is a 10-segment unicode bar
    (`▰`×filled + `▱`×rest) from `cur/req` (claim clamps to full).
- **thumbnail** = `image` (unchanged).
- **footer.text** = `"GrubDrops • {campaign}"` (drop the `• campaign` suffix
  when no campaign).
- Events with neither `drop` nor `game` (auth/error) keep the existing
  title + `descFor` fallback path.

Helper `progressBar(cur, req int) string` lives in this file; unit-tested.

### 3. Webhook payload + branding (`internal/notify/discord.go`)

`DiscordWebhook` gains `Username string` and `AvatarURL string`. `Notify` adds
them to the POST body when non-empty:

```go
payload := map[string]any{"embeds": []any{embed}}
if d.Username != "" { payload["username"] = d.Username }
if d.AvatarURL != "" { payload["avatar_url"] = d.AvatarURL }
```

`NewDiscordWebhook` gains the two values; `cmd/miner/main.go` `buildNotifier`
sets `Username = "GrubDrops"` and `AvatarURL` from a new setting
`settings:notify_avatar_url` (empty default → field omitted). The
`AccountRoutedNotifier` passes both through to per-account webhook clients
(`router.go` — extend `NewDiscordWebhook` call at `:52`).

### 4. Settings store (`internal/store/settings.go`)

Add `NotifyAvatarURL(ctx) (string, error)` + `SetNotifyAvatarURL(ctx, string)`
keyed `settings:notify_avatar_url`, mirroring the existing webhook-URL accessors.

### 5. Settings page (`internal/web/templates/settings.html`, `handlers_settings.go`)

- **Avatar URL input** in the Discord notifications section, wired through the
  existing settings POST (add `notify_avatar_url` to the save handler at
  `handlers_settings.go:166` area).
- **Live preview**: a static HTML replica of the embed (reuse the mockup markup
  from `.superpowers/brainstorm/.../embed-compare-v2.html`, scoped CSS) rendered
  inside the notifications section with sample data, so the operator sees the
  style. No server data; pure presentation.
- **Send-test button**: `POST /settings/notify-test`. New handler builds a
  representative sample event (`EventClaim`, fake game/drop/channel/minutes) and
  calls the current notifier so it routes to the configured global webhook.
  Returns an inline HTMX fragment: green "sent ✓" or red error text. Register the
  route alongside the other settings routes.

The handler needs access to the live notifier. `buildNotifier`/`indirectNotifier`
already exist in `main.go`; expose the current notifier to the settings handler
(pass the `*indirectNotifier` or a `func() notify.Notifier` getter into the API
handler struct).

## Testing

- `internal/notify/discord_test.go`:
  - `buildEmbed` table tests: progress (purple, ⏳, bar), claim (green, ✅, full
    bar, footer campaign), auth/error fallback (title + desc, no panic),
    raw-account-id fallback when `account_label` absent.
  - `progressBar`: 0/req, partial, full, req==0 guard.
  - `Notify` posts `username`/`avatar_url` only when set (httptest server
    captures the body).
- Settings test-event handler: httptest webhook server, assert one POST with a
  well-formed embed; assert error path returns the error fragment.
- `go build ./... && go test ./internal/notify/... ./internal/api/...`.

## Files touched

- `internal/watcher/watcher.go` — minutes into notify fields (2 emit sites).
- `internal/notify/discord.go` — embed rebuild, color-by-platform, bar helper,
  username/avatar payload.
- `internal/notify/router.go` — pass username/avatar through.
- `internal/store/settings.go` — avatar-url setting.
- `cmd/miner/main.go` — wire username/avatar; expose notifier to handlers.
- `internal/api/handlers_settings.go` — avatar save + `/settings/notify-test`.
- `internal/web/templates/settings.html` — avatar input, live preview, test button.
- tests as above.
