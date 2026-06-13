# Changelog

All notable changes to GrubDrops.

## [Unreleased]

_No unreleased changes._

## [1.0.3] — 2026-06-13

### Added

- **Per-account reload** — each row in the dashboard "Currently mining" cards
  now has a subtle reload arrow (↻) that restarts ONLY that account's watcher,
  leaving every other account running uninterrupted. New
  `POST /accounts/{id}/reload` endpoint (authed + CSRF) backed by the scheduler's
  targeted `ReloadAccount`, which cancels just that entry's per-account context,
  waits for it to exit, and respawns it under the long-lived base context — never
  the request context — so a finished HTTP request can't tear the rebuilt watcher
  down. The global "Reload all" button is unchanged.
- **arm64 miner image** — the miner is now published for `linux/arm64` as well
  as `linux/amd64`, so Raspberry Pi / ARM self-hosters pull a prebuilt image
  instead of building from source (pure-Go `modernc.org/sqlite`, no CGO, so it
  cross-compiles cleanly). The browser sidecar stays amd64-only: it bundles
  google-chrome-stable for the IVS codecs and Google ships no arm64 Linux Chrome,
  so Kick browser-watch still needs an amd64 host (Twitch drops run natively on
  ARM). README + release.yml document this.
- **Data-dir permission docs** — README and the example compose now explain that
  the miner runs as distroless nonroot (UID 65532) and a host-owned bind-mounted
  `./data` must be `chown`ed to `65532:65532` (or use a named volume), or SQLite
  can't write `miner.db` and login fails with "failed to persist session".

### Fixed

- **Friendlier "failed to persist session" hint (#15)** — when a Kick login's
  DB write fails with a permission/readonly/disk signature (the common
  host-owned `./data` on a nonroot container), the error shown now appends a
  hint to chown the data dir to `65532:65532` and points at the README, instead
  of just surfacing the raw SQLite error. The verify flow is unchanged.
- **"Invalid CSRF token" on plain-HTTP self-hosts (#15)** — every form POST
  failed on a plain-HTTP deployment (e.g. a Raspberry Pi at `http://pi:8080`).
  Root cause: `nosurf` v1.2 defaults its same-origin check to assume HTTPS
  (`isTLS` always true), so it built a `https://host` self-origin that never
  matched the browser-sent `http://host` Origin/Referer. The CSRF middleware
  now derives the request scheme from the actual transport — honoring
  `X-Forwarded-Proto` only when `GRUB_SECURE_COOKIES=1` (a declared HTTPS
  deployment behind a TLS-terminating proxy) and reporting plain HTTP
  otherwise. The same-origin requirement and the masked token cookie/form
  comparison are unchanged, so protection is not weakened. A failed check now
  logs a `csrf check failed` diagnostic and returns a hint naming the likely
  secure-cookie/scheme mismatch. README documents the `GRUB_SECURE_COOKIES`
  guidance.

## [1.0.2] — 2026-06-13

### Added

- **Account profile pictures** — each account now shows its real platform
  avatar (Twitch via `currentUser.profileImageURL` on `static-cdn.jtvnw.net`,
  embedded directly; Kick via the authed `/api/v1/user` `profile_pic`, served
  through the existing `/img/kick` proxy so Cloudflare doesn't 403 the
  hotlink). New `avatar_url` column on `accounts` (migration `0012`),
  `UpdateAccountAvatar` query, and a `platform.AvatarFetcher` backend
  capability. Avatars are backfilled on login and refreshed by the ~12h
  auth-health sweep — never on the per-tick hot path. Rendered in the dashboard
  mining rows, the account modal head, and the accounts list, each falling back
  to the existing letter circle when no avatar is set or the image fails to
  load (`onerror`).

### Changed

- **Button system redesign** — global refresh of the `.btn` (secondary) and
  `.btn.primary` pair for a cohesive, restrained hierarchy: secondary is now a
  quiet ghost (transparent, muted mono label, soft border) that lifts to full
  text + warmed accent border + faint tint on hover; primary keeps the accent
  fill with a hairline top highlight and lightens to `--accent-2` on hover.
  Wider mono tracking (0.14em), tactile `:active`, and a visible accent
  `:focus-visible` ring on both. Applies to all `.btn`/`.btn.sm`/`.btn.ghost`
  usages (page-head actions, account pages, alert CTAs, nav). CSS-only.

### Fixed

- **Reload stall after Kick re-login (P0)** — every watcher (Twitch + Kick)
  tore down and never resumed `watching` after a Kick re-login, requiring a
  container restart to recover. Root cause: the scheduler ran watchers under
  whatever context triggered a `Reload`, and the Kick login handler reloaded
  with the HTTP **request** context — cancelled the instant the handler
  returned its redirect, which cancelled every freshly-rebuilt watcher. The
  scheduler now runs watchers under a long-lived base context captured on the
  first start, so a reload triggered by a request context (Kick login, the
  /accounts apply button, link-override, settings reload) can never tear the
  roster down. The Kick login handler also now reloads under the root context,
  matching the Twitch handler.

## [1.0.1] — 2026-06-12

### Breaking

- **Helper CLI/GUI removed** — `cmd/grubdrops-helper` and `cmd/grubdrops-helper-gui`
  binaries, `internal/helper` package, `POST /helper/accounts/{id}/kick`,
  `GET /download/helper`, and the `GRUB_HELPER_DIR` env var are all gone.
  Kick cookie ingestion is now done via the **cookies.txt** flow: export with
  the "Get cookies.txt LOCALLY" (Chrome) or "cookies.txt" (Firefox) extension
  and paste or upload on the Kick authorize page. Remote users sign in via SSO
  first.

### Added

- **Release workflow** — `.github/workflows/release.yml` publishes
  `ghcr.io/aalejandrofer/grubdrops` and `ghcr.io/aalejandrofer/grubdrops-browser`
  images on `v*` tags.
- **cookies.txt Kick login** — parser, upload handler, and authorize-page template
  replacing the helper-binary path.

### Changed

- README rewritten deployment-first (Docker Compose quickstart, cookie-export
  instructions, environment variable reference).
- `cmd/kick-encrypt` one-shot ops tool deleted (superseded by cookies.txt form).

## [1.0.0] — 2026-06-07

First tagged release.

### Added
- **Auth-health agent** — periodic (12h) per-account auth probe (Twitch token /
  Kick cookies) plus a manual "Check auth" button on /accounts; ✓/✗ pill per account.
- **Manual "I've linked it" override** — break the Kick connect-deadlock (the
  drops/progress endpoint 403s until you've already earned). Mark a campaign
  linked and the watcher attempts it; the live progress check confirms.
- **Per-account connect chips** on /drops with **mineable-if-any** grouping: a
  campaign stays in the mineable list if ≥1 whitelisting account is linked;
  "account not linked" shows only campaigns no account can mine. Chips show
  only for accounts that whitelist the game.
- **Lazy item fetch** — non-whitelisted campaigns now load their benefits +
  end dates on demand (no more "No items recorded").
- **Inventory reconcile** — drops claimed manually (outside the bot) now show
  as collected.
- **Per-account targeted reload** — editing one account restarts only that
  account's watcher; whitelist/priority saves no longer reload everything.
  Confirm dialog on every reload.
- **AWAITING CONNECT** watcher state (distinct from idle/sleeping).
- **Kick image proxy** — reward images served via the utls transport
  (`ext.cdn.kick.com`), bypassing Cloudflare hotlink blocks.
- **GrubDrops logo** + SVG favicon + README.
- Discord notifications: rich embeds (drop image, game, channel, account handle).
- **Downloadable cookie helper** — pre-built macOS/Windows/Linux binaries baked
  into the image and served from the Kick login page; double-click runs an
  interactive prompt (Kick-only) instead of flashing a console and closing.
- **GitHub link** in the header.
- Template parse smoke test (`web.Templates()`) so a bad template fails CI
  instead of crash-looping in prod.

### Changed
- Channel selection now requires the stream to actually be playing the
  campaign's game (Twitch ACL + Kick) — no more wasted watch-time on wrong-game
  streams.
- Sleeping / awaiting-connect watchers self-rearm (re-discover every 5m) instead
  of dying until a manual reload.
- Inventory/progress poll relaxed 20s → 60s (PubSub is the real-time signal).
- /drops: tab filter now re-renders all panes; whitelist control moved inline;
  borderless item panel; boxed ✓ collection marks (orange ✗ for action-only).
- Discord verbosity toggles (claims/progress/auth/errors) are now honored.
- Module/binary/image renamed `dropsminer` → `grubdrops`.
- Kick login page rebuilt on the flat design system (dashed-rule sections,
  flat fields) with the download helper as the recommended path.

### Fixed
- **Account deletion now fully purges** — deleting an account explicitly removes
  its session, games, campaign links/priorities, progress, and claims inside one
  transaction before deleting the account row, instead of relying solely on
  `ON DELETE CASCADE` (which only fires when foreign-key enforcement is on for
  the live connection). A deleted account could survive and keep being loaded,
  device-code-polled, and idled on every boot. Hard-delete; no soft-delete column.
- **Discovery stall** — the discovery whitelist ignored the global games list,
  so every tick no-opped and campaigns went stale (looked like "Kick campaigns
  vanished after reload"). Now unions the global list.
- Twitch device-code: superseded orphan pollers that flooded the auth log.
- Stale empty "REWARD · — · —" history row filtered out.

### Notes
- Stack: Go + html/template/HTMX, SQLite (sqlc + goose), age-encrypted sessions.
- Twitch via Android device-code + GraphQL; Kick via utls Chrome-fingerprint
  (no browser, no cf_clearance).
