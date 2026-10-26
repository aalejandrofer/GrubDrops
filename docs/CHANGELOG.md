# Changelog

All notable changes to GrubDrops.

## [Unreleased]

### Added

- **Rewards search.** A search box filters claim history by reward, campaign or game on the server, so it covers every claim, not just the loaded ones.
- **Rewards pagination.** Previous / Next with page and range counts replace Load more; page size is 20, 50 or 100 and is kept per browser.
- **Home shows at most 7 games.** Games that are mining or queued stay visible; a link to Games shows how many more are tracked.
- **Compact list view on Games.** A Cards/List toggle shows one dense row per game with the same reordering; the choice is kept per browser.
- **New web app.** A SvelteKit single-page app in `web/` is built by npm, the Docker image and CI, and embedded in the binary. A "UI not built" page shows when the build is missing; `index.html` is never cached and hashed assets are immutable.
- **Home** with KPIs, ranked game cards with per-account progress, Up next, recent claims, and first-run, nothing-live, offline and many-account states.
- **Up next** has one queue per account. Reorder by button or drag for this session, or Reset to Games order.
- **Games** page: search or type a game to add it, reorder priority by button or drag, remove with confirm. Search runs locally and shows box art.
- **Rewards** page: drops in progress with time left, and claim history filtered by game or account.
- **Settings:** language, time format, theme and timezone, Discord and notification toggles, update check and command, sign out. Accounts: add Twitch with a device code or Kick with cookies.txt, log in again, rename and remove. Health: miner, sidecar, scan and canary status, a filtered live event log in your language, and a full log download.
- **Pause and resume mining per account** (`PUT /api/accounts/{id}/enabled`), with a Paused status in Settings and a Home notice when every account is paused. Accounts paused in 1.x stay paused and can now be resumed.
- **Planner and runner.** `internal/plan` and `internal/runner` replace the watcher and scheduler: one runner per account, a global chosen-games list, inventory progress that overrides a stale claim log entry, a plan rescan every 15 minutes while watching, and claimed drops of chosen active campaigns in the plan result.
- **Collection store.** Migration 0017 adds claim log, drop state, watch minute, account scan, chosen game and skip tables, with the `collect` package filling them from runner scans, progress and claims. A one-time `<db>.pre-v2` backup is written before an existing database migrates.
- **1.x upgrade** carries over chosen games (global list, then per-account games), claim history and total minutes watched.
- **JSON API and live updates.** Session, setup, login and CSRF under `/api`; overview, runners, games, claims, accounts, settings, health, update check and the Kick image proxy; Server-Sent Events at `/api/events` for runner, claim, games, accounts and log.
- **Daily Discord summary** at midnight in the configured timezone.
- **Shared locales.** The web app reads `internal/i18n/locales`, and `npm run check` fails on missing, unused or mismatched keys.

### Changed

- The miner runs the v2 planner, runner and collector and serves the JSON API and the embedded SPA.
- The pool restarts a panicked runner from its builder after one minute.
- `GET /api/events` streams every ring log line as a `log` event, and open streams end at shutdown instead of holding it for 5 s.
- Adding or reordering chosen games returns at once. Runners reload in the background, one reload at a time with repeated changes folded together, and a `games` event fires when the reload finishes.
- A campaign with no live stream moves on to the next queued campaign without rescanning, and every pick retries campaigns that had no live stream earlier.
- Swept rewards are recorded in the claim log under their planned game.
- README, translations, DESIGN.md, the site and AGENTS.md describe the SvelteKit UI, chosen games, the planner, the runner and the new login flows. A source build says to build the UI before `go build`.
- Home lists games that are mining or queued first, then the rest, each in priority order. Cards keep their real rank number.
- Up next on Home shows 5 drops per account, with Show more / Show less for the rest.
- The Health event log shows 10 events, with Show all for the rest of the last 50. DEBUG lines are left out of the list and its counts; Download full log still has them.
- With nothing live, a runner scans again after 12 hours instead of 5 minutes. Changing the chosen games or an account still rescans at once; awaiting a channel connect still rechecks every 5 minutes. The empty Up next line counts down to the server's next scan time.

### Fixed

- Twitch integrity tokens and raw responses no longer appear in logs. The integrity line records only the expiry and bad-bot flag, and gql and PubSub lines drop the response body (GRUB-20).
- The health canary says when it skipped because no account was signed in and unpaused, and tells you to sign in or resume one. Run now shows progress and a result or timeout message.
- Box art shows for newer games such as Marvel Rivals and Delta Force.
- The overview and games APIs report the next scan time (`next_scan_at`), and the scan stamp shows it.
- Paused signed-out accounts show as needing login and offer "Log in again" next to Resume. Home counts only signed-out accounts when deciding all are signed out, and the header chip skips the account whose login is open in Settings.
- Home is never blank: an unreachable miner, a failed load, no chosen games and all accounts signed out each show a message with a way forward.
- Home lists a runner on its game card when the game matched by slug, not only by name.
- Dates and clocks render in UTC when the saved timezone is one the browser rejects, such as "Local".
- The version popover links release notes only over https, and shows `{current} → {latest} available` when an update exists.
- Games search highlights the right letters in names that change length when lowercased, and suggestions highlight the typed match.
- First-run setup says a game is already added, with its priority, instead of a generic failure.
- `POST /api/games` returned 500 for games a migration seeded under another id (Apex Legends, Marvel Rivals); it now reuses the existing row by slug.
- The pre-v2 backup is written to a `.tmp` file and renamed, so an interrupted backup is retried on the next boot.
- Next scan time follows the runner's real recheck cadence and is blank for stopped accounts.
- Home shows signed out before the all-paused notice, as the Accounts list does.
- The collection migration is 0017 and safe to re-run. Databases at version 16 from the 1.x pipeline-v2 build skipped it, so Games search and Home failed on missing tables.
- A swept reward is recorded only when its title resolves to one drop, looking in the current campaign, then the current game, then the rest of the plan. An ambiguous title records nothing.
- A reward code is stored against the drop just claimed, within 10 minutes, never against the next drop. Each claim takes one code.
- Progress notifications fire again (start, 50% and 100%) when turned on in Settings.
- `log` events take the account name from memory, so logging never waits on the database.
- Shutdown waits for background loops before closing the Twitch backends and the database, including after an HTTP listen failure.
- Kick avatars on runners, the overview and `runner` events go through the Kick image proxy, so they load in the browser.
- Every API time field is an RFC 3339 UTC string, and an unset time is `null` instead of `0001-01-01T00:00:00Z`.
- Release notes with two equal lines, or a Twitch and a Kick campaign with the same id, no longer crash the page on a duplicate list key.
- A failed OIDC sign-in redirects to `/login?login_error=oidc`, so the login page shows the SSO error.
- A Twitch device status the UI does not know ends the flow with the error line instead of a blank panel.
- Settings shows no empty strip under Accounts when there are none, and the Health "Last scan" dot stays grey until a scan has run.
- The Discord webhook field is no longer a password input, so browsers stop autofilling the admin password into it. It saves only on Save or Enter, a URL that is not a Discord webhook shows an inline message, and language and timezone save only on a real change.

### Removed

- HTML + HTMX web UI, watcher, scheduler and catalog scraper (replaced by v2).
- The `GRUB_DISCOVERY_INTERVAL` env var, which only drove the removed catalog scraper.
- Settings with no v2 reader: tick interval, discovery interval, progress notify step, priority mode, log level and log retention. Their stored values are ignored.
- Per-account Discord webhooks; every notification goes to the global webhook.

## [1.4.2] — 2026-09-28

### Fixed

- **Twitch accounts on the older Android login keep mining when their token
  can't be refreshed.** Twitch now rejects refreshes for that login, so an
  account whose token had passed its expiry time sat idle at every restart
  even though the token still worked. GrubDrops now checks the token first
  and keeps using it while it's valid.

## [1.4.1] — 2026-09-28

### Fixed

- **Kick accounts no longer show "needs re-auth" a week after login.** The
  7-day expiry stamped at login is ignored for Kick; the periodic auth check
  decides whether cookies still work.
- **A brief database or network hiccup at startup no longer parks an
  account as needing re-auth.** Loading and refreshing a session now retry a
  few times first.
- **A refreshed Twitch session is kept even if saving it fails**, so its new
  refresh token isn't lost.

## [1.4.0] — 2026-09-28

### Fixed

- **New Twitch logins work again.** Around 2026-09-18 Twitch stopped accepting
  device-code sign-in from the Android app client that GrubDrops used, so
  adding or re-logging a Twitch account failed with "Device Error HTTP 400".
  New logins now go through the Twitch for TV client. Each account remembers
  which client signed it in, and every request for that account uses the same
  one: Twitch accepts mismatched requests but credits no watch time for them.
  Accounts already logged in keep their Android session and behave exactly as
  before, so there is nothing to redo. (#48)
- **Campaign discovery for accounts on the TV client.** Twitch hides the drops
  dashboard and campaign details from that client. For these accounts
  GrubDrops finds campaigns through the live drops-enabled channels of each
  whitelisted game, plus the campaigns already in your inventory, and reads
  account-link state from the inventory so campaigns you have not linked are
  skipped.
- **Drops that need a subscription are no longer mined.** Twitch marks them
  with a subscription requirement but can still list watch minutes, so the
  miner treated them as watch drops. They now show as action required. (#47)
- **A claim Twitch does not confirm is no longer recorded as claimed.** An
  empty claim response used to count as success, leaving drops marked claimed
  that Twitch still showed unclaimed. It is now a failed claim, and a completed
  drop that fails to claim three times in a row is skipped instead of retried
  forever.
- **Device-login errors show Twitch's reason** instead of a bare HTTP status.

### Known limits

- Accounts on the TV client only discover campaigns for whitelisted games. A
  campaign that runs only on channels outside a game's top ten live
  drops-enabled channels can be missed until it shows up in your inventory, and
  the Discoverable tab on /drops cannot list games you have not whitelisted.

## [1.3.12] — 2026-08-06

### Added

- **Download a database snapshot from Settings → Health.** Copying `miner.db`
  by hand while the miner is running can capture a half-written state; the new
  button uses SQLite's `VACUUM INTO` to write a consistent copy and streams it
  to you, so a backup no longer means stopping the container.

### Fixed

- **An account whose login dies now actually tells you.** The auth-health
  sweep has always detected expired sessions hourly and written the result to
  the Accounts page, but the notification event it was supposed to send
  (`EventAuth`) had no producer anywhere in the code — it was declared, had a
  Discord message template, was registered in the notification filter, and was
  never once fired. A Kick account could sit dead for days with the dashboard
  showing nothing. The sweep now sends a notification the moment auth health
  changes, and a recovery notice when it comes back. It fires only on the
  change, so a still-broken account is not re-reported every hour.

- **The dashboard no longer reads green while an account is dead.** Broken auth
  was tracked by two separate signals that could disagree: the watcher's live
  state, which drove the dashboard banner, and the hourly auth check, which
  only showed on the Accounts page. An account sitting idle never flipped its
  watcher state, so a dead session raised no dashboard alert at all. Both
  signals now feed the same banner, and an account that trips both still shows
  a single row.

- **Disabling a dead account no longer leaves a banner you can't clear.** The
  auth-health sweep only ever re-checks enabled accounts, so a disabled
  account's stored failure was frozen forever. Combined with the new
  auth-check banner above, disabling the natural response to a dead login
  produced a permanent, unclearable "needs re-auth" alert. Disabled accounts
  are now skipped when building dashboard alerts.

- **Release badge on the README no longer goes stale.** It was hardcoded to
  v1.3.8 while v1.3.11 was out, so the front page understated the project by
  two releases. It now reads the current release from GitHub instead of naming
  a version.

### Changed

- **Dependency updates, including a security fix.** gRPC moves to 1.82.1,
  which closes a high-severity advisory (GHSA-hrxh-6v49-42gf). The xDS half of
  that advisory never applied here since GrubDrops does not use xDS; the HTTP/2
  half touched the sidecar's gRPC server, which is only reachable from inside
  the Docker network. Also updates golang.org/x/net, golang.org/x/crypto,
  modernc.org/sqlite, chromedp, go-oidc, chi, goose, and go-connections. (#34,
  #30)

### Removed

- **Stale files from the abandoned Svelte port** are no longer tracked, and the
  agent worktree directory that held them is now ignored so it cannot be
  committed again.

## [1.3.11] — 2026-07-22

### Added

- **`keygen` subcommand generates a valid `GRUB_MASTER_KEY` with only Docker.**
  `docker run --rm ghcr.io/aalejandrofer/grubdrops:latest keygen` prints a fresh
  age X25519 identity (`AGE-SECRET-KEY-1…`), the exact format the session store
  requires. No Go toolchain or `age` install needed, so Portainer/GUI users have
  a one-liner too.

### Fixed

- **Quickstart no longer hands out a broken master key.** The docs told users to
  generate `GRUB_MASTER_KEY` with `head -c32 /dev/urandom | base64`, which is not
  an age identity — it fails to parse and crashes the miner at startup
  (`malformed secret key`). README, translated READMEs, landing page, and the
  reference compose now point at the new `keygen` command, and explain that
  Portainer/GUI deploys must set `GRUB_MASTER_KEY` in the stack's Environment
  variables (there's no shell to export it). (#33)
- **Compose file no longer errors in Portainer / strict YAML parsers.** The
  quickstart compose set `GRUB_MASTER_KEY` to an unquoted
  `${GRUB_MASTER_KEY:?run: head -c32 ...}` whose default-error message contains
  a colon-space; `docker compose` tolerated it, but strict YAML parsers like
  Portainer's read the `run:` as a nested mapping and rejected the stack with
  "Nested mappings are not allowed in compact mappings". The value is now
  quoted (README, translated READMEs, landing page, and the reference
  `deploy/docker-compose.yml`); interpolation is unchanged. (#32)
- **Kick stops re-hammering rewards that need an account link.** A completed
  Kick reward gated behind an external account link (Riot, Steam, …) returns a
  `connect_url` when claimed, which the sweep mislabeled as "likely already
  granted" and retried every poll, spamming the log roughly once a minute
  forever. Such a rejection is now recognised, the reward is skipped after the
  first attempt, and it's logged once with the link the user needs to follow
  instead of on every sweep.

## [1.3.10] — 2026-07-13

### Fixed

- **Twitch Spade beacon only follows twitch.tv hosts.** The minute-watched
  beacon URL is scraped from the (attacker-influenceable) channel page and the
  beacon POST carries the account's OAuth token, so an unpinned host was a
  token-exfiltration surface. The resolver now rejects any scraped spade or
  settings.js URL whose host is not `twitch.tv`/`*.twitch.tv` (production spade
  edges are `*.spade.twitch.tv`, so real traffic is unaffected). Hardening
  follow-up to the v1.3.9 Spade beacon work.
- **Ghost-skipped drops self-heal.** The persisted ghost-skip added in v1.3.9
  (skip completed drops across restarts) could permanently strand a legitimate
  drop if Twitch was merely slow to enroll it. The watcher now clears a skip,
  in memory and in the durable `kv` row, the moment that benefit reappears in
  the in-progress inventory unclaimed, so a transient enrollment lag no longer
  needs a manual SQL delete to recover.

## [1.3.9] — 2026-07-13

### Fixed

- **Twitch drops progress again (Spade beacon).** As of ~2026-07-11 Twitch
  stopped crediting minute-watched heartbeats sent through the GQL `SendEvents`
  mutation, so drops silently stopped progressing even though the transport
  returned success. The heartbeat is now sent as the Spade beacon POST Twitch
  requires: the per-channel beacon URL is scraped from the channel page (with a
  `settings.<hex>.js` fallback), and the minute-watched event is POSTed as
  `data=<urlencoded-base64-of-json>` (plain base64, no gzip/twilight envelope),
  expecting HTTP 204. Failed beacons evict and re-resolve the URL once before
  giving up. Both the direct and browser backends are fixed. (PR #28, thanks
  @steethebutcher)
- **Browser-backend Spade beacon now honours the proxy.** The Spade
  channel-page GET + beacon POST are direct net/http calls that don't route
  through the sidecar, so on the browser Twitch backend they bypassed the
  configured proxy. The per-account beacon HTTP client now uses the global
  proxy transport, matching the proxy-everything policy from v1.3.7.

### Added

- **`go run ./cmd/keygen`** emits a correctly-formatted age X25519 identity for
  `GRUB_MASTER_KEY`. The compose README's `head -c32 /dev/urandom | base64`
  produces a blob that fails `age.ParseX25519Identity` and crashes the miner at
  startup; keygen reuses the existing `filippo.io/age` dependency to emit a
  valid key. (PR #28, thanks @steethebutcher)

## [1.3.8] — 2026-07-05

### Fixed

- **Proxy tunnels no longer drop the first bytes of a connection.** When using
  an HTTP proxy, the CONNECT handshake could swallow the leading bytes of the
  tunneled stream, which could break a proxied connection. Fixes a correctness
  bug in the v1.3.7 proxy support (SOCKS5 was unaffected).

## [1.3.7] — 2026-07-03

### Fixed

- **All outbound traffic now honours the proxy.** Kick's Chrome-fingerprinted
  HTTP and WebSocket paths, Twitch PubSub, per-account Twitch, Discord webhooks,
  and the Kick browser sidecar previously bypassed the configured proxy. They
  now tunnel through it (SOCKS5 or HTTP CONNECT) with the TLS fingerprint
  unchanged. Restart to apply.

## [1.3.6] — 2026-07-01

### Added

- **Display-timezone setting.** Settings → General now has a "display timezone"
  field (IANA name, e.g. `Europe/Madrid`), so times no longer require the `TZ`
  environment variable. The change applies live, with no restart, across every
  page, the header clock, and the "claims today" boundary. Blank falls back to
  `TZ` then UTC; an invalid name is rejected with a flash. (`internal/timeutil`)

### Fixed

- **Header clock now honours the configured timezone** instead of always
  showing UTC. It renders in the display timezone (with its short zone label)
  via the browser's `Intl`, falling back to UTC only if the zone is unloadable.
- **Language switching no longer 403s.** The language-switch form POSTs to the
  CSRF-protected `/api/lang`; it now carries the CSRF token, so switching
  languages works again.
- **Kick sessions no longer flagged as expired after idle.** The auth-check
  probe now retries once after a short delay before marking an account
  needs-auth, so a single transient failure (idle-timeout, a lone 403, a
  network blip) no longer wrongly reports a still-valid session as expired.
  (`internal/authcheck`)
- **Uniform /drops row heights.** The "account not linked" and both
  "Discoverable" sections rendered taller than the whitelisted rows (a
  `.btn.sm`-inflated link button, a 1.3 line-height connect chip, and a native
  select flooring ~21px). All drops rows now share one height. (shadow-deployed
  under v1.3.6)

## [1.3.5] — 2026-06-30

### Fixed

- **Drop item lists no longer get stuck on "Loading…".** The items panel now
  always renders (it returns a friendly "couldn't load items" message instead
  of a silent 404), and campaign ids from the scrape fallback are URL-encoded
  so they resolve.

### Added

- **Sub / action drops now show in a campaign's item list** (e.g. a "1 Sub or
  Gift Sub" drop), rendered as "action required". The miner still ignores them
  — they can't be earned by watching.
- **Kick campaign item lists backfill on open**, matching Twitch.
- **Optional auto-update via Watchtower.** A commented-out `watchtower` service
  in `deploy/docker-compose.yml` (scoped to the miner container only) that pulls
  and recreates GrubDrops when a new image is published. Uncomment to enable.
- **Manually mark a drop collected.** If you claimed a drop outside GrubDrops
  (e.g. redeemed it directly), open the drop on `/drops`, click the `+` in a
  benefit's COLLECTED column, and pick the account. The mark is protected: the
  watcher's self-heal will not clear it (unlike an auto claim), so it survives
  even while the drop is still in progress. Uncollecting it (click the chip)
  removes the protection. Item lists now also load for drops on disabled
  accounts.
- **Update checker.** GrubDrops now checks GitHub for new releases in the
  background (on boot and every 6 hours) and shows an "update" pill by the
  GitHub icon plus an amber status dot when a newer version is out, linking to
  the release. Set `GRUB_UPDATE_CHECK=0` to disable the check.

### Changed

- Bumped dependencies: `coreos/go-oidc` 3.18.0 → 3.19.0,
  `modernc.org/sqlite` 1.52.0 → 1.53.0 (+ `libc` 1.72.5 → 1.73.4). (#22)
- Bumped GitHub Actions: `actions/checkout` → v7 across CI/release/pages,
  `configure-pages` → v6, `upload-pages-artifact` → v5, `deploy-pages` → v5. (#21)

## [1.3.4] — 2026-06-26

### Fixed

- **No more re-watching drops you already collected.** After a campaign
  finishes, Twitch stops listing its drops as in-progress, so the miner could
  no longer tell an already-claimed drop apart and would re-watch it for a few
  minutes before giving up (e.g. an R6S skin you collected yesterday). The
  miner now skips any drop it already holds a claim for, matched by the drop's
  unique id so a brand-new campaign offering the same item is still mined.
  Verified on a live environment. (#24)
- **Bogus "collected" marks now clear themselves for any in-progress drop.**
  Building on 1.3.3, the cleanup no longer depends on the bot actively mining a
  campaign. Every discovery pass sweeps your in-progress drops directly and
  removes any collected mark Twitch reports as unclaimed, so leftover false
  marks on lower-priority or not-currently-mined campaigns (e.g. a recurring
  SMITE weekly) clear on their own. It only ever acts on drops Twitch is
  actively tracking in-progress, so it never removes a real claim. Verified on
  a live environment: false marks cleared, genuine claims untouched. (#24)

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
