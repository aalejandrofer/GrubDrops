<p align="center">
  <img src="internal/web/static/img/logo.png" width="160" alt="GrubDrops">
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

## Quick start

Two published images — the miner and a codec-enabled Chrome sidecar that
earns the Kick watch-time:

```yaml
# compose.yml
services:
  miner:
    image: ghcr.io/aalejandrofer/grubdrops:latest
    restart: unless-stopped
    ports: ["8080:8080"]
    environment:
      GRUB_MASTER_KEY: ${GRUB_MASTER_KEY:?run: head -c32 /dev/urandom | base64}
      GRUB_DB_PATH: /data/miner.db
      GRUB_KICK_BROWSER_WATCH: "1"
      GRUB_SECURE_COOKIES: "0"   # plain-HTTP localhost; set 1 behind HTTPS
    volumes:
      - ./data:/data
      # lets the miner start/stop browser sidecars on demand
      - /var/run/docker.sock:/var/run/docker.sock

  # one per Kick account; name must be grubdrops-browser-<username-slug>
  grubdrops-browser-myuser:
    image: ghcr.io/aalejandrofer/grubdrops-browser:latest
    container_name: grubdrops-browser-myuser
    restart: unless-stopped
    expose: ["9090"]
```

```bash
GRUB_MASTER_KEY=$(head -c32 /dev/urandom | base64) docker compose up -d
# → http://localhost:8080 — first run asks you to create an admin login
```

No Kick accounts (Twitch only)? The miner image alone is enough — drop the
sidecar service, the socket mount and `GRUB_KICK_BROWSER_WATCH`.

The full reference compose (sidecar profiles, OIDC, every knob commented) is
[`deploy/docker-compose.yml`](deploy/docker-compose.yml). Prefer building
yourself? `docker build -f deploy/Dockerfile.miner .` or plain
`go build ./cmd/miner`.

## Add accounts

**Twitch** — official device-code login. Add the account, approve the code at
`twitch.tv/activate`. Your password and cookies never touch GrubDrops.

**Kick** — no public OAuth, so you hand GrubDrops your kick.com session as a
`cookies.txt` export:

1. Install [Get cookies.txt LOCALLY](https://chromewebstore.google.com/detail/get-cookiestxt-locally/cclelndahbckbenkjhflpdbgdldlbecc)
   (Chrome/Edge/Brave) or [cookies.txt](https://addons.mozilla.org/en-US/firefox/addon/cookies-txt/) (Firefox).
2. Sign in at `kick.com`, click the extension icon, **Export** (current site).
3. In GrubDrops: account → **Authorize** → upload or paste the export. Done.

When the cookies go stale (discovery logs cloudflare / 401), re-export and
paste again. Channels auto-discover from each campaign's game — nothing else
to configure.

## What it does

- 🎯 **You set a whitelist** (global or per-account) and nothing outside it ever gets mined.
- 🟣🟢 **Twitch and Kick together**, several accounts each, all on one page.
- ✅ **It checks the game** so you never burn watch-time on the wrong stream.
- 🔗 **It knows about account links** (Krafton, Embark, …) with a per-account "I've linked it" override.
- 🖥️ **A live console**: lifetime stats, current mining, drops catalog, claim history.
- 🔔 **Discord notifications**, toggle per event type.

## How it works

- **Twitch:** device-code login, then GraphQL + PubSub for progress and claims.
- **Kick:** detection and claims ride a Chrome-TLS-fingerprinted HTTP client
  (`utls`) — no Cloudflare dance. Watch-time accrues in an on-demand,
  per-account Chrome sidecar that actually plays the IVS stream; the miner
  starts and stops those containers over the docker socket so Chrome only
  runs while watching.
- **Discovery** sweeps the catalog into SQLite every few minutes.

## Configuration

| Var | Default | Purpose |
|-----|---------|---------|
| `GRUB_MASTER_KEY` | required | Key for the age-encrypted session store. |
| `GRUB_HTTP_ADDR` | `:8080` | Listen address. |
| `GRUB_DB_PATH` | `/data/miner.db` | SQLite path (override to e.g. `./miner.db` outside docker). |
| `GRUB_KICK_BROWSER_WATCH` | `0` | `1` = credit-earning browser watch for Kick (needs sidecar image + socket). |
| `GRUB_KICK_SIDECAR_TEMPLATE` | `grubdrops-browser-{slug}` | Per-account sidecar container name template. |
| `GRUB_KICK_SIDECAR_PORT` | `9090` | Sidecar gRPC port. |
| `GRUB_BROWSER_URL` | none | Fixed sidecar address (legacy always-on mode). |
| `GRUB_BROWSER_URLS` | none | Comma-separated always-on sidecar pool (one Chrome per Kick account). |
| `GRUB_DISCOVERY_INTERVAL` | `60m (DB default)` | Catalog scrape cadence override (e.g. `30m`, `2h`); configurable in Settings UI. |
| `GRUB_AUTHCHECK_INTERVAL` | `12h` | Auth health sweep cadence. |
| `GRUB_DISCORD_WEBHOOK` | none | Optional global Discord webhook. |
| `GRUB_SECURE_COOKIES` | `0` | Secure session cookies (turn on behind HTTPS). |
| `GRUB_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error`. |

### Single sign-on (OIDC)

Optional; password login stays as fallback. Works with any OIDC provider
(authentik, Auth0, Keycloak, Google, Okta, …). SSO turns on when the first
four are set:

| Var | Required | Purpose |
|-----|----------|---------|
| `GRUB_OIDC_ISSUER` | yes | Issuer URL. |
| `GRUB_OIDC_CLIENT_ID` | yes | OAuth client ID. |
| `GRUB_OIDC_CLIENT_SECRET` | yes | OAuth client secret. |
| `GRUB_OIDC_REDIRECT_URL` | yes | `https://<host>/auth/oidc/callback`, registered with the IdP. |
| `GRUB_OIDC_PROVIDER_NAME` | no | Button label (default `SSO`). |
| `GRUB_OIDC_ALLOWED_EMAILS` | no | Comma-separated email allowlist. |
| `GRUB_OIDC_ALLOWED_GROUPS` | no | Required group(s) on the `groups` claim. |

**With no allowlist set, any user the IdP authenticates becomes admin** — scope
membership in the IdP or set an allowlist.

## Pages

| Page | What's on it |
|------|------|
| **Console** (`/`) | Lifetime stats, per-account mining, live event feed. |
| **Drops** (`/drops`) | Past/current/upcoming campaigns, items, connect chips, one-click whitelisting. |
| **History** (`/history`) | Claim log across every account. |
| **Settings** (`/settings`) | Priority list, intervals, Discord, log level, password. |
| **Accounts** | Add accounts, per-account whitelists, re-auth, auth health. |

## Project layout

```
cmd/miner               main daemon
internal/platform/...   per-platform backends (twitch, kick)
internal/watcher        per-account state machine (watch, mine, claim)
internal/dockerctl      on-demand sidecar start/stop over the docker socket
internal/discovery      catalog scraper
internal/api + web      HTMX UI and handlers
internal/store          SQLite (sqlc + goose), age-encrypted sessions
```

## Credits

GrubDrops stands on the shoulders of projects that figured out the hard parts first:

- **[DevilXD/TwitchDropsMiner](https://github.com/DevilXD/TwitchDropsMiner)** — the Twitch device-code flow, GraphQL queries and watch-time mechanics.
- **[HyperBeats/KickDropsMiner](https://github.com/HyperBeats/KickDropsMiner)** — mapped out how Kick drops work in the first place.

GrubDrops is its own Go rewrite with a web UI and multi-account support, but it wouldn't exist without their groundwork. Thank you.

## Notes

Self-hosted, single-tenant, actively developed. `/healthz` for liveness; keep
`/data` across redeploys; put it behind a reverse proxy. Use it responsibly
and within each platform's Terms of Service, against your own accounts, at
your own risk.

---

<sub>Built by <a href="https://github.com/aalejandrofer">@aalejandrofer</a> with <a href="https://claude.com/claude-code">Claude Code</a>. See the <a href="CHANGELOG.md">changelog</a> and <a href="docs/DESIGN.md">design notes</a>.</sub>
