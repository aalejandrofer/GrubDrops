# Operator chores — design

**Date:** 2026-08-04
**Status:** approved, pre-implementation
**Scope:** stop the miner needing to be babysat. Fire an alert the moment an
account's auth dies, keep Kick sessions alive longer, and let the operator take
a consistent DB snapshot from the UI.

This is sub-project **P1** of three. P2 (adoption / onboarding) and P3
(internals: splitting `watcher.go`, fat handlers, metrics) get their own
spec → plan → implementation cycles and are out of scope here.

## Problem

An account whose session expires silently stops mining, and nothing tells the
operator. On production, Kick account `acc_89de4e08` sat dead for days before
being noticed by hand.

The parts to detect this all exist and are wired to nothing:

- **`notify.EventAuth` has no producer.** It is declared
  (`internal/notify/notify.go:12`), has a Discord embed case
  (`internal/notify/discord.go:239`), and is registered in the verbosity filter
  (`cmd/miner/main.go:283`). A `grep` for `EventAuth` across all non-test Go
  finds those three declarations and **zero callers**. No auth notification has
  ever been sent.
- **`authcheck` cannot notify.** The component whose entire purpose is probing
  each account's auth (`internal/authcheck/authcheck.go`) holds no notifier. It
  runs hourly (`cmd/miner/main.go:580-581`, `GRUB_AUTHCHECK_INTERVAL`), persists a
  `Result{OK, CheckedAt, Msg}` into `kv` under `authcheck:<accountID>`, logs at
  info level, and stops there.
- **Two unrelated "needs auth" signals disagree.** The dashboard alert list is
  built from *scheduler state* `needs_auth`
  (`internal/api/handlers_dashboard.go:309-316`); the Accounts page shows the
  *authcheck `kv` result*. An account that is not actively watching can fail
  authcheck without ever flipping scheduler state, so the dashboard reads green
  while the account is dead.

Two secondary chores compound it:

- **Kick sessions cannot renew and rotated cookies are discarded.**
  `RefreshSession` returns its input unchanged
  (`internal/platform/kick/backend.go:352`, "Kick cookies don't refresh
  server-side"). The utls HTTP client has no cookie jar — no `Set-Cookie`
  capture exists anywhere in `internal/platform/kick`. The Kick sidecar only
  ever *sets* cookies (`internal/auth/browser/sidecar/kick.go:84`) and never
  reads them back, whereas the Twitch sidecar does round-trip them
  (`internal/auth/browser/sidecar/twitch.go:1303,1378`, commented "may have
  refreshed cookies"). So if Kick rotates `kick_session` on use, the fresh
  cookie is thrown away and the account rides the original until it expires.
- **No consistent DB snapshot.** `miner.db` sits in the operator's bind mount,
  but `cp` of a live SQLite database with an active WAL can capture a torn
  state.

## Non-goals

- **New notify channels.** Discord webhooks already exist, global
  (`settings:discord_webhook`) plus per-account routing
  (`internal/notify/router.go`). Adding ntfy/Telegram/SMTP is not part of P1 —
  the alert has a working channel the moment it has a producer.
- **noVNC headed Kick login.** Considered and deferred to its own sub-project.
  It needs noVNC in the sidecar image, ~4 GB RAM on ARM, and CDP Chrome already
  403s on the Kick API, so harvest-then-use is the only viable pattern. Too
  heavy to bet P1 on.
- **Scheduled auto-backup with rotation.** A manual snapshot button covers the
  need without a new scheduler entry, retention setting, disk-growth concern,
  or three-locale settings surface.
- **Changing the authcheck cadence.** Hourly stays hourly.
- **Fixing the same persistence gap on Twitch.** The Twitch sidecar already
  round-trips possibly-refreshed cookies
  (`internal/auth/browser/sidecar/twitch.go:1303,1378`) and those are equally
  never persisted at runtime, for the same reason. Twitch has a real
  refresh-token path and an `ExpiresAt`, so it degrades far more gracefully than
  Kick and is not the chore being fixed here. Worth its own follow-up; changing
  it would widen the live-verification surface to both platforms at once.

## Design

### 1. Edge-triggered auth notification

`authcheck.Checker` gains an optional `notify.Notifier`. A nil notifier keeps
today's behaviour exactly (persist + log, no send), so the miner still runs with
notifications unconfigured.

`persist()` (`internal/authcheck/authcheck.go:178`) is the detection point: it
already writes the new `Result`, so it reads the prior one via the existing
`Load` helper (`internal/authcheck/authcheck.go:187`) first and compares `OK`.

Transitions and what they send:

| prior | new | action |
|---|---|---|
| none (first ever) | `OK` | no send |
| none (first ever) | `fail` | send `EventAuth` |
| `OK` | `fail` | send `EventAuth` |
| `fail` | `fail` | **no send** |
| `fail` | `OK` | send `EventAuth` (recovery) |

Edge-triggering is the whole point. Level-triggering against an hourly sweep
would send 24 notifications a day per dead account — the same defect shape as
the Kick claim spam fixed in v1.3.11, and the reason the canary already
specifies "fail→fail does NOT re-fire" (`internal/notify/notify.go:18`).

The notify `fields` map carries `account` (the account ID), so the existing
`AccountRoutedNotifier` routes to a per-account webhook when one is configured
and falls back to the global otherwise — no routing work needed. Fields also
carry the platform, the account display name, and `Result.Msg` (already
truncated to 200 chars by `checkOne`) so the message says which account died
and why.

A send failure is logged and never affects the persisted `Result`; auth health
is the source of truth, notification is best-effort.

### 2. One expiry signal

The dashboard alert builder additionally consults the authcheck `kv` result, so
an account failing authcheck raises a dashboard alert even when scheduler state
has not flipped. The two signals are OR'd into the existing `needs_auth` alert
kind rather than adding a second kind — the operator needs one row per broken
account, not two, and the existing alert already carries the CTA and its
translations.

The CTA points at the account's login route, which for Kick is the
cookie-paste form. No new template or locale keys.

### 3. Kick session longevity

Capture rotated cookies rather than discarding them, mirroring the Twitch
pattern that already works:

- **utls HTTP client** (`internal/platform/kick/transport.go`): `httpDoer.do`
  already holds the `*http.Response` after `RoundTrip`
  (`transport.go:220`), so `resp.Cookies()` is available at that point with no
  new plumbing to the wire. But `do` returns only `([]byte, int, error)` and
  has many callers, so the capture must **not** change its signature. Instead
  `httpDoer` gains an `onCookies func([]*http.Cookie)` hook set at
  construction (`newHTTPDoer`, `transport.go:56`) and invoked from both `do`
  and `getRaw` when the response carries any `Set-Cookie`. Callers stay
  untouched; a nil hook is a no-op. The TLS fingerprint is unaffected — this
  reads response headers only.
- **Which cookies count as rotation.** Only the names the session actually
  authenticates with, as established by `cookieHeaderFor`
  (`transport.go:234`): `session_token` (the Sanctum bearer, sent whole
  including the `id|` prefix), `XSRF-TOKEN`, and `kick_session`. A `Set-Cookie`
  for any other name is ignored, so analytics or consent cookies never churn
  the stored session. Merge is per-name over `kickSession.Cookies`, preserving
  every cookie not mentioned in the response.
- **Kick sidecar** (`internal/auth/browser/sidecar/kick.go`): call
  `GetCookies` after the watch and round-trip them out over the existing
  `KickSession` proto message, which already has a `Cookies` field
  (`browser.pb.go:136`). No proto change.
- **`RefreshSession`** (`internal/platform/kick/backend.go:352`): return the
  freshest captured cookie instead of the input unchanged.
- **A runtime persist seam, which does not currently exist.** Capture alone
  changes nothing, because a rotated cookie held only in memory dies with the
  process. `store.SessionStore.Put` is called from exactly three places: the
  two login handlers (`handlers_login_twitch.go:136`,
  `handlers_login_twitch_cookie.go:135`) and the boot-time watcher-entry build
  (`cmd/miner/main.go:405`). That boot-time branch is additionally gated on
  `s.ExpiresAt.Before(time.Now())` **and** a non-empty `s.RefreshToken`
  (`main.go:393-397`) — a Kick session has neither, so it never fires for Kick.
  The comment at `internal/discovery/twitch.go:48` claims persisting a refreshed
  session is "the watcher's job", but the watcher never calls `Put` either.

  So the Kick backend gains a `SessionPersister func(accountID string, s
  platform.Session) error` field, wired in `cmd/miner/main.go` to
  `sessions.Put`, and invoked only when the merge actually changed a cookie
  value. The account ID is available: `internal/watcher/watcher.go:266` sets
  `cfg.Session.AccountID = cfg.AccountID`, so every session reaching the Kick
  backend from a watcher carries it. A session arriving with an empty
  `AccountID` is not persisted (nothing to key on) and is logged at debug.
  A nil `SessionPersister` is a no-op, keeping tests and any non-wired caller
  working.

This is correct behaviour whether or not Kick actually rotates — Twitch proves
the pattern, and discarding a server-issued cookie refresh is a bug in either
world. **If** Kick rotates, sessions live as long as the miner runs and the
chore disappears; if it does not, item 1 catches the death within the hour.

Which world we are in cannot be determined without a live valid Kick session
(production's is expired). That determination is a verification step, not a
design fork — the implementation is the same either way.

### 4. Snapshot download

Settings → Health gains a "Download snapshot" button. The handler runs
`VACUUM INTO` against a fresh path in the OS temp dir, streams the result as
`grubdrops-YYYYMMDD-HHMM.db` with `Content-Disposition: attachment`, and
removes the temp file afterwards including on the error and client-disconnect
paths. `VACUUM INTO` is consistent against a live WAL database, which is the
reason for the button over telling the operator to `cp`.

POST behind the existing CSRF middleware, same as every other mutating route,
even though it only reads — it is an admin-only data-egress action and belongs
on the same footing.

## Isolation

Each piece is independently testable and has one job:

- `authcheck` — decides *whether* auth is healthy and *whether the state
  changed*. Depends on the `notify.Notifier` interface, not on Discord.
- `notify` — unchanged. Already an interface with a Discord implementation, a
  router, and a noop.
- dashboard alert builder — reads two health sources, emits one alert list.
- Kick cookie capture — lives behind `RefreshSession` and the session store;
  callers are unaffected.
- snapshot handler — one route, one temp file, no shared state.

## Testing

| item | test |
|---|---|
| 1 | Fake notifier + in-memory DB. Drive `persist` through none→OK→fail→fail→OK and assert exactly two sends, with the right `account` field and no send on `fail→fail`. Assert nil notifier sends nothing and still persists. |
| 2 | Table test over (scheduler state, authcheck result) pairs; assert one alert per broken account and no duplicate rows when both signals fire. |
| 3 | Unit-test the cookie merge directly (rotated `session_token` replaces, unrelated names ignored, untouched cookies preserved, no-change returns not-changed). Assert `RefreshSession` returns the merged session, that a changed merge calls a fake `SessionPersister` exactly once with the right account ID, that an unchanged merge calls it zero times, and that an empty `AccountID` calls it zero times. Assert a nil persister does not panic. |
| 4 | Temp DB with known rows; assert the response is a non-empty SQLite file that opens and contains those rows, and that the temp file is gone afterwards. |

## Release gating

Per `CLAUDE.md`: item 3 touches the Kick watch path, so it holds in
`[Unreleased]` until confirmed against a live Kick session. Items 1, 2 and 4
touch notification, UI, and an admin route — none touch accrual or claim — so
they are taggable on a green build plus passing unit tests plus a green
transport canary.

Every item lands in `docs/CHANGELOG.md` under `[Unreleased]` as it is
committed.

## Prerequisite

Item 3's live verification needs a working Kick session. Production
`acc_89de4e08` is expired and must be re-logged-in via cookie paste before that
verification can run. Items 1, 2 and 4 have no such dependency and proceed
regardless — in fact item 1 is what prevents the next expiry going unnoticed.
