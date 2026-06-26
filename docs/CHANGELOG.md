# Changelog

All notable changes to GrubDrops.

## [Unreleased]

## [1.3.3] — 2026-06-26

### Fixed

- **New campaigns granting an item you already own are mined again.** Twitch
  reuses the same reward item across campaigns and seasons, so when a fresh
  campaign (e.g. "R6S S1 2026 6") offered the same Esports Pack as the ended
  one before it, the watcher saw the item in your inventory and instantly
  marked all of its drops collected without watching — and skipped mining them.
  Claim state is now read only from Twitch's per-drop status, never from
  whether you own the reward item, so each campaign's drops are mined and
  claimed on their own merits. Also fixes the same under-mining on reused
  Rocket League rewards. (#24)
- **False "collected" marks self-heal while a drop is in progress.** When a
  drop is actively in your in-progress drops and Twitch reports it unclaimed, a
  leftover collected mark from the bug above is cleared. This only acts on drops
  Twitch is currently tracking as in-progress, so it never removes a real claim
  that has already left that list. (#24)

## [1.3.2] — 2026-06-24

### Added

- **Manually mark a drop as uncollected.** Click a COLLECTED mark on the
  `/drops` item list and confirm to clear it — a backup escape hatch if the
  self-heal ever misses a stale mark. Best-effort: if the platform still
  reports the drop as owned, it gets re-marked on the next discovery cycle. (#24)
- **Info filter on the live-events feed.** Uncategorized info events had no
  matching filter chip, so they only showed under "all". Added an "info" chip
  between state and discovery.

### Changed

- **Successful Twitch GQL responses dropped to debug.** Every successful API
  call was logged at info, and the per-channel live-check fan-out alone is
  hundreds per minute, which drowned the live-events feed. Every failure mode
  (5xx, 429, integrity, decode, application error, partial) is still logged
  loudly, so the feed loses no signal.

### Fixed

- **Stale "collected" marks now self-heal.** Drops wrongly marked collected by
  the pre-v1.3.1 shared-reward bug are cleared automatically: each discovery
  cycle, the reconcile pass removes any claim record for a drop that inventory
  still reports as in-progress and unclaimed. No manual database cleanup
  needed, and it corrects accounts that already had the bad marks. (#24)

## [1.3.1] — 2026-06-24

### Fixed

- **Multi-tier campaigns with the same reward now mine every tier.** When a
  Twitch campaign granted the same item at escalating watch-time thresholds
  (e.g. 60m / 180m / 360m / 540m), claiming the first tier marked its reward
  as owned, and the watcher then treated all the higher tiers as claimed too —
  going idle and showing every tier as collected when only one item was
  received. The owned-reward skip now applies only to drops that have left the
  in-progress inventory, so live tiers sharing a reward keep mining. (#24)

### Changed

- **Tiers within a campaign are mined lowest-required-minutes first**, so a
  claim lands at the earliest threshold and each higher tier builds on the
  watch-time already banked.

## [1.3.0] — 2026-06-22

### Added

- **Priority is now a top-level nav item** (`/priority`), moved out of Settings
  and placed between Drops and History.
- **Toast notifications** — actions (whitelist, force-watch, link override,
  settings saves) now confirm with a bottom-right toast instead of an
  easy-to-miss inline banner.
- **Channel-points force-watch (per account).** When an account has no drops
  to mine, it can keep watching configured channels 24/7 to farm channel
  points. Managed on the account page (toggle + channel list); lowest
  priority, so a live whitelisted drop always preempts it. Warns that 24/7
  watching may be flagged by Twitch/Kick.
- **`GRUB_AUTHBYPASS` env flag** — disables all auth (staging/dev only) so the
  UI is reachable without login behind a proxy. Logs a loud startup warning.
- **Channel whitelist for category-less drops.** Opt an account into a
  Kick/Twitch channel so drops with no game category (e.g. Kick Football
  drops) get mined — they previously fell through the game-only whitelist and
  were never picked. New "Discoverable — no game category" section on `/drops`
  with a per-account WHITELIST+, plus a Channel Whitelist editor on the account
  detail page. Campaigns are matched by game OR by a whitelisted channel. (#20)
- **Spanish UI translation.** Full `es` locale (688 keys, parity with English),
  selectable in the nav and Settings ▸ General language switcher and
  auto-detected from `Accept-Language`. Added a locale-parity unit test that
  fails if any supported locale drifts from the English key set.

### Fixed

- **Auto-clean stale drop channels.** Channels whitelisted for a category-less
  drop are removed automatically once that campaign ends (a sweep on the
  discovery cadence), so the list doesn't accumulate dead channels.
- **Dashboard mining-row polish.** The "scanning channels" status no longer
  overlaps the state pill, the force-watch row shows the channel in green, and
  placeholder dashes were removed for a cleaner read.
- **Hardened the Twitch reward-claim eval script against injection** (CodeQL
  critical). Game names and drop titles were concatenated into the chromedp
  eval as JS literals, where a crafted value could break out of the script.
  They are now base64-encoded (`jsB64JSON`) and decoded in-script via
  `TextDecoder` (UTF-8 safe, handles CJK titles); base64 output can't escape
  the enclosing quotes. No change to which rewards are claimed.
- **Closed an open-redirect path on the language switch** (CodeQL). The
  `/api/lang` handler now reuses `applyRedirectTarget` (referer path only,
  never its host) and the flawed `isLocalRedirect` was removed.
  `applyRedirectTarget` also rejects protocol-relative `//host` paths.
- **`lang` cookie now sets `Secure`** when secure cookies are enabled
  (`GRUB_SECURE_COOKIES`), matching the session/CSRF/OIDC cookies (CodeQL).

## [1.2.5] — 2026-06-18

### Added

- **Multi-language UI (English + Simplified Chinese).** Full app i18n with a
  language switcher in Settings ▸ General; language is auto-detected from the
  `Accept-Language` header and remembered in a `lang` cookie. Live-event logs
  (kind tags + recurring watcher/pubsub/twitch/kick messages) are translated too.
- **Timezone auto-detection.** Set `TZ` (e.g. `TZ=Asia/Shanghai`) and all
  displayed times render in that zone with the correct abbreviation.
- **Proxy support (HTTP / SOCKS5).** Global proxy in Settings ▸ Proxy, routing
  outbound requests through the configured proxy.
- **Per-account enable/disable toggle** on the accounts list — flips an account
  on/off with a targeted watcher reload, no full restart.
- **Migrate from TwitchDropsMiner.** Coming from DevilXD or rangermix
  TwitchDropsMiner? Upload that miner's existing `cookies.jar` (next to its
  executable) to authorize a Twitch account here. TDM mints its auth-token under
  the same Android client_id GrubDrops uses, so the token is integrity-exempt and
  works for mining — unlike a browser-issued web cookie, which fails Twitch's
  drops integrity check. Upload is `.jar`/`.pkl`/`.pickle`.
- **"Reload Watchers" button** on the settings tabs whose changes need it
  (General, Drop Priority, Experimental, Proxy).

### Changed

- **Language switcher** moved from the nav bar into Settings ▸ General.
- **Account edit screen**: a compact inline "re-authenticate →" link replaces the
  verbose device-code copy block.
- **Numeric setting values** right-align so the value and unit hug the right edge.
- **Accrual-canary notification** relabelled "Health Failed" — it already fires
  only on an OK→fail transition.
- **Top-right clock shows UTC** (24h, with a `UTC` suffix) to match the UTC
  timestamps in the drops and history lists, instead of browser-local time.

### Fixed

- **Kick WS watch no longer death-loops after a stream ends.** A transient tick
  error fell back to re-discovery without stopping the live watch, leaking the
  presence goroutine and holding the one-watch-per-account slot; the error path
  now tears the watch down first.
- **Audit hardening:** CSRF on `POST /api/lang`, supported-language validation,
  Referer open-redirect guard, proxy-credential masking in logs, XSS-safe HTML
  escaping, a PubSub goroutine leak, a WebSocket write race, Twitch backend
  lifecycle cleanup, watcher nil-pointer guards on the pick/claim paths, Kick HTTP
  transport connection cleanup, and a watcher timer leak.
- **Timestamps** show the configured timezone abbreviation instead of a hardcoded
  "UTC"; the drops table no longer overlaps the platform dot after the change.
- Re-auth cancel-link styling, account-hint HTML rendering, and duplicate locale
  keys.

## [1.2.3] — 2026-06-16

### Fixed

- **"Run now" no longer duplicates the Heartbeat settings form.** The channel and
  interval form was part of the panel that gets swapped on Run-now (and the
  auto-refresh after it), so each run stacked another copy of the form. The form is
  now rendered once, separate from the results panel.

## [1.2.2] — 2026-06-16

### Changed

- **Settings inputs are bare again and size to their content.** Reverted the boxed
  look from 1.2.1 back to a transparent field with just the accent underline on
  focus. Values sit on the right and grow leftward as you type, instead of being
  full-width lines.
- **Faster release builds** — each image now builds on a native per-arch runner
  (amd64 + arm64) instead of emulating arm64 with QEMU, and the two images build in
  parallel. The slow part was the sidecar's emulated chromium install; native
  runners cut release build time substantially.

### Fixed

- **The console live log no longer makes the whole page scroll forever.** A change
  in 1.2.1 let it grow unbounded; it's back to a fixed-height panel that scrolls on
  its own.

## [1.2.1] — 2026-06-16

### Changed

- **Renamed the accrual canary to "Heartbeat Health Checker"** in the UI.
- **Settings fields are boxed and readable.** The password and channel inputs used
  to be near-invisible bare lines; they're proper bordered fields now, with room to
  breathe. The Heartbeat channel value sits on the right and grows leftward as you
  type, capped so it won't overlap the label.

### Fixed

- **The Accounts page no longer shows stale settings tabs.** It carried its own copy
  of the subnav, so opening Accounts reverted to the old order with no Health tab.
  Both pages share one subnav now, so they can't drift.
- **The console live log fills the screen.** It used to stop short and leave a big
  empty gap; it now runs to the bottom and scrolls.

## [1.2.0] — 2026-06-15

### Added

- **Accrual canary** — verifies watch-time still works without waiting for a live
  drop. Standalone transport probes (Twitch watch-beacon, Kick WebSocket) run on a
  schedule (default every 6h) against a configured always-live channel and report
  whether accrual is healthy. Probes are independent of the watchers, so they never
  disturb live mining. Note: this proves the transport is *accepted*, not that a
  drop was *credited*.
- **Settings → Health tab** — shows live canary results per platform (✓ / ✗ with
  detail + "X ago", or "not configured"), a form to set the canary channels +
  interval, and a **Run now** button that re-checks on demand. The read-only
  **Status** panel (version, uptime, sidecars, …) now lives here too.
- **Accrual-failure Discord alert** — opt-in "accrual canary" notification
  (Settings → Notifications, default off) fires once when a platform transitions
  OK → fail; it does not re-fire while still failing.
- **CI regression guards** — replay/golden tests pin the Kick WS frame shapes and
  the Twitch watch-beacon request shape, so a wire-format regression fails CI.

### Changed

- **Settings reorganised** — tabs reordered to General · Accounts · Drop Priority ·
  Notifications · Security · Health · Experimental. Logging (log level + retention)
  is now its own section on General, split out of Runtime. Section headers now use
  the accent colour.

### Fixed

- **Console row alignment** — idle Twitch and Kick account rows now line up at the
  same height (the per-account WS/Chrome pill no longer makes Kick rows taller).

## [1.1.0] — 2026-06-15

### Added

- **Light theme + toggle.** A ☀/☾ toggle in the top bar switches between the
  original dark theme and a new warm light theme; the choice persists
  (localStorage) and is applied before paint, so there's no flash on reload.

### Changed

- **Kick defaults to WS-first (WS, fall back to Chrome).** Fresh installs now
  start on the browserless WebSocket watch path — no Docker/Chrome needed to mine
  Kick on a Pi — and fall back to the Chrome IVS sidecar automatically if WS stops
  accruing. Was `browser` (Chrome required) by default. Existing installs keep
  their saved mode; only new installs get the new default. Override in
  Settings → Experimental.

## [1.0.5] — 2026-06-15

### Changed

- **Settings → Status "sidecars" lists only running sidecars** — Kick browser
  sidecars are created on demand, so the row previously showed every *registered*
  account address even when no container was up (and fell back to the login
  browser URL when empty), implying sidecars were running when none were. It now
  reflects runtime: only actually-running sidecars appear, and the row reads
  "none running" when idle.

### Added

- **README "Pick what to mine" section** — documents the whitelist-first step
  (nothing mines until a game is whitelisted) and the by-name add paths, matching
  the new cold-start prompt on `/drops`.

## [1.0.4] — 2026-06-15

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
