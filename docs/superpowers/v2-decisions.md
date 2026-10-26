# v2 critical decisions

Decisions made while building v2 on `rebuild/v2` without check-ins. Each entry gives the decision, the reason and the cost if it is wrong.

## Plan 3: API, SSE, miner wiring

- **Per-account Discord webhooks removed.** Every notification goes to the global webhook. Reason: the spec says global only, and v2 has no UI to see or clear per-account webhooks. Cost: users who relied on a per-account webhook lose it. The `accounts.webhook_url` column stays until plan 5 drops old tables.
- **Do not tag before plan 5.** Nothing seeds `chosen_games` from 1.x yet, so upgraded accounts boot with no games.
- **Twitch integrity-token log line not fixed in plan 3.** `internal/platform/twitch/client.go:234` logs a token prefix at Info. It is tracked as GRUB-20 because platform code was out of scope.
- **`POST /api/logout` is public.** An unauthenticated call is a CSRF-gated no-op that returns 204, so a client can clear a stale cookie.
- **No v2 setter for proxy, canary channels and interval, Kick watch mode or notify avatar.** The spec drops those pages. Stored values still apply.

## Plan 4: SvelteKit UI

- **The r4 mockups are the visual target as-is.** Jandro approved them on 2026-10-25.
- **The app lives in top-level `web/`.** It uses SvelteKit adapter-static, Svelte 5, TypeScript and npm. It builds into `internal/web/dist`; git tracks only `dist/.gitkeep`. Go builds and tests without Node, and serves a "UI not built" page when `index.html` is missing. The Docker image gains a Node build stage.
- **Locales stay in `internal/i18n/locales/`, and Svelte imports them from there.** The 1.x-only keys are deleted, and a check enforces equal key sets.
- **Settings order is General, Accounts, Notifications, Health, Updates.** The r4 HTML and the spec agree; the r4 brief text listed Accounts first.
- **Telegram notifications are built.** Settings > Notifications saves a bot token and chat ID next to the Discord webhook, stored the same way (plain in settings), and the token is shown masked. Events, toggles, the daily summary and canary alerts fan out to Discord and Telegram. `GRUB_TELEGRAM_BOT_TOKEN` / `GRUB_TELEGRAM_CHAT_ID` are the env defaults.
- **r4 filler lines are dropped** ("After that it runs on its own.", "We'll watch for one."). The empty queue reads "Nothing queued. Next scan in N min."
- **Health messages are localised by exact English `msg` lookup.** Events gain an allowlisted `fields` object. A stable event code is a follow-up.
- **Next scan time is computed in the browser** as last scan plus 5 min. Exposing `next_scan_at` is a follow-up.
- **Game search shows no "Campaign live now" lines.** The search API has no campaign info.
- **Removing a game sends `PUT /api/games` without it.** There is no game DELETE.
- **The header problem chip follows overview `problems` only.** An account that never logged in shows only on Settings Accounts. Fixing this on the Go side is listed for plan 5.
- **Up next has one queue per account,** with an account header when more than one account has a queue.
- **Canary status is a fourth Health stat with "Run now".** The spec needs it; r4 had no slot.
- **Settings General gains a Timezone row and a "Sign out" button.** `GET /api/settings` gains `zone_effective` so the browser can show the server's zone.
- **Plan 4 tasks run one at a time,** with no parallel worktrees, to avoid locale-file merge conflicts.
- **Live accounts:** page checks use fixture data. The final walk also logs in Jandro's Kick account on the local miner from the cookies he supplied. The new version has no Twitch cookie login, so Twitch is tested through fixtures and the device-code screen only.
- **v2's collection migration is `0017`, not `0016`.** The 1.x `feat/pipeline-v2` build already used version 16 on staging and prod, so goose skipped v2's tables. `0017` uses `IF NOT EXISTS`, so DBs that ran the old v2 `0016` upgrade cleanly.
- **Accounts keep the 1.x `enabled` flag as Pause/Resume.** A disabled account shows "Paused" with "Resume mining". The upgrade never flips the flag, so staging does not start mining on sessions it shares with prod.

## Plan 5: import, box art, health, logs

- **The 1.x import runs once, on first v2 boot,** guarded by kv `v2.legacy_import_done`. It fills `chosen_games` only if empty: the global list by rank, then per-account games, unioned by best rank then name. Claims go to `claim_log` with source `1.x`. Total minutes per account land in `watch_minutes` under day `''`. Verified on a copy of the staging pre-v2 DB: 29 games, 821 claims, 4 minute rows. A second boot does not re-import.
- **Box art uses the Twitch game ID** (`ttv-boxart/<id>-144x192.jpg`, migration 0018 `games.twitch_game_id`). Name URLs return a placeholder for some games (Marvel Rivals, Delta Force). Games without an ID fall back to the name URL until the first Twitch scan.
- **A stable health event code was dropped.** No consumer, so YAGNI.
- **The canary with no enabled session saves a skipped result and sends no notification.** A skip does not count as a previous result. The copy tells the user to sign in or resume an account.
- **Run now decides "done" by a change in the server's `checked_at`** versus a snapshot taken before the click, never by comparing with the browser clock. It polls every 5 s and times out after 90 s of browser elapsed time.
- **The Twitch client logs no integrity token and no raw gql or PubSub response bodies** (GRUB-20). Follow-up: the browser sidecar (`internal/auth/browser/sidecar/twitch.go`) still logs truncated gql bodies.
- **Staging accounts were not resumed during testing,** because they may share sessions with prod. Box art by ID and the next-scan stamp were verified by unit tests only.

## Post-plan UI feedback (2026-10-26)

- **Home shows at most 7 game cards.** Active games (mining or waiting, with queued drops) come first, so a lower-priority game that is mining is never hidden. Games with no campaign follow, in priority order. Under the cards, "N more games tracked" links to Games.
- **Games has a Cards / List toggle.** The list view shows only the rank, a grip, the up and down buttons, and the name, so 30 games fit on about one screen. The choice is saved per browser in `grub.games.view`.
- **Rewards search runs on the server** (`q` on `GET /api/claims`), matching reward, campaign or game. sqlc rejects `LIKE ... ESCAPE`, so the query uses `like(pattern, col, '!')` and Go escapes the pattern. The 20/50/100 pager replaces "Load more". Page size is saved per browser in `grub.rewards.page_size`.
- **Up next shows 5 items per account,** and each account expands on its own.
- **The Health event list shows 10 rows and expands.** DEBUG lines are left out of the UI feed and its counts, but the full-log download keeps them.
- **The idle recheck is 12 h,** at Jandro's request. "Nothing live" means few scans, and a campaign that starts while idle can wait up to 12 h. The rebuild while watching stays at 15 min, and the Twitch beacon tick is unchanged.
- **The canary re-runs when an account is resumed or signs in** while the stored result is "skipped". Otherwise Health kept saying "Skipped" for up to the 6 h canary interval.
- **The "Failing:" canary with an empty detail** came from a browser tab that still ran an older SPA bundle. The API returned `skipped: true`, and a fresh load shows "Skipped". There was no server fault.
- **The rebuild while watching is now 12 h too,** at Jandro's request ("evry 12hrs is good"). A Scan now button on Games forces a rescan of every account at once. The 5 min recheck while campaigns are queued but no stream is live stays, since it waits for a stream to go live. A campaign that starts mid-watch can wait up to 12 h unless Scan now is pressed.
- **Mine now writes the session order** (chosen drop, then the drop it replaced, then the rest of Up next), so the switch survives later rescans and Reset to Games order undoes it. The HTTP handler only sets a flag; the Run goroutine stops the watch and re-picks, and a claim in flight finishes first.
- **Mine next is a button per account on Home (Rewards embeds the same panel), and the server picks the target.** `POST /api/runners/{id}/mine-now` takes no body and starts that runner's current Up next item 0, so a stale client list cannot send a drop the runner no longer queues. With several accounts in Up next, the button acts on the first listed account that is running and has a queue; its tooltip names the drop. The runner also pins the target: Twitch TV discovery samples the top live channels per game, so the rescan right after the switch can miss the campaign, and the order alone then fell back to the old drop. The pin holds while a scan merely omits the target and drops when the target is claimed, skipped, blocked, ended or has no live stream.

## Twitch Android login (2026-10-26)

- **Android login is a paste flow, not a sidecar-driven browser.** The user signs in on twitch.tv in their own browser with the Android client id and pastes the `#access_token=` address back. The browser sidecar has no interactive login UI, and 2FA, captchas and email codes need the user's own browser. The paste flow also works with no sidecar running.
- **The token is checked at `id.twitch.tv/oauth2/validate`** and rejected unless its `client_id` is the Android client. The response gives the user id and login. The user id is kept inside the encrypted session, so adding the same Twitch user again updates its account instead of making a second one.
- **Android paste sessions have no refresh token.** Refresh re-validates the token and pushes `ExpiresAt` forward. A 401 from validate marks the account signed out. Sessions marked `android` skip the "no refresh token" idle in session load.

## Account order (2026-10-26)

- **Account order is for display and one pick; mining stays parallel.** `accounts.sort_order` (migration 0021, backfilled from creation order) sorts `ListAllAccounts` and `ListEnabledAccounts`, so Settings > Accounts, Home rows, Up next, runners and Rewards follow it. Mine now acts on the first running account with a queue, and that now means the first in this order. Every runner still mines at once, and a reorder does not reload runners.
- **`PUT /api/accounts/order` takes the full id list.** Unknown, repeated or missing ids get 400, so a stale client cannot silently drop an account from the order. New accounts take max + 1 and land last. The same up/down buttons, Shift+click and drag as Games, with an optimistic update that rolls back on error.
