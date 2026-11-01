# Changelog

All notable changes to GrubDrops.

## [Unreleased]

## [2.3.0] — 2026-11-01

### Added
- **New features carry a "New" pill.** The Points link in the top bar and each new setting (follow raids, sleep window, skip sleep, points mode, password sign-in, error pings, reward code notices, the experimental share switch) show a small "New" pill on 2.3.x builds. It flags that the feature is fresh and may be rough, and it goes away by itself in 2.4.
- **Sleep window.** Settings → Engine can pause mining every night between two times, in your timezone. Every account stops watching, drops and Points watch alike, and picks up again when the window ends. Start and end shift by up to 20 minutes each night, so the pattern isn't identical every day. It's off by default. An account set to "Skip sleep window" in its menu keeps mining through it. Home and the header show when mining resumes, and Mine next waits until then. Accounts gain a `skip_sleep` column (migration 0025).
- **SSO can be the only way in.** With OIDC single sign-on configured, Settings → Authentication has a Password sign-in switch. Turned off, the login page shows only the SSO button and `POST /api/login` answers 403 `password_login_disabled`. It's on by default, without SSO the row shows "OIDC not enabled" instead of the switch, and the API refuses to turn it off without SSO (409 `sso_not_configured`). If SSO stops being configured, or the identity provider can't be reached at startup, the password works again, and first-run setup is unaffected. The README shows how to turn it back on from the host if SSO breaks. `GET /api/session` gains `password_login`; `GET /api/settings` gains `sso` and `password_login_disabled`, which `PUT` accepts. The setting is the KV key `settings:password_login_disabled`.
- **Twitch accounts claim the channel-points bonus while they watch.** Each Twitch account has a points mode, set on the Points page: Drops only, Drops + points or Points only. With Drops + points, a Twitch account checks for the bonus when a watch starts and every 5 minutes after, on whatever it's watching, a drop or a Points watch channel, and claims it. A claim logs one line with the points gained; nothing is logged while no bonus is up. The check runs beside the watch, with its own 10-second timeout, so it never holds up the watch beacon, and it stops with the watch. Accounts on Drops only send no channel-points requests. Channel points are supported on Twitch only for now: Kick balances and bonus claims aren't supported yet (the Points page says they're coming soon), Kick accounts get no points mode, and their Points watch works as before. Accounts gain a `points_mode` column (`off` for Drops only, `drops_points` or `points_only`, migration 0026). Every account, new or upgraded, starts on Drops only and keeps its Points watch channels, so an account that used Points watch before needs Drops + points or Points only on the Points page for its Points watch to run again. `GET /api/accounts` gains `points_mode`, and `PUT /api/accounts/{id}/points-mode` with `{"points_mode": "off" | "drops_points" | "points_only"}` sets it (204). On a Kick account it's refused with 400 `points_unsupported_platform`.
- **Points only mode.** A Twitch account's points mode on the Points page has a third choice, Points only. A points-only account never looks for drop campaigns: it watches its Points watch channels in their order, moves to the next live one when a channel goes offline, and claims the channel-points bonus on Twitch as with Drops + points. It needs no games, only Points watch channels; with none it shows "No Points watch channels". Switching applies at once without a restart: Points only ends a running drop watch, and leaving it lets a live drop take over straight away. Drops a points channel happens to finish are still claimed.
- **Channel-points balances are cached for each account and Points watch channel.** For a Twitch account on Drops + points or Points only, GrubDrops reads its balance on each of its Points watch channels once an hour, one channel at a time, and retries a failed read after 5 minutes, waiting longer after each further failure, up to 30 minutes. A balance the bonus claim already saw while watching counts as a fresh read, so watched channels need no extra request. Balances are kept across restarts (migration 0027) and are dropped when an account leaves a channel. The new `GET /api/points` lists each Points watch channel with every assigned account's balance, read time and status (`ok`, `pending`, `error`, `unavailable` or `off`), plus each account's `points_mode` and total; Kick accounts show as `unavailable`. A `points` SSE event fires when a balance changes.
- **Refresh balances now.** The Points page has a Refresh now button next to the refresh interval on the Twitch tab. It reads every balance at once, still one channel at a time, instead of waiting for the hourly read, and the button stays busy until the new balances arrive. It works at most once a minute. A read already running is not doubled: channels it reads after the click count as refreshed. `POST /api/points/refresh` starts it (202). Inside the one-minute cooldown it answers 429 `cooldown` with `retry_after_sec` and a `Retry-After` header, and 503 `unavailable` when balances are off.
- **Refresh drops and claims now.** Inventory has a Refresh button in its header, and a campaign's side panel has a small one next to its close button. Refresh on Inventory rescans every running account, and Refresh in a campaign rescans the accounts on that campaign's platform, so drops collected outside GrubDrops show without waiting for the next scan. The button stays busy until the first scan lands, then the list or panel reloads on its own. It works at most once a minute, one cooldown shared by both buttons. `POST /api/scan` starts it (202), with an optional body `{"platform": "twitch" | "kick"}`; any other platform is 400. Inside the cooldown it answers 429 `cooldown` with `retry_after_sec` and a `Retry-After` header. `POST /api/games/scan` is unchanged.
- **Error notifications can ping you.** Settings → Notifications → Advanced has a Discord user ID and a Telegram user ID field. With one set, auth problems, failures and failed health checks mention that user: Discord pings them through the webhook, and Telegram mentions them in the chat. Claims, progress, daily summaries and test messages stay silent, and an empty field sends no mention. Both fields take a numeric user ID only. `GET /api/settings` gains `discord_mention_id` and `telegram_mention_id`, which `PUT` accepts (400 for anything but digits).
- **Points page.** A new Points page in the top bar, between Games and Inventory, is the home of Points watch and channel points. Its Twitch and Kick tabs show that platform's Points watch channels in order: drag a row or use its arrows to reorder, tap an account's chip on a channel to have it watch there, paste a channel link to add one at the bottom, and click remove twice to drop one. Each channel shows whether it's live and who is watching it now. On Twitch, each account has its points mode picker (Drops only, Drops + points or Points only) and when its balances were last read, and each chip shows that account's cached balance on the channel. A balance that isn't read yet, one whose last read failed (showing the last good value and its age), and a channel without channel points each show as such. Balances update as they're read, without a reload. The Channel points section is gone from the account menu on Settings → Accounts; an account's details drawer there has the same picker.
- **Points page account cards show what each account is doing.** Each card on the Points page shows the account's points total and a state line. While an account is on Points watch it reads "Watching shroud · 37m", with the time on the channel ticking over each minute, and the card gets a light green tint. Other states read as before: Idle, Mining: Rust, Paused and so on. Kick cards show no total. With more than two accounts on Points watch, the header shortens to "Points watching · 3 accounts".
- **Discover can sort by start date.** The Discover page has a Sort control: A-Z (the default), Newest and Ending soon. Newest puts the games with the most recently started campaigns first, orders each game's campaigns the same way, and shows when each campaign started. Ending soon puts the game whose next campaign ends first at the top. Requested in discussion #84.
- **Twitch accounts can follow raids (#81).** Settings → Engine has a Follow raids switch, off by default. Turned on, a Twitch account listens for raids from the channel it's watching, whether a drop or a Points watch channel, and when that channel raids someone it joins the raid after a random 5 to 20 second pause. Each raid is joined once, even when Twitch repeats the announcement, and only if the account is still watching the raiding channel when the pause ends. A joined raid logs one line. The account keeps watching the channel it chose and doesn't move to the raid target, which may have no drops: once the raiding stream ends, a drop watch picks its next channel by the usual rules and a Points watch picks the first live channel on its list, so watch time and claims work as before. The switch applies at once, without restarting any account. Kick has no raids. `GET /api/settings` gains `follow_raids`, which `PUT` accepts; it's stored as the KV key `settings:follow_raids`.
- **Experimental: TV accounts can mine campaigns an Android login sees.** A new Settings → Experimental section, right after Engine, has a Share Android campaigns with TV accounts switch. It's experimental and off by default. A Twitch account signed in with the TV code finds campaigns only on the top live channels of each game and in its own inventory, so it misses campaigns none of those channels serve. Turned on, its scan also takes the active campaigns from the Android login's drops dashboard (the cached Discover catalog, no extra requests per account) that it didn't find itself, on games in its list. Its own copy of a campaign always wins. Nothing of the Android account comes along: no progress, claims, drop instance IDs or link state. A shared campaign that needs a publisher account link shows as needing one until the TV account's own inventory says otherwise, and a campaign limited to some channels is watched only on those. Once Twitch credits watch time, the campaign shows in the TV account's inventory and progress and claims work as usual. The switch applies at the next scan, which it starts right away. Without both an enabled Android Twitch login and an enabled TV one, the switch is replaced by a "Not available" chip and the hint reads "Needs an Android and a TV Twitch login."; a saved "on" does nothing. Android accounts are unaffected. `GET /api/settings` gains `exp_share_catalog`, which `PUT` accepts, and the read-only `share_catalog_available`; it's stored as the KV key `settings:exp_share_catalog`.
- **Every account sweeps its inventory once a day.** When an account starts, and every 24 hours after, it reads its inventory and claims every finished drop that isn't claimed yet, 2 seconds apart, under its own drop instance ID. It runs beside the watch, so it never holds up the watch beacon. It waits out the sleep window, and paused accounts don't sweep. Each claim logs one line and sends the usual claim notification, and each sweep logs a summary with how many drops it claimed, failed and corrected. On Twitch, a drop the claim log calls claimed but the inventory still shows unclaimed, logged more than an hour ago, is put back as claimable and claimed again; drops you marked collected by hand are left alone. A drop Twitch refuses is skipped until a later sweep claims it. Kick accounts run their existing finished-reward sweep on the same schedule.
- **Twitch reward codes show on Inventory and in notifications.** Every 3 hours, and once at start, each Twitch account reads the reward codes Twitch gave it, one account at a time and 30 seconds apart, beside the watch so the beacon never waits. A new code sends one Discord or Telegram notification with the reward, account, code, redeem link and expiry, and a reminder goes out 2 days before it expires unless you marked it redeemed or dismissed it. Each notification is sent once, even across restarts. A code that arrives while notifications are off (no Discord or Telegram set up, or the Reward codes switch off), or that has already expired when first seen, is recorded as skipped (migration 0029) and isn't sent later, not even its reminder. Settings → Notifications has a Reward codes switch, on by default. Inventory has a Codes section, shown only when there are codes, with each code's reward, account, a copy button, a Redeem link and the time left, plus Redeemed and Dismiss buttons that hide it. Codes are stored encrypted, like sessions (migration 0028), and never written to the log; the pubsub reward code lines no longer log the code or the message either. A login Twitch refuses for this read is skipped quietly and tried again a day later. `GET /api/reward-codes` lists open, unexpired codes, and `PUT /api/reward-codes/{id}` with `{"state": "redeemed" | "dismissed"}` marks one (204, 404 unknown, 400 bad state). `GET /api/settings` `notify` gains `reward_codes`, which `PUT` accepts; it's stored as the KV key `settings:notify_reward_codes`.
- **Single sign-on setup guide.** Settings → Authentication → Set up opens a short guide: create an OIDC app with this miner's redirect URL, add the `GRUB_OIDC_*` lines to your env, and restart with `docker compose up -d`. The redirect URL, the env block and the restart command each have a copy button, and the env block comes prefilled with your miner's address. It warns that without an email or group allowlist, anyone the provider signs in is admin, and links to the full guide in the README.
- **Expand advanced settings.** Settings → General has a switch that keeps every Advanced section open, so the browser's find (Ctrl+F) reaches the settings inside them. Collapsing one still works until the next reload. It's kept in this browser only.

### Changed
- Settings → General: Sign out sits under the panel on its own instead of in an empty row.
- The TV code login warning in Add account now also says TV accounts find drops only on the top live channels of your games, so some show up late or not at all.
- **Refresh buttons show Refreshing, then turn green for 5 seconds.** On Discover, Inventory, Points and a campaign's panel the refresh icon spins in amber while it works and the button goes green once fresh data is in. There's no countdown any more: a click inside the one-minute limit just shows the data as fresh. Discover's button no longer sits greyed out until it may refetch.
- **Typed settings save on their own.** The sleep window times, Proxy URL, Discord webhook, Telegram bot token and chat ID, notification avatar URL and mention IDs save 10 seconds after you stop typing. Each edit restarts the wait, and a toast counts down: "Saving in 10s". Save and Enter still save at once, and Cancel drops the edit. A value that isn't valid is never sent: its error shows and the edit stays, and a failed save keeps the edit too. Leaving Settings or closing the tab saves pending edits straight away, and asks first when one can't be saved. In the webhook, bot token and proxy fields only typing or pasting starts the wait, so a password manager filling them in sends nothing. Switches and menus still save at once.
- Top bar: the current page is underlined in purple instead of sitting on a grey block, and hover only brightens the text.
- **Save countdown toasts sit top right.** The Games reorder toast and the Settings save toast now show under the top bar on the right, with a dark amber border, instead of at the bottom of the page. On phones they span the screen width, and on Settings they sit under the section pills.
- **Notifications settings have the new look.** Discord and Telegram each get their own mark and a Connected or Not connected chip, and Telegram's form opens with Connect. All seven events share one list, each with a coloured dot, so Drop progress and Canary failures are no longer under Advanced. Advanced keeps the avatar URL and the mention IDs. The webhook URL and bot token still show only as hints.
- **Settings: controls that can't be used show "Not available".** When something a control needs is missing, Settings no longer shows it greyed out. It shows a "Not available" chip in its place, and the hint says why. This applies to the Experimental share switch, Restart on a paused account, Mine now when the account can't mine, Use proxy until a proxy URL is saved ("Add a proxy URL first."), and Test notification when neither Discord nor Telegram is set up. Mine next in Up next does the same, with the reason in its tooltip.
- **Settings has a colour per section.** Each section has a colour and an icon, on its link in the index, its heading, its switches and focus rings, in dark and light themes. Red, amber and green are kept for status, so only Health uses one of them. Headings show a status chip from data the page already has: how many accounts need a login, a failing health check, Discord or Telegram on, and whether GrubDrops is up to date. The index puts an amber dot on Accounts when one needs a login and a red dot on Health when a health check fails. Screen readers hear what each dot means. On phones the index is a sticky row of coloured pills that follows the scroll. A click in the index scrolls the section to just under the top bar. Buttons, switches and inputs are bigger to tap on phones, and an off switch now has a visible edge. Twitch accounts show in purple and Kick accounts in green. The active link and selected rows no longer have a bar on their left edge.
- **Admin password is now Authentication.** The section has a Single sign-on row first: with OIDC set up it names the provider and shows the issuer's host, how many emails and groups are allowed and that it's set in the env; without it, a Set up button opens the setup guide. The Password sign-in row comes next and is always shown; without SSO it says OIDC is not enabled instead of showing the switch. Change password follows. `GET /api/settings` `sso` gains `issuer_host` (the host name only), `allowed_emails` and `allowed_groups` (counts only). The client ID, secret and allowlist entries never leave the server.
- **Force watch is now Points watch.** It's the same list on the Points page, only the name changed.
- **Inventory's Up next shows each game's campaigns without expanding.** Rows no longer open; click a campaign name to open its drawer, where its drops and the collect button live.
- **Home's Points watch card shows Twitch channel points.** The card's total sits next to its label, and each live Twitch channel shows the balance of the accounts on it, from the cached balances. Balances not read yet and a failed last read show as such; Kick channels show none. `GET /api/overview` `force_watch` gains `points`, `points_read` and `points_at`, and each live row `balance` and `read`. On phones the card and Accounts mining each take a full row.
- **Home's Points watch card links to the Points page,** where the list now lives, instead of Games. The four Home cards are now the same width.
- **Each account has a details drawer in Settings → Accounts.** Click an account's row to open a panel on the right with everything about that account: rename it, see its session, check the login or log in again, pause or resume mining, turn its own game list and Skip sleep window on or off, switch to its next drop with Mine now, pick its channel points mode (Drops only, Drops + points or Points only) next to its total balance, restart its runner, and remove it after a confirm. Each change saves as you make it and shows Saving, Saved or Not saved beside it. Clicking another row switches the drawer to that account, and a click anywhere else or Escape closes it. On phones it opens full width. The ⋯ menu keeps the quick actions (details, check login, pause or resume, restart, skip sleep window), Remove moved into the drawer, and the badges under each row are gone.
- **Points watch moved from Games to Points.** The Points watch panel and its dialog are gone from the Games rail, and a line there links to the Points page, where the list now lives.
- **Points watch follows a Twitch account's points mode.** A Twitch account on Drops only skips Points watch, even when it's assigned to a channel. Its assignments stay, so switching to Drops + points brings them back. On the Points page its chip shows dashed on the channels it's ticked on. Kick accounts have no points mode: a Kick account still watches its Points watch channels whenever it has no drop, whatever `points_mode` is stored, and is never points only.
- **Changing an account or engine setting no longer restarts a watch that can keep going.** Skip sleep window, the points mode and the Engine sleep window and timezone now apply to the running account at once instead of rebuilding it. A watch outside the sleep window carries on, and one the window now covers stops right away. Switching to Drops + points or Points only starts the bonus check on the current watch, and an idle account picks up its Points watch channel. Switching to Drops only stops the check and ends a Points watch. Marking a drop collected, or removing a claim, no longer rebuilds the account either. If it was watching that drop, it moves on to the next one. Any other watch stays. A rescan, such as after a priority or scan pace change, keeps a Points watch going unless a drop is now ready to mine. A login, a pause or resume, an account's own games list, and Restart still rebuild the account.
- **A rotated Twitch query hash is logged as such.** When Twitch answers `PersistedQueryNotFound`, the log shows an error naming the operation and saying its hash needs refreshing, for every query, instead of a generic GraphQL error.
- **Twitch watch beacon sends `player=site` again.** Twitch accepts a minute-watched beacon without it but credits no channel points for the watch. Drop watch-time rides the same beacon, so this needs a live-drop check before release.
- **Releases only rebuild the browser sidecar image when the sidecar changes.** Each `v*` tag still builds the miner. The release workflow compares the sidecar's Go packages, `proto/`, `deploy/Dockerfile.browser`, `go.mod`/`go.sum` and `.dockerignore` against the previous tag. If none changed, it points `grubdrops-browser:<tag>` and `:latest` at the previous browser image instead of building a new one, which saves two runner jobs and stops hosts pulling an identical sidecar. To force a rebuild, for example for Chromium or Debian security fixes, run the Release images workflow by hand with the newest tag and `force_browser` checked.
- **Shorter text across the app.** Labels, hints and status lines are trimmed to the essentials; an account's status line now reads like "Mining: War Thunder".
- **Settings → Health log is easier to read.** The activity log uses the normal font, starts each line with its kind in coloured text (Claim, Progress, Watch, State, Discovery, Auth, Error), and colours the account name by platform. Warnings and errors turn the kind amber or red, so a failed claim never shows green. About 35 more log lines read as plain text, such as "Started watching xqc" and "Sign-in checked", and a state change reads "Starting → Watching". All, Problems and Claims are a segmented control with their counts. The canary sits above the log with Run now and one row per platform showing OK, Failing, Skipped or Off, plus the failure reason. Run now shows "Not available" when no canary channel is set.
- **Shorter help under fields.** The Add a game box drops its "e.g." line and the "Campaign missing?" toggle for a Twitch and a Kick example link; click one and the box fills with that link with "rust" selected, so typing a game replaces it. Points does the same with `twitch.tv/streamer` or `kick.com/streamer` under its add box and drops the line under its tabs and the one about where a new channel lands. Proxy URL gets clickable `http://`, `https://` and `socks5://` that set the scheme, and the Kick cookies.txt hint links to the Get cookies.txt browser extension.

### Fixed
- **Twitch claims are only recorded once Twitch has granted them (GRUB-58).** Twitch can answer a claim with a success status and still leave the drop unclaimed, as it did for an account that already held the same rewards from an earlier War Thunder campaign. GrubDrops took that answer as a claim, logged it, and never tried again, so the drops showed as claimed but were never collected. A claim now counts only when the inventory confirms it, read up to 3 times 2 seconds apart, and an empty status counts as refused. A refused claim leaves the drop claimable, and the daily inventory sweep retries it and corrects rows logged this way before.
- **Home names the platform of the account a game waits for.** With two accounts sharing a name, such as TTik3r on Twitch and on Kick, a card said "Waiting for TTik3r" and it was easy to read it as the wrong one. It now says "Waiting for TTik3r (Kick)", and the line under it names the platform too.
- **Paused accounts' drops stay on Home.** A game whose drops only a paused account still had to mine dropped off Home, as if there were nothing left. Its card now shows an amber "TTik3r (Twitch) paused" pill (with "+n" when several paused accounts hold it) and says to resume the account to mine them. Paused accounts still don't watch or claim anything. `GET /api/games` gains `paused_holders` and the `paused` state, which Games also shows.
- **A saved engine setting can no longer be lost to a read in progress.** If a runner read the settings just as you saved them, the old values (UI language, priority, scan pace, preempt, sleep window) could stay in effect until the next save or a restart. A read that started before a save is now thrown away instead of cached.
- **A second account no longer starts a new campaign hours after the first (discussion #83).** Each account checked for campaigns on its own clock, so with two Twitch accounts the second could sit idle for up to a full scan pace after the first one found a new campaign. Now when one account finds a new campaign for a game on the list, the other accounts on that platform check right away. An idle account also wakes up when an upcoming campaign is due to start, and an account signed in with the TV login (which finds campaigns through live channels) rechecks every 5 minutes instead of every scan pace while it has nothing to mine.
- **Passing sign-in checks no longer count as problems.** Every sign-in check landed under Problems on the activity log, even a passing one. Now only a failed check counts (a WARN or ERROR line, or a check that reports `ok` false). Health events pass `ok` through in `fields`, and the log line reads "Sign-in OK" or "Sign-in check failed".
- **Activity filter counts match the list.** The chips counted the whole log buffer ("All 94") while the list showed the newest 50. Counts and list now use the same newest 50 lines.
- **Inventory sweeps no longer show under Claims.** "Inventory checked" lines were tagged as claims. The sweep summary and sweep failure lines are now tagged as discovery, so Claims lists only claims and the sweep lines still show under All.
- **Old `/drops` links redirect to Inventory**, like `/rewards` already did, instead of showing a 404.
- **Small UI fixes.** The "Waiting for…" chip on Home and Games is readable in the light theme (about 5:1, was 1.9:1). The Games save countdown no longer blocks clicks on the card under it. English seconds read "9s" everywhere ("Restarting in 9s", "0s ago", "retrying in 5s"). The time format example in Settings ticks each minute, and the "Updated … ago" age on Points ticks every 5 seconds while it is under a minute old.

## [2.2.2] — 2026-10-29

### Added
- **Help on game names.** Under the Games add box, "Kick or Twitch campaign not showing up?" explains that each platform names its own categories (Twitch's World of Warcraft is World of Warcraft: Forever on Kick) and that you add the name each platform uses, or paste its category link.

### Fixed
- **Box art for Kick-only games.** A game with no Twitch match, such as Kick's World of Warcraft: Forever, showed the placeholder on Games and Home because its art came from a name-keyed Twitch URL. It now uses the Kick category image while a Kick campaign for it is live.
- **"Link your game account" banner clears on Check now.** When Check now reported Linked, the banner stayed until the account's next scan, up to a minute later for an account that was watching. It now clears straight away.
- **Kick open campaigns find live channels again.** A Kick campaign open to any channel in its category, such as World of Warcraft: Forever, could log "no eligible streams live" while thousands watched it. The category directory GrubDrops asked ignored the category and returned five random streams. It now asks Kick's livestream list by category id, biggest streams first.
- **No sideways scroll on phones.** At phone width the top nav was wider than the screen, so every page scrolled sideways. The nav links now scroll inside the bar, and the current page's link stays in view.

### Changed
- **Game order changes apply after a 10 second pause.** Every arrow click on Games saves the order, and each save used to restart every account's watch, so moving a game five places restarted the miner five times. The miner now waits until saves stop for 10 seconds and restarts once. A toast counts down: "Saved. Restarting the miner in 10 s". Adding or removing a game waits the same way.
- **`.playwright-cli/` is git-ignored.** Console logs and page snapshots from `playwright-cli` runs no longer show as untracked files.

## [2.2.1] — 2026-10-28

### Fixed
- **Games Twitch renamed now get mined** ([#78](https://github.com/aalejandrofer/GrubDrops/issues/78)). Twitch renamed Overwatch 2 to Overwatch and Counter-Strike 2 to Counter-Strike, so their Twitch campaigns never matched the game on your list and nothing was queued. Adding Overwatch again said "Already in your list", because it resolved to the seeded Overwatch 2 row. Games now store the name Twitch uses (`games.twitch_name`, migration 0024), picked up from the campaigns and from the Twitch game lookup after a Twitch scan, and the planner matches a campaign by name, slug or Twitch name. Kick keeps matching the stored name, so Counter-Strike 2 still finds Kick's Counter-Strike 2 category. When a lookup saves a new Twitch name, every account rebuilds, including those on their own list, so the campaign is queued without a restart. Home and Discover match the same way, and `GET /api/games` carries `twitch_name`. The seeded Overwatch 2 row is renamed to Overwatch, the name Twitch and Kick both use now; it keeps its id, so lists and priorities stay as they were.
- **Release notes in the version popover read cleanly.** The update popover in the top bar showed release-note bullets as raw markdown, with `**` and backticks on screen. Bold and code now render as bold and code, link markup keeps only its text, and each bullet is cut to 3 lines so the popover stays short. Full release notes has the rest.

## [2.2.0] — 2026-10-27

### Added
- **Turkish.** The UI and notifications are now available in Turkish (Türkçe).
- **Campaign details on Games and Inventory.** Clicking a campaign on Games, or a claimed reward on Inventory, opens a side drawer with the whole campaign: every drop with its image and minutes, and each account's state (claimed, minutes watched, not started, or blocked, such as needing a linked account). From Inventory, the clicked reward is highlighted. Campaigns that ended fall back to your stored claims.
- **Campaign details from Home.** The campaign name on a Home game card and in each Up next row is now a link that opens the same campaign drawer. From Up next, the clicked drop is highlighted. Clicking the name doesn't start a drag, so reordering Up next works as before.
- **Force watch.** Idle accounts watch a channel you pick from Games, a drop always comes first, and live channels show on Home. The Force watch panel under the Lists rail takes a twitch.tv or kick.com channel link and the accounts that watch it, ranked with move up and down. Home gets a Force watch tile with the live channels and who watches them, and an account doing it shows Force watching. With a force watch channel that has an enabled account, Home shows the dashboard even with no games chosen. A channel whose watch fails to start is passed over for 5 minutes so the next live one is watched. Twitch force watch needs the default Twitch login, not the browser login. `GET/PUT /api/force-watch`, `GET /api/overview` gains `force_watch`, and the `force_watch` event fires after a save.

### Changed
- **Accounts comes before Engine in Settings.** The Accounts section and its sub-nav link now sit right after General.
- **Update check runs every hour.** The background release check now runs hourly instead of every 6 hours, so the blue version pill in the top bar shows a new release sooner. Settings, Updates says "Every hour." under Check automatically. `GRUB_UPDATE_INTERVAL` still overrides it.
- **No keygen step to start.** When `GRUB_MASTER_KEY` is unset, the miner creates `master.key` next to the DB on first start and reuses it, so `docker compose up` works with no key in the compose file. A set `GRUB_MASTER_KEY` still wins, so existing installs are unchanged. If neither is present but the database already exists, the miner refuses to start instead of making a new key that cannot read the saved sessions. The quick start in the README, translations and landing page drops the keygen step.
- **Home totals read the same way.** Total minutes and total drops claimed both show the all-time number first with today's under it. The account count is gone from the minutes tile.
- **Discover shows a loading bar.** While campaigns load or refresh, Discover shows a moving bar and "Loading, pulling from Twitch and Kick", so a slow fetch no longer looks stuck.
- **Add a game from its category link.** The Games add box also takes a pasted `twitch.tv/directory/category/...` or `kick.com/category/...` link and adds the game under the name the platform uses. Handy when a game is renamed and the typed name no longer matches. A link Twitch or Kick doesn't know returns `unknown_category`. Search suggestions show each game's box art. `GET /api/games/search` hits carry `box_art_url`. A small grey line under the box shows an example Twitch link.
- **Rewards is now Inventory.** The page and its nav link are called Inventory and live at `/inventory`. Old `/rewards` links redirect there.
- **Inventory groups Up next by game.** Up next on Inventory now shows one row per game, in queue order, with the drops of all its campaigns and accounts added up, the accounts that have it queued, and the watch time still needed. Click a row to open it in place and see each campaign with its drops, accounts and minutes; several rows can be open at once, and a campaign name opens the campaign drawer. Mine next stays on Home's Up next.
- **Reorder games in Inventory's Up next.** Drag a game row by its handle to move that game ahead of or behind the others for this session. Every account's Up next is sorted to the new game order, and drops of one game keep their order. The rows stay where you dropped them, also after a reload in the same tab, and games that show up later go after them. Like Home, the change is temporary: Reset to Games order brings back the Games order, here or on Home, and the footer now reads "Edits above are temporary. Permanent order lives on Games."
- **Home lists idle accounts on their own.** Game cards on Home now list only the accounts mining that game. Idle, signed-out and paused accounts used to sit under the top card, so an account looked like it was waiting for a game that is not on its list or its platform. They now show as small chips under the last card, idle first, then signed out, then paused, each with its platform and state. The chips are hidden when every account is mining.
- **Home counts mining accounts.** The accounts tile on Home now reads Accounts Mining with mining accounts out of all accounts (for example 2 / 4), and the line under it says how many accounts are healthy ("4 accounts healthy") instead of "All signed in". A signed-out account still turns the tile red and names the account.
- **Ending Soonest shows where it matters.** When the Priority Mode is Ending Soonest, Home's Up next shows an "Ending Soonest" tag (it links to Games). On Games, the Priority Mode panel sits at the top of the left rail, above the account lists and Force watch, with the Priority Order / Ending Soonest switch (the same setting as Settings, Engine). Cards | List moved to the top right beside Scan now, and the list hints and the old Priority heading are gone, so the search is followed straight by the games. While Ending Soonest is on, the list shows games in the order it mines them (earliest campaign end first) and the drag handle and move arrows turn into a clock, so the list can't be reordered until Priority Order is back. The Games page also drops the "Changes here are permanent" and "Each extra game adds a little time to every scan" hints. In Settings, Engine, the "Switch to a higher-ranked drop" description says it leaves the current drop for a campaign that ends sooner while Ending Soonest is on.
- **Inventory links to the platform pages.** The Inventory header now shows "Twitch / Kick Inventory" at the top right, with Twitch linking to your Twitch drops inventory and Kick to the Kick drops campaigns page, both opening in a new tab. The "updated" time moved next to the status line on the left.
- **Admin password moved to the bottom of Settings.** Changing the admin password is now its own Admin password section, last in Settings under Updates, instead of sitting inside General.
- **Waiting says what it waits for.** A Home game card that is waiting now names the reason: "Waiting for <channel>" when the campaign only counts on certain channels and none is live (with "+n" for more channels), "Waiting for a live stream" for an open campaign with nothing live, and "Waiting for <account>" when the account is busy mining something else. The same reason shows in the card's line and above its queued drops. Up next shows each target's allowed channels (first 3) and their total count.
- **Channel-limited Twitch campaigns go first.** Twitch campaigns now report the channels they are limited to, the same as Kick, so within the same game a campaign that only counts on certain channels is queued ahead of an open one.
- **Mark drops collected or uncollected from the campaign drawer.** Each account on a drop in the campaign drawer now has a tick to mark it collected (when that account has the drop queued) or a × to mark it uncollected (when it is claimed in GrubDrops or was skipped). The tick is green and the × red, and hovering shows Mark as collected or Mark as uncollected right away. Both take a second click: the tick turns into a green Confirm Collected and the × into a red Confirm Removal. While armed, the cell's status text hides so nothing peeks out beside the button. Mark uncollected also clears a skip, so the account mines the drop again; a drop the platform already shows as claimed comes back on the next scan. The same tick sits at the end of each drop line when you open a game in Inventory's Up next, and the drop leaves the list once confirmed. The tick on Home's Up next and the Mark collected link on Inventory's In progress rows are gone. The × on Inventory's Claimed table stays and now shows Confirm Removal instead of a red tick before it removes. API: `DELETE /api/claims/{account}/{benefit}` now also removes the account's skip for that drop and answers 204 when it removed a claim or a skip, 404 `not_found` when there was neither. `GET /api/campaigns/{platform}/{id}` cells carry `queued`, true when the drop is in that account's plan.
- **Quieter logs.** `runner heartbeat sent` now logs at DEBUG; `runner progress` still shows accrual at INFO once a minute. The "session refresh failed but the current token still verifies" warning now logs once per account and refresh error, then at DEBUG until the refresh succeeds, the token stops verifying, or the error changes. Before, every roster reload repeated it.

### Fixed
- **Box art for games no Twitch campaign has named.** Chosen games added by name or from Kick (Battlefield 6, WARDOGS, Counter-Strike 2) showed the Twitch placeholder, because the art fell back to a name-keyed URL that newer games never answer. After a Twitch scan, the miner now looks up the Twitch id of each chosen game that has none: the exact name first, then a category search that also matches a name with a version number at the end, so "Counter-Strike 2" finds Twitch's "Counter-Strike". It saves the id and uses the id-form art on Games, Home and Discover. A game Twitch does not know is tried again after 6 hours.
- **Finished games no longer show as Waiting.** A Home game card said "Waiting, starts when an account is free" for a campaign whose drops were all claimed or skipped on every account, though Up next had nothing for it. Waiting now needs an account with a drop of that game still queued; otherwise the card shows Done ("Every drop here is claimed or skipped"). Games shows the same Done badge.

## [2.1.2] — 2026-10-27

### Fixed
- **Overwatch drops find channels again.** Twitch renamed "Overwatch 2" to "Overwatch" but kept the `overwatch-2` directory slug, and GrubDrops built the slug from the name (`overwatch`), which names no game, so campaigns open to any channel found nothing to watch. The Twitch backend now asks Twitch for each game's real directory slug once and caches it, falling back to the name-based slug if the lookup fails. Covers every renamed category, not only Overwatch. Reported in #76.

## [2.1.1] — 2026-10-27

### Changed
- README and landing page screenshots show 2.1.0: the Games page with the Lists rail, and Home, Discover and Rewards.

### Fixed
- **Home progress moves while watching.** The minutes and progress bar on Home, Rewards and the account line stayed at the value from when the drop was picked, and only moved after a restart. Each inventory poll and Twitch PubSub progress push now also updates the current drop's minutes, which is what the UI reads. Display only: accrual and claims are unchanged. Reported in #75.
- **Favicon.** The browser tab shows the grub icon again, and phones get the home-screen icon and the purple theme color. The 2.0 UI had no icon links, so every icon path got the app page instead. The root layout now links the 16 and 32 px favicons and the Apple touch icon from `assets/img/`.

## [2.1.0] — 2026-10-26

### Added
- **Ending Soonest priority mode.** A new Settings > Engine section, between General and Accounts, picks the Priority Mode: Priority Order (default) mines games in list order; Ending Soonest mines the campaign that ends first across all chosen games, then the one with fewer minutes left, then game order. Campaigns with no end date go last. Mine now and the Up next order still win. The change replans every account at once, with no restart. Stored as `settings:priority_mode` (same key as 1.x); `GET/PUT /api/settings` gain `priority_mode`.
- **Import a TwitchDropsMiner login.** The first-run wizard has an Import from TwitchDropsMiner tab next to Android login and TV code, Settings > Accounts offers it next to the other Twitch methods, and the Android login step has the same Import TwitchDropsMiner cookies.jar button. It reads the `auth-token` cookie from the file, in both the JSON format of newer aiohttp and the pickle format of older aiohttp, and signs in like a pasted Android URL, so the same Twitch user updates its existing account. The pickle reader only allows the cookie jar's own classes and runs no code. `POST /api/login/twitch/android` takes `jar` (base64) instead of `url`; a file with no Twitch token returns `tdm_no_token`. New dependency: `github.com/nlpodyssey/gopickle`.
- **Scan interval.** Settings > Engine has Check for new campaigns every (15 minutes to 24 hours, default 1 hour). It sets how often a watching account rescans and how often an idle account with nothing to mine looks again. A change replans every account at once, with no restart. Stored as `settings:scan_interval_min`; `GET/PUT /api/settings` gain `scan_interval_min`.
- **Switch to a higher-ranked drop.** On by default: when a higher-ranked campaign has a live stream at a rescan, the account leaves its current drop and mines that one. Turn it off in Settings > Engine to finish the current drop first. A Mine now pin still holds. Stored as `settings:preempt`; `GET/PUT /api/settings` gain `preempt`.
- **Proxy in Settings.** Settings > Engine has a proxy URL (http, https or socks5), an on/off switch and Test, which fetches Twitch through the proxy and shows the time (`POST /api/settings/proxy-test`). The saved password shows as `***`. Applies after a restart. `GET /api/settings` gains `proxy`; `PUT` takes `proxy_url` and `proxy_enabled`.
- **Advanced settings.** Collapsed Advanced blocks at the bottom of three tabs: Kick watch mode on Engine (applies after a restart); Drop progress and Canary failures notifications plus the notification avatar URL on Notifications; the canary interval (applies after a restart) and the Twitch and Kick canary channels on Health. `GET/PUT /api/settings` gain `kick_watch_mode`, `notify_avatar_url`, `canary` and `notify.progress` / `notify.canary`.
- **Link-to-claim notification.** When a platform refuses a claim until the game account is linked, one error notification per drop says to link the account to claim it. It follows the Errors toggle and the UI language.
- **Per-account game list.** The Games page has a Lists rail beside the shared list, with an own-list switch for each account. Turning it on opens the account's list in a dialog, and only Save keeps it on; the account then mines only its own ranked list instead of the shared list. Edit list reopens the dialog. Games are added from the same search as the Games page, or by typing a name, and reordered or removed in the dialog. The first time, the list starts as the 1.x per-account games if the account had any, otherwise as a copy of the global list. Turning it off keeps the stored list for next time. An account row in Settings > Accounts with its own list says so under its name, with a link to Games. Changing the shared list leaves accounts on their own list running. Games only on an account's own list do not show on the Games page. Migration `0022` adds `accounts.own_games`; `GET/PUT /api/accounts/{id}/games`, and accounts gain `own_games` and `own_game_count`.
- **Mark collected and remove a claim.** Each drop in Up next and the In progress drop on Rewards have a Mark collected button: the drop is logged as claimed by hand and the account moves on. A claimed row on Rewards has a remove button (click twice) that deletes the claim record, so the account can mine that drop again; a drop the platform still reports as claimed comes back at the next inventory check. Claims marked by hand say so on Rewards. `POST /api/claims` and `DELETE /api/claims/{account}/{benefit}`.
- **Check login and Restart runner.** An account's menu in Settings > Accounts has Check login now, which asks the platform if the session is still valid and shows the result on the row, and Restart runner, which restarts that account's runner. `POST /api/accounts/{id}/check-login` and `POST /api/accounts/{id}/restart`.
- **Change the admin password.** Settings > General has Admin password: enter the current password and the new one twice. The session stays signed in; every other signed-in browser is signed out. `POST /api/settings/password`.
- **Database snapshot.** Settings > General has Database snapshot, which downloads a consistent copy of the database (`VACUUM INTO`). Account sessions in it stay encrypted with the master key. `POST /api/settings/snapshot`.
- **Test notification.** Settings > Notifications has a Test notification panel that sends a sample drop message to Discord or Telegram, with one button for each one that is set up. With neither set up, it says to connect one first. `POST /api/settings/notify-test`.
- **Canary help.** The canary card on Settings > Health has a help tooltip that explains what the canary checks.
- **Skip onboarding.** The first-run wizard has a Skip onboarding, go to Settings link.
- More environment variables are documented in the README Configuration table: `GRUB_UPDATE_INTERVAL`, `GRUB_AUTHCHECK_INTERVAL`, `GRUB_CANARY_INTERVAL`, `GRUB_LOG_RING`, `GRUB_KICK_SIDECAR_TEMPLATE`, `GRUB_KICK_SIDECAR_PORT`, `GRUB_BROWSER_URL`, `GRUB_BROWSER_URLS`, `GRUB_TWITCH_BROWSER`, `GRUB_AUTH_BYPASS_LOCAL` and `GRUB_AUTHBYPASS`.

### Changed
- **Unlinked games always mine.** A campaign the platform reports as not linked is mined like any other instead of being blocked; only a refused claim blocks the drop, and it is retried as before. Twitch has no link-refusal status, so a failed claim on a Twitch campaign it reports unlinked counts as a link refusal, not toward the 3-failure skip. The Games and Home link warnings also cover these campaigns, and the Home line now reads "Link your game account to claim". `plan.Result` gains `NeedsLink`.
- **Add account in a dialog.** Add account in Settings > Accounts opens a dialog with the same sign-in steps as the first-run wizard instead of an inline panel.
- **Account menu order.** The ⋯ menu in Settings > Accounts is grouped: Rename; Log in again and Check login now; Pause mining and Restart runner; Remove account.
- **Runner settings read from memory.** Runners read the UI language, drop priority, scan interval and switch-to-higher-rank settings from memory instead of the database on every tick and status update. Saving settings refreshes them.
- **TV code sign-in.** The step shows an Open Twitch activate button and a Copy button that copies the full activate link with the code in it; the code itself is no longer shown. Choosing TV code warns that the Discover tab won't work for that account.
- **Onboarding spacing.** The first-run wizard steps have more even spacing.
- **Scan cadence.** The default rescan while watching and the idle recheck are now 1 hour instead of 12 hours, and both follow the new scan interval setting.
- Bumped grpc to 1.83.2, chi to 5.4.0, x/crypto to 0.55.0 and x/net to 0.58.0 (Dependabot security alerts).
- Dependabot no longer proposes chromedp upgrades; chromedp 0.21 breaks the browser sidecar and needs a manual port.
- PR and bug report templates match v2: UI check and test, locale and changelog checklist items, login method, version and full log from Settings.

### Fixed
- **Mark collected sticks.** A drop marked collected by hand stays claimed even when the platform inventory still shows it in progress.
- **Proxy password kept on edit.** Changing the host or port of a saved proxy URL whose password shows as `***` keeps the stored password, for both Save and Test.
- **Proxy auth and https proxies.** A socks5 proxy URL with a user and password now authenticates, and an https:// proxy now talks TLS to the proxy.
- **Twitch claim errors.** A network or server error on a Twitch claim for a campaign Twitch reports unlinked counts as a claim failure, not as a link refusal, so it no longer sends a link notice.
- **Switch to a higher-ranked drop loop.** An account no longer keeps leaving its drop for a higher-ranked campaign whose only streams stalled or failed to start; a failed switch target waits 3 hours.
- **Link notices repeat.** The link-to-claim notification goes out once per drop and account, also across runner restarts and miner restarts.

### Removed
- The unused 1.x `link_override:` kv read path (manual "I've linked it" override); unlinked campaigns mine without it.
- Outdated design docs and 1.x UI mockups for features v2 dropped or replaced: SPA migration, channel whitelist, manual mark-collected, item pulling, settings tabs, SSO login UI.

## [2.0.0] — 2026-10-26

### Added
- **Reorder accounts.** Settings > Accounts has move up, move down and a drag handle on each row, like Games; Shift+click on a move button sends the account to the top or bottom. The order also applies to Home, Up next and Rewards, and Mine now acts on the first running account with a queue in that order. Mining stays parallel. New accounts go last. Stored in a new `accounts.sort_order` column (migration 0021, filled from the current order); `PUT /api/accounts/order` takes the full id list.
- **First-run wizard.** With no accounts, Home opens a full-screen wizard at `/welcome` with a progress bar and Back/Continue: add one or more accounts (Twitch Android login, Twitch TV code or Kick cookies.txt; listed with a remove button, Add another account for more), then pick and order games. Two steps: Start mining on the second goes straight to Home. The Twitch Android paste field shows an example of the redirect URL with the token hidden. An account with no games opens on the games step; an install with accounts and games goes straight to Home. It replaces the old first-run panel on Home. The add-account flow is now one component shared with Settings > Accounts. A new Kick account is created on the first Save of its cookies, not when the form opens, so a reload mid-form leaves no half-made account.
- **Log in again from the account menu.** Every account's ... menu has Log in again, not just signed-out ones, so a working account can switch its Twitch login from TV to Android or refresh its Kick cookies. The session is replaced on the same account.
- **Telegram notifications.** Settings > Notifications takes a bot token and chat ID, with Send test and Change like Discord. The same events, toggles, daily summary and canary alerts go to Telegram (Bot API, HTML, drop image as a photo when there is one), and to both services when both are set. The token is never returned to the UI and is redacted from errors. A 429 waits `retry_after` (up to 10 s) and retries once. `GRUB_TELEGRAM_BOT_TOKEN` and `GRUB_TELEGRAM_CHAT_ID` are the env defaults. `POST /api/settings/notify-test` takes an optional `target` of `discord` or `telegram`.
- **Account link warnings.** A chosen game whose drops an account can't claim until it links the publisher's account (Riot, Steam, ...) shows the account on Games with a Link account button, and a warning line on its Home card. Twitch uses the campaign's link state; Kick flags a campaign after it rejects a claim with a connect URL. Only http(s) link URLs are shown. `GET /api/games` gains `needs_link`. Kick's link flag is saved to `account_campaign_links`, so it survives a restart, and a successful claim clears it. A Check now button next to the warning retries the account's link-refused claims right away and shows what it claimed or that the account is still not linked (`POST /api/accounts/{id}/recheck-links`).
- **Discover tab.** A new page between Games and Rewards lists every current drop campaign on Twitch and Kick, grouped by game: box art, campaign, time left and end date, rewards with images and minutes, and whether an account link is needed. Add to games adds the game in one click; games already chosen show Added. Search, a platform filter and Hide added narrow the list. Hide added is on when the page opens, so games already on your list stay out of the way. `GET /api/discover` caches for 15 minutes (2 minutes after a failure) and fetches both platforms at once; `POST /api/discover/refresh` bypasses the cache at most once a minute. A platform that fails or times out is reported on its own while the other still lists. Twitch is read through an enabled Android-login account; without one the page says so and links to Settings > Accounts.
- **Shift+click to move to the end.** On Games, Shift+click on Move up sends the game to the top and on Move down to the bottom. A plain click still moves one step.
- **Rename accounts.** Settings > Accounts has Rename in each row's menu. The custom name shows everywhere the account is named (Home, Rewards, Health, notifications) and survives logins; an empty name resets to the platform name. Max 40 characters. Stored in a new `accounts.custom_name` column (migration 0019), so two accounts with the same platform name can be told apart.
- **Twitch Android login.** Adding a Twitch account offers Android login (recommended) next to the TV code. Sign in to Twitch in a new tab, then copy the whole URL of the Twitch homepage it lands on and paste it in. Android tokens see the drops dashboard, so campaign discovery is complete; TV tokens get null from ViewerDropsDashboard and DropCampaignDetails. Adding the same Twitch user again updates its session.
- **Mine now.** The Up next header on Home and Rewards has one Mine now button. It stops the current drop and starts the first Up next drop at once; the stopped drop moves to the top of Up next. The switch holds through rescans, including Twitch TV-token scans that briefly miss the campaign, until the drop is claimed, ends, is blocked or has no live stream. The button is hidden when Up next is empty or no listed account is running.
- **Scan now.** A button at the top right of Games asks every account to rescan campaigns at once. It replaces the "GrubDrops only mines these" line.
- **Up next on Rewards.** A read-only Up next section lists each account's queued drops (5 per account, with Show more), replacing the "more drops queued" line.
- **Home game card queue.** A waiting game's card lists at most 3 queued drops and counts the rest.
- **Rewards search.** A search box filters claim history by reward, campaign or game on the server, so it covers every claim, not just the loaded ones.
- **Rewards pagination.** Previous / Next with page and range counts replace Load more; page size is 20, 50 or 100 and is kept per browser.
- **Home shows at most 7 games.** Games that are mining or queued stay visible. A "Show N more games" button under the last card expands the rest in place, and "Show fewer" folds them back.
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
- **README rewritten for v2, new screenshots.** The README is shorter and covers Android login, Discover, per-account Up next and the first-run wizard. The Spanish and Chinese versions match it. `docs/screenshots` has new v2 shots of Home, Discover, Games and Rewards made with sample data; the old settings shot is removed.
- **Mine now is now Mine next, per account.** On Home, each account's Up next header has its own Mine next button instead of one in the panel header. It starts that account's first Up next drop. It is disabled with a tooltip when the account is signed out, has no games or is idle, and a failure shows under that account. Rewards now shows the same Up next panel as Home, so its queue can be reordered by drag or move buttons and has the same Mine next buttons.
- **Top bar order.** Home, Discover, Games, Rewards, Settings.
- **Home game cards fill the left column.** On wide screens Home shows as many game cards as fit down to the bottom of the right column (Up next and Claimed), instead of a fixed 7, with at least 2. The narrow single-column layout keeps 7.
- **Login and setup page.** A tighter centered card with the host below it. Password comes first; when SSO is on it sits under an "or" rule. Signing in shows a spinner and locks the field, a wrong password marks the field, and setup shows the 8-character hint and a mismatch error on the confirm field. After setup the app opens the first-run wizard.
- **Light mode.** The light theme is one step darker: an off-white background instead of pure white, with darker surfaces, borders and text colors to match (GRUB-22).
- **Scan cadence.** A watching account now rescans campaigns every 12 hours instead of every 15 minutes. Use Scan now on Games for a sooner one.
- **Health log colors.** Each log line is tinted by its type: red for errors, amber for warnings, green for claims. Normal lines use the main text color instead of grey. The account column stays neutral so only the message carries the color, entries have a thin line between them, and the dot sits on the first line when a message wraps.

- **Quieter logs.** Repeated lines log once per change: `runner progress` only when minutes change, the same step error once (with a recovery line carrying the suppressed count), and unchanged state, picked-drop and starting-watch lines are skipped. `notify` lines moved to DEBUG, and the Twitch campaigns diagnostic no longer logs the raw response body.
- **Home card size.** Cards no longer show a rank number, and card size follows the order on the page, so an idle top-priority game listed below active ones is small. A waiting game now lists above games with no campaign even when nothing is queued for it yet.
- **Up next account names.** Each name is tinted by platform: purple for Twitch, green for Kick.

- The miner runs the v2 planner, runner and collector and serves the JSON API and the embedded SPA.
- The pool restarts a panicked runner from its builder after one minute.
- `GET /api/events` streams every ring log line as a `log` event, and open streams end at shutdown instead of holding it for 5 s.
- Adding or reordering chosen games returns at once. Runners reload in the background, one reload at a time with repeated changes folded together, and a `games` event fires when the reload finishes.
- A campaign with no live stream moves on to the next queued campaign without rescanning, and every pick retries campaigns that had no live stream earlier.
- Swept rewards are recorded in the claim log under their planned game.
- README, translations, DESIGN.md, the site and AGENTS.md describe the SvelteKit UI, chosen games, the planner, the runner and the new login flows. A source build says to build the UI before `go build`.
- Home lists games that are mining or queued first, then the rest, each in priority order. Cards keep their real rank number.
- Up next on Home shows 5 drops per account, with Show more / Show less for the rest. Each account's queue is headed by its name next to Mine next, even when only one account has drops queued.
- The Health event log shows 10 events, with Show all for the rest of the last 50. DEBUG lines are left out of the list and its counts; Download full log still has them.
- With no campaign to mine, a runner scans again after 12 hours instead of 5 minutes. Changing the chosen games or an account still rescans at once. Queued campaigns with no live stream, and awaiting a channel connect, still recheck every 5 minutes. The empty Up next line counts down to the server's next scan time, in hours and minutes.
- The README and site screenshots show the current Home, Games, Rewards and Settings pages.

### Fixed
- Kick sidecar cleanup only removes sidecars named by this miner's own template, so two miners on one docker host (prod and staging) no longer delete each other's sidecars.
- **Drag to reorder works.** Games, Settings > Accounts and Up next now pick a row up from anywhere on it (not only the dotted handle) after a 4 px move, so clicks still work. The drag no longer ends after one slot, no text gets selected while dragging, and Escape cancels. The dragged row is lifted with a shadow and accent border. One shared helper, `web/src/lib/drag.ts`, serves all three lists.

- **Discord webhook URL in logs.** A failed Discord send no longer puts the webhook URL, whose path is the secret, in its error or the log.
- **A drop that needs an account link no longer blocks the runner.** A Kick claim rejected for a missing link was retried, then skipped like any failure. The runner now marks the drop not linked, moves on to the next Up next target, and retries the claim every 5 minutes, so it lands once the account is linked. The link rows had also never saved: their campaigns foreign key failed on every insert, and migration 0020 drops it.
- **Twitch Android accounts never mined.** DropCampaignDetails reads its `channelLogin` variable as a user ID, but GrubDrops sent the login, so Twitch answered `user: null`, every campaign came back with no drops, and Up next stayed empty. It now sends the account's numeric user ID (from the session, or CurrentUser). An empty details answer is logged and no longer cached for 30 minutes.
- **Kick watch time never accrued from EU hosts.** Kick now shows a OneTrust consent banner, and its player does not request the stream until the visitor picks an option, so the sidecar's video stayed at readyState 0 and every watch gave up with "player never started". The sidecar now accepts the banner each time it drives the player. A watch that still fails to start logs the page URL without its query, title, video state and error, whether the banner is showing, and whether the playback API and an HLS playlist were requested (no cookie or token values). Each player poll now has a 10 s timeout.
- **Home order.** A game that is mining is listed above games that are only waiting, whatever their priority.
- Twitch watch time was not crediting even though the Spade beacon was accepted. Each heartbeat now fetches the stream's lowest-quality HLS playlist and sends a HEAD request to every chunk in it, the method TwitchDropsMiner moved to. The Spade beacon is still sent, and a heartbeat fails only when both fail. Playlist and chunk requests go only to ttvnw.net and live-video.net over https and carry no Twitch credentials.
- Home cards are large by display position, so a mining game shown first is large. The rank on the card is unchanged.
- Rewards search skips the reload when the text is unchanged, uses 16px text on phones so iOS does not zoom, and trims spaces after the 100 character cut.
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
- Progress notifications fire again (start, 50% and 100%) when turned on in Settings > Notifications > Advanced.
- `log` events take the account name from memory, so logging never waits on the database.
- Shutdown waits for background loops before closing the Twitch backends and the database, including after an HTTP listen failure.
- Kick avatars on runners, the overview and `runner` events go through the Kick image proxy, so they load in the browser.
- Every API time field is an RFC 3339 UTC string, and an unset time is `null` instead of `0001-01-01T00:00:00Z`.
- Release notes with two equal lines, or a Twitch and a Kick campaign with the same id, no longer crash the page on a duplicate list key.
- A failed OIDC sign-in redirects to `/login?login_error=oidc`, so the login page shows the SSO error.
- A Twitch device status the UI does not know ends the flow with the error line instead of a blank panel.
- Settings shows no empty strip under Accounts when there are none, and the Health "Last scan" dot stays grey until a scan has run.
- The Discord webhook field is no longer a password input, so browsers stop autofilling the admin password into it. It saves only on Save or Enter, a URL that is not a Discord webhook shows an inline message, and language and timezone save only on a real change.
- The health canary re-runs when an account is resumed or signs in and its platform's last result was Skipped, instead of showing Skipped until the next 6-hour run. A check already running is followed by one more.
- **Box art missing on Discover, Games and Home.** Box art was built from the Twitch game ID as `<id>-144x192.jpg`, which newer IGDB-backed games such as ARKNIGHTS: ENDFIELD or Captain's Call redirect to Twitch's placeholder, and Kick campaigns used the name form, which only older games answer. Twitch campaigns now use the GQL `game.boxArtURL` with its size filled in, then the `<id>_IGDB-144x192.jpg` form. Kick campaigns use the category image through the Kick image proxy, then the same game's Twitch art from the same Discover result.
- **"Needs account link" on almost every Twitch campaign.** Twitch leaves `self.isAccountConnected` false on many campaigns that need no link, with an empty or twitch.tv `accountLinkURL`. Discover and the planner (and so the Games link warning and Awaiting connect) now treat a Twitch campaign as needing a link only when it is not connected and its link page is off twitch.tv, such as a publisher's connect page.

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
