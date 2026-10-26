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
- **Telegram stays disabled with "Coming in v1.5.0".** That text lives in one locale key, so it is a one-line change if a v2 release should say "Not available yet".
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
