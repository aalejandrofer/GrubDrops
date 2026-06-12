# Changelog

All notable changes to GrubDrops.

## [Unreleased]

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
  `linux/amd64` images on `v*` tags.
- **cookies.txt Kick login** — parser, upload handler, and authorize-page template
  replacing the helper-binary path.

### Changed

- README rewritten deployment-first (Docker Compose quickstart, cookie-export
  instructions, environment variable reference).
- `cmd/kick-encrypt` one-shot ops tool deleted (superseded by cookies.txt form).

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
- **Discovery stall** — the discovery whitelist ignored the global games list,
  so every tick no-opped and campaigns went stale (looked like "Kick campaigns
  vanished after reload"). Now unions the global list.
- Twitch device-code: superseded orphan pollers that flooded the auth log.
- Stale empty "REWARD · — · —" history row filtered out.

### Notes
- Stack: Go + html/template/HTMX, SQLite (sqlc + goose), age-encrypted sessions.
- Twitch via Android device-code + GraphQL; Kick via utls Chrome-fingerprint
  (no browser, no cf_clearance).
