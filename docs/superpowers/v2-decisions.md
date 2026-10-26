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
