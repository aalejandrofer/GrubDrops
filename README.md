<p align="center">
  <img src="internal/web/static/img/logo.svg" width="88" alt="GrubDrops logo">
</p>

<h1 align="center">GrubDrops</h1>

<p align="center"><b>Self-hosted Twitch &amp; Kick drops miner.</b><br>
Pick your games, it watches the right streams, mines the drops, and claims them — hands-free.</p>

<p align="center">
  <img alt="Go" src="https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white">
  <img alt="Twitch" src="https://img.shields.io/badge/Twitch-drops-9146FF?logo=twitch&logoColor=white">
  <img alt="Kick" src="https://img.shields.io/badge/Kick-drops-53FC18?logo=kick&logoColor=black">
  <img alt="UI" src="https://img.shields.io/badge/UI-HTMX%20%2B%20Go%20templates-2c2c2c">
  <img alt="Storage" src="https://img.shields.io/badge/DB-SQLite-003B57?logo=sqlite&logoColor=white">
  <img alt="Self-hosted" src="https://img.shields.io/badge/self--hosted-Docker-2496ED?logo=docker&logoColor=white">
</p>

<p align="center">
  <img src="docs/screenshots/console.png" width="900" alt="GrubDrops Console — lifetime stats, currently-mining per account, live event feed">
</p>

---

**Keywords:** twitch drops miner · kick drops miner · self-hosted · multi-account · headless · auto-claim · twitch drops bot · kick drops bot

## Why

A whitelist-driven drops miner for **both Twitch and Kick**, multiple accounts each, from one dashboard. You choose the games; it never touches anything else. No browser farm, no clicking — it authenticates the way each platform actually allows, watches an eligible live channel that's playing your game, accrues watch-time, and claims drops as they complete.

## Features

- 🎯 **Whitelist-driven** — a global priority list or a per-account list; only your games are ever watched/mined.
- 🟣🟢 **Twitch + Kick**, multiple accounts each — one dashboard, watch → mine → claim, automatically.
- ✅ **Game-aware channel pick** — only watches a stream actually playing the campaign's game (no wasted watch-time).
- 🔗 **Connection-aware** — campaigns needing an external account link (Krafton/PUBG, Embark/ARC…) are flagged per-account and skipped until linked, with a manual *"I've linked it"* override.
- 🟰 **Mineable-if-any** — with several accounts, a campaign stays in the mined list if *any* linked account can earn it; the rest get a *connect* nudge.
- 🖥️ **Live console** — real-time event feed, currently-mining panel, `/drops` catalog (past / current / upcoming + discoverable), and a `/history` claim log.
- 🩺 **Auth health** — periodic per-account token/cookie checks + a manual *Check auth* button.
- 🔔 **Discord** notifications — rich embeds (drop image, game, channel, account) with per-kind toggles (claims / progress / auth / errors).

## How it works

- **Twitch** — official **device-code** OAuth (no password or cookies pass through GrubDrops). GraphQL against the Android client + PubSub for real-time progress/claim/stream events; a 60s minute-watched beacon credits watch-time.
- **Kick** — Kick's API sits behind Cloudflare bot-management that 403s any browser-driven client. GrubDrops talks to it over a pure-HTTP client with a **real Chrome TLS fingerprint** (`utls` + HTTP/2) — no browser, no `cf_clearance`. Watch presence runs over Kick's viewer WebSocket.
- **Discovery** scrapes the active catalog every few minutes and persists campaigns + benefits to SQLite, so the UI stays populated even when nothing is actively mining.

## Pages

| Page | What |
|------|------|
| **Console** (`/`) | Lifetime stats, currently-mining per platform/account, live event feed. |
| **Drops** (`/drops`) | Whitelisted past/current/upcoming + discoverable campaigns; per-campaign items, collection marks, connect chips, one-click whitelist. |
| **History** (`/history`) | Claim log across accounts. |
| **Settings** (`/settings`) | Global priority list, runtime intervals, Discord, log level, **change master password**. |
| **Accounts** | Add accounts, per-account whitelist, re-auth, auth-health. |

## Quick start

```bash
go build -o grubdrops ./cmd/miner
MINER_MASTER_KEY=$(head -c32 /dev/urandom | base64) ./grubdrops   # → http://localhost:8080
```

First run creates the admin login. Add a Twitch account → approve a **device code** at `twitch.tv/activate`. Add a Kick account → download the **helper** from the Kick login page (or paste cookies).

## Configuration

All via env vars:

| Var | Default | Purpose |
|-----|---------|---------|
| `MINER_MASTER_KEY` | — (required) | Key for age-encrypted session storage. |
| `MINER_HTTP_ADDR` | `:8080` | Listen address. |
| `MINER_DISCOVERY_INTERVAL` | `5m` | Catalog scrape cadence. |
| `MINER_AUTHCHECK_INTERVAL` | `12h` | Auth-health sweep cadence. |
| `MINER_DISCORD_WEBHOOK_URL` | — | Optional global Discord webhook. |
| `MINER_HELPER_DIR` | `/helpers` | Where the baked cookie-helper binaries live. |
| `MINER_LOG_LEVEL` | `info` | `debug` / `info` / `warn` / `error`. |

## Deploy

`linux/amd64` container (built from `deploy/Dockerfile.miner`). Build → transfer the image → `docker compose up`. The image bakes the cross-compiled cookie helpers (served at `/download/helper`). Persist `/data` (SQLite) across redeploys; route behind a reverse proxy; `/healthz` returns `ok`.

## Project layout

```
cmd/miner               main daemon
cmd/dropsminer-helper   cookie helper CLI (cross-compiled into the image)
internal/platform/twitch · kick   per-platform backends
internal/watcher        per-account state machine (watch/mine/claim)
internal/discovery      catalog scraper
internal/api + web      HTMX UI + handlers
internal/store          SQLite (sqlc + goose), age-encrypted sessions
```

## Status & notes

Active development. Twitch mining + claiming and Kick discovery/watch run in production. Self-hosted, single-tenant. Use responsibly and within each platform's Terms of Service — you run it against your own accounts at your own risk.

---

<sub>Built by <a href="https://github.com/aalejandrofer">@aalejandrofer</a> with <a href="https://claude.com/claude-code">Claude Code</a> (Opus 4.8). See <a href="CHANGELOG.md">CHANGELOG</a> · <a href="docs/DESIGN.md">DESIGN</a>.</sub>
