# Changelog

All notable changes to GrubDrops.

## [Unreleased]

### Added

- **Experimental settings tab** — the Kick watch-path toggle moved out of General
  into its own "Experimental" tab (after Accounts) at `/settings/experimental`.
- **Kick watch mode "WS, fall back to Chrome"** — a third option that runs the
  WebSocket path first and, if the WS connection dies (exhausts reconnects),
  falls back to the Chrome sidecar for that account on the next watch. The
  dashboard KICK header shows a `WS→Chrome` bubble for this mode.

### Changed

- **More legible settings row labels** — the key labels (tick interval, discovery
  interval, …) are now full-contrast and semibold instead of dim grey.
- **Settings → Status lists all Kick sidecars** — the single "sidecar" row is now
  "sidecars" and lists every per-account sidecar address, not just one.
- **Per-account Kick watch-path tag** — each Kick row on the dashboard now shows
  its *live* accrual path (WS or Chrome) instead of a single column-header pill,
  so auto-mode fallback is visible per account. The header pill was removed.
- **Accounts moved under Settings** — the accounts page is now `/settings/accounts`
  (with `/accounts` kept as an alias) and shares the unified settings subnav, so
  the Experimental tab shows there too.
- **Telemetry row reworked + a 6th tile** — the band now fills its 6-column grid:
  Watch time, Drops claimed, **Claimed today** (since midnight), Active campaigns
  (broadcasting on your whitelist), Drops collected (`X/Y`), Next claim. Scope
  labels make campaigns-vs-drops unambiguous.

### Fixed

- **No more false "session expired or never authenticated"** — an account that was
  fully authenticated but simply had no games whitelisted (and several other
  non-auth idle reasons) was reported by the scheduler as `needs_auth`, so the
  dashboard showed the alarming "session expired" banner. Idle entries now report
  their real reason; an authed-but-no-games account surfaces a distinct
  `no_games` state ("no games yet" / "Add games →") instead of an auth error.
  This was the headline "WS mode on a Pi looks broken but isn't" complaint.
- **Cold-start whitelist trap on `/drops`** — discovery only crawls whitelisted
  games, so a fresh install with an empty whitelist left `/drops` silently empty
  with no row to click "whitelist +" on. The page now shows a bootstrap CTA
  ("No games whitelisted yet… Add a game →") linking to the priority list when no
  game is whitelisted anywhere.
- **Settings saves no longer report false success** — General, Notifications,
  Priority-mode and Experimental tabs swallowed DB write errors and always flashed
  "saved", so a failed write looked successful. These writes are now checked and
  surface the error instead of a misleading success.
- **NEXT CLAIM now tracks the closest drop live** — the telemetry band was static
  and only updated on page load; it now polls every 10s, so NEXT CLAIM reflects
  the current nearest ETA across all accounts instead of freezing.
- **Dashboard row no longer overlaps the state pill** — long activity/campaign
  text now truncates with an ellipsis at the column edge (flex children were
  keeping their full width and sliding under the pill).
- **Whitelisting a multi-word game** no longer fails with "UNIQUE constraint
  failed: games.slug" — the add-game handlers now use the canonical game id.

### Removed

- Redundant helper text on the settings pages ("Applies live to all accounts.",
  "Fallback when account list empty").

## [1.0.3-ws] — 2026-06-13

### Changed

- **Version shown in Settings now auto-tracks the release tag.** The displayed
  version was a hand-maintained `GRUB_VERSION` env var that nobody bumped, so
  Settings still read `1.0.0` after 1.0.1/1.0.2/1.0.3 shipped. The release build
  now injects the git tag into the binary at build time (`-ldflags -X
  main.version`), so every tagged image reports its own version; `GRUB_VERSION`
  remains a fallback for plain source/dev builds.

### Added

- **Kick pure-WebSocket watch path (experimental)** — Kick drop watch-time can
  now accrue over a pure-WebSocket viewer presence with **no browser / no IVS
  video** (`internal/platform/kick/wswatch.go`): a utls Chrome-fingerprinted wss
  connection that sends a periodic `channel_handshake` (~12s) plus a
  `tracking.user.watch.livestream` `user_event` (~60s). Live-verified to accrue
  (+8 watch-minutes over 10 min, zero browser, 2026-06-13). Selected via the new
  **Experimental** card under Settings → General — **Chrome sidecar** (default,
  IVS `<video>`) or **WebSocket only** — stored in `settings:kick_watch_mode`,
  read by the miner at startup (reload to switch). The two are mutually
  exclusive (the server credits one active watch per account). The dashboard
  KICK column header shows a **WS / Chrome** bubble for the active path. The
  Chrome sidecar remains the default and the only verified-stable path; the WS
  mode is opt-in. Gotcha baked in: `livestream_id` must be sent as a JSON
  **number** — the server silently doesn't credit a stringified id.

## [1.0.3] — 2026-06-13

### Changed

- **Lower-bandwidth Kick playback (#15)** — the browser sidecar now caps the
  watch tab's bandwidth and pins the IVS player to its lowest rendition. Drop
  watch-time only needs the stream alive, not a clean picture, so this cuts the
  per-sidecar network + CPU/decode load (and on a small ARM box, lets more
  sidecars run at once).

### Added

- **Per-account reload** — each row in the dashboard "Currently mining" cards
  now has a subtle reload arrow (↻) that restarts ONLY that account's watcher,
  leaving every other account running uninterrupted. New
  `POST /accounts/{id}/reload` endpoint (authed + CSRF) backed by the scheduler's
  targeted `ReloadAccount`, which cancels just that entry's per-account context,
  waits for it to exit, and respawns it under the long-lived base context — never
  the request context — so a finished HTTP request can't tear the rebuilt watcher
  down. The global "Reload all" button is unchanged.
- **arm64 images — miner _and_ Kick browser sidecar** — both images now publish
  for `linux/arm64` as well as `linux/amd64`. The miner is pure-Go
  (`modernc.org/sqlite`, no CGO) so it cross-compiles cleanly. The browser
  sidecar's Dockerfile now picks its browser per arch: amd64 keeps
  google-chrome-stable (the verified path; Google ships no arm64 Linux Chrome),
  arm64 installs Debian's `chromium`, which is built against system FFmpeg and so
  still carries the H.264/AAC codecs that decode Kick's IVS stream. This means
  **Kick browser-watch now runs on Raspberry Pi / ARM** — user-confirmed working,
  though the arm64 sidecar is resource heavy (~4 GB RAM per live sidecar, ~2
  concurrent on a small box). README + release.yml document the arch split.
- **Data-dir permission docs** — README and the example compose now explain that
  the miner runs as distroless nonroot (UID 65532) and a host-owned bind-mounted
  `./data` must be `chown`ed to `65532:65532` (or use a named volume), or SQLite
  can't write `miner.db` and login fails with "failed to persist session".

### Fixed

- **Account edit now takes effect immediately** — editing an account
  (enable/disable, label, webhook) already kicked a per-account reload, but ran
  it on the request context, which cancels the moment the handler redirects — so
  the reload tore the watcher down without rebuilding it, and a just-disabled
  account kept mining until a manual reload. The edit handler now reloads under
  the long-lived root context (matching the per-account reload button), so the
  change applies at once.
- **Accounts list layout after avatars** — adding the avatar cell pushed the
  display name into the 10px bullet column, where it overflowed and floated in
  the middle of the row. The accounts list now has its own grid track
  (avatar · name · ● · platform/links · state); the shared event-row grid is
  untouched.
- **README i18n** — added full Simplified Chinese (`README.zh-CN.md`) and
  Spanish (`README.es.md`) translations with a language switcher atop all three
  (addresses #15's localisation ask).
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
