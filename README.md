<p align="center">
  <img src="internal/web/static/img/logo.svg" width="84" alt="GrubDrops">
</p>

<h1 align="center">GrubDrops</h1>

<p align="center">
  A self-hosted, whitelist-driven drops miner for <b>Twitch</b> and <b>Kick</b>.<br>
  Watch the right streams, mine the drops you actually want, claim them automatically.
</p>

---

## What it does

- **Whitelist-driven.** You pick the games (global priority list or per-account). GrubDrops only ever watches/mines whitelisted games — nothing else is touched.
- **Twitch + Kick.** One dashboard, both platforms, multiple accounts each.
- **Watch → mine → claim.** Picks an eligible live channel actually playing the campaign's game, accrues watch-time, and claims drops as they complete.
- **Connection-aware.** Campaigns that need an external account link (Krafton/PUBG, Embark, …) are surfaced separately with per-account "connect" chips, and skipped until linked — with a manual "I've linked it" override.
- **Live console.** Real-time event feed, currently-mining panel, per-account state, auth-health checks, and a `/drops` catalog (past / current / upcoming + discoverable).
- **Discord notifications.** Per-kind toggles (claims / progress / auth / errors) with rich embeds (drop image, game, channel).

## How it works

- **Twitch** — direct GraphQL against the Android client, authenticated via the **device-code** flow (no password/cookies through GrubDrops). Real-time progress/claim/stream events over PubSub; a 60s minute-watched beacon credits watch-time.
- **Kick** — Kick's API sits behind Cloudflare bot-management that 403s any browser-driven client. GrubDrops talks to it over a pure-HTTP client with a **real Chrome TLS fingerprint** (`utls` + HTTP/2) — no browser, no `cf_clearance`. Watch presence runs over Kick's viewer WebSocket.
- **Discovery** scrapes the active catalog every few minutes and persists campaigns + benefits to SQLite, so the UI is populated even when nothing is actively mining.

## Stack

Go · `html/template` + HTMX (no SPA) · SQLite (sqlc + goose) · age-encrypted sessions · Docker. No JavaScript build step.

## Quick start

```bash
# build + run locally
go build ./cmd/miner
MINER_MASTER_KEY=$(head -c32 /dev/urandom | base64) ./miner
# open http://localhost:8080 → first-run setup creates the admin login
```

Key env vars: `MINER_MASTER_KEY` (session encryption), `MINER_HTTP_ADDR`,
`MINER_DISCOVERY_INTERVAL` (default 5m), `MINER_AUTHCHECK_INTERVAL` (default 12h),
`MINER_DISCORD_WEBHOOK_URL` (optional global webhook).

## Deploy

`linux/amd64` container. Build, transfer the image, and `docker compose up` —
the compose file references a local `grubdrops`/`dropsminer:latest` tag and
persists `/data` (SQLite) across redeploys. Routed behind a reverse proxy;
`/healthz` returns `ok`.

## Status

Active development. Twitch mining + claiming and Kick discovery/watch are
working in production.
