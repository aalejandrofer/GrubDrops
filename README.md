<p align="center">
  <img src="internal/web/static/img/logo.png" width="240" alt="GrubDrops">
</p>

<p align="center"><b>Self-hosted Twitch &amp; Kick drops miner.</b><br>
You pick the games. It watches the right streams, racks up the watch-time, and claims the drops for you.</p>

<p align="center">
  <img alt="Go" src="https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white">
  <img alt="Twitch" src="https://img.shields.io/badge/Twitch-drops-9146FF?logo=twitch&logoColor=white">
  <img alt="Kick" src="https://img.shields.io/badge/Kick-drops-53FC18?logo=kick&logoColor=black">
  <img alt="UI" src="https://img.shields.io/badge/UI-HTMX%20%2B%20Go%20templates-2c2c2c">
  <img alt="Storage" src="https://img.shields.io/badge/DB-SQLite-003B57?logo=sqlite&logoColor=white">
  <img alt="Self-hosted" src="https://img.shields.io/badge/self--hosted-Docker-2496ED?logo=docker&logoColor=white">
  <img alt="License" src="https://img.shields.io/badge/license-MIT-green">
</p>

<p align="center">
  <img src="docs/screenshots/console.png" width="900" alt="GrubDrops console: watch-time stats, per-account mining across Twitch and Kick, and a live event feed">
</p>

---

Tell GrubDrops which games you care about. It finds a live channel that's actually playing one of them, keeps a viewer present so the watch-time counts, and claims each drop the moment it's ready. No browser farm, no tabs to babysit, no clicking. Add as many Twitch and Kick accounts as you like and run them all from one dashboard.

## What it does

- **You set a whitelist** (one global list, or a per-account one) and nothing outside it ever gets mined.
- **Twitch and Kick together**, several accounts each, all on one page.
- **It checks the game.** A stream has to actually be playing the campaign's game, so you never burn watch-time on the wrong thing.
- **It knows about account links.** Campaigns that need an external account connected (Krafton, Embark, and friends) get flagged per account, and there's an "I've linked it" override when the check is being stubborn.
- **A live console** shows lifetime stats, what's mining right now, the drops catalog, and a claim history.
- **Discord notifications** if you want them, with toggles per event type.

## How it works

Twitch and Kick don't let you in the same way, so GrubDrops talks to each one on its own terms:

- **Twitch** uses the official device-code login (your password and cookies never touch GrubDrops), then GraphQL plus PubSub for live progress and claims.
- **Kick** has no public API and sits behind Cloudflare, so GrubDrops speaks to it over a plain HTTP client wearing a real Chrome TLS fingerprint (`utls`). No headless browser, no `cf_clearance` dance. Watch-time is kept alive over Kick's viewer WebSocket.
- **Discovery** sweeps the active catalog every few minutes into SQLite, so the dashboard stays useful even when nothing's actively mining.

## Pages

| Page | What's on it |
|------|------|
| **Console** (`/`) | Lifetime stats, what each account is mining, a live event feed. |
| **Drops** (`/drops`) | Past, current, upcoming and discoverable campaigns; per-campaign items, collection marks, connect chips, one-click whitelisting. |
| **History** (`/history`) | The claim log across every account. |
| **Settings** (`/settings`) | Global priority list, intervals, Discord, log level, change master password. |
| **Accounts** | Add accounts, edit per-account whitelists, re-auth, check auth health. |

## Quick start

```bash
go build -o grubdrops ./cmd/miner
MINER_MASTER_KEY=$(head -c32 /dev/urandom | base64) ./grubdrops   # http://localhost:8080
```

The first run asks you to create an admin login. For Twitch, add an account and approve the device code at `twitch.tv/activate`. For Kick, download the helper from the Kick login page (or paste your cookies in by hand). Channels auto-discover from each campaign's game, so there's nothing else to configure.

## Configuration

Everything is set through environment variables:

| Var | Default | Purpose |
|-----|---------|---------|
| `MINER_MASTER_KEY` | required | Key for the age-encrypted session store. |
| `MINER_HTTP_ADDR` | `:8080` | Listen address. |
| `MINER_DISCOVERY_INTERVAL` | `5m` | How often the catalog is scraped. |
| `MINER_AUTHCHECK_INTERVAL` | `12h` | How often auth health is swept. |
| `MINER_DISCORD_WEBHOOK_URL` | none | Optional global Discord webhook. |
| `MINER_HELPER_DIR` | `/helpers` | Where the baked cookie-helper binaries live. |
| `MINER_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, or `error`. |

## Deploy

It ships as a `linux/amd64` container built from `deploy/Dockerfile.miner`. Build it, move the image to your host, and `docker compose up`. The image bakes in the cross-compiled cookie helpers (served from `/download/helper`). Keep `/data` (the SQLite file) across redeploys, put it behind a reverse proxy, and `/healthz` will tell you it's alive.

## Project layout

```
cmd/miner               main daemon
cmd/grubdrops-helper    cookie helper CLI (cross-compiled into the image)
internal/platform/...   per-platform backends (twitch, kick)
internal/watcher        per-account state machine (watch, mine, claim)
internal/discovery      catalog scraper
internal/api + web      HTMX UI and handlers
internal/store          SQLite (sqlc + goose), age-encrypted sessions
```

## Credits

GrubDrops stands on the shoulders of two excellent projects that figured out the hard parts first:

- **[DevilXD/TwitchDropsMiner](https://github.com/DevilXD/TwitchDropsMiner)** for the Twitch side: the device-code flow, the GraphQL queries, and the watch-time mechanics all trace back to it.
- **[HyperBeats/KickDropsMiner](https://github.com/HyperBeats/KickDropsMiner)** for the Kick side, which mapped out how Kick drops work in the first place.

GrubDrops is its own Go rewrite with a web UI and multi-account support, but it wouldn't exist without their groundwork. Thank you.

## Notes

Still actively developed. Twitch mining and claiming plus Kick discovery and watching run in production. It's self-hosted and single-tenant. Use it responsibly and within each platform's Terms of Service, against your own accounts, at your own risk.

---

<sub>Built by <a href="https://github.com/aalejandrofer">@aalejandrofer</a> with <a href="https://claude.com/claude-code">Claude Code</a>. See the <a href="CHANGELOG.md">changelog</a> and <a href="docs/DESIGN.md">design notes</a>.</sub>
