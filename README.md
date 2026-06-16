<p align="center">
  <img src="internal/web/static/img/logo.png" width="160" alt="GrubDrops">
</p>

<p align="center"><sub><strong>English</strong> · <a href="docs/translations/README.zh-CN.md">简体中文</a> · <a href="docs/translations/README.es.md">Español</a></sub></p>

<h3 align="center">Self-hosted, set-and-forget Twitch &amp; Kick drops miner.</h3>

<p align="center">
  <img alt="Go" src="https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white">
  <img alt="Twitch" src="https://img.shields.io/badge/Twitch-drops-9146FF?logo=twitch&logoColor=white">
  <img alt="Kick" src="https://img.shields.io/badge/Kick-drops-53FC18?logo=kick&logoColor=black">
  <img alt="UI" src="https://img.shields.io/badge/UI-HTMX%20%2B%20Go%20templates-2c2c2c">
  <img alt="Storage" src="https://img.shields.io/badge/DB-SQLite-003B57?logo=sqlite&logoColor=white">
  <img alt="Self-hosted" src="https://img.shields.io/badge/self--hosted-Docker-2496ED?logo=docker&logoColor=white">
  <a href="https://github.com/Ab-code520/GrubDrops/releases"><img alt="Latest release" src="https://img.shields.io/badge/release-v1.2.3-2c2c2c?logo=github"></a>
  <a href="https://github.com/Ab-code520/GrubDrops/pkgs/container/grubdrops"><img alt="ghcr.io image" src="https://img.shields.io/badge/ghcr.io-grubdrops-2496ED?logo=github"></a>
  <img alt="License" src="https://img.shields.io/badge/license-MIT-green">
</p>

<p align="center">
  <img src="docs/screenshots/console.png" width="900" alt="GrubDrops console: watch-time stats, per-account mining across Twitch and Kick, and a live event feed">
</p>

---

Watches the right Twitch and Kick streams, banks the watch-time, and claims the
drops — across several accounts at once. One small self-hosted web app: a Docker
image and a single SQLite file.

## Features

- 🎯 **You set a whitelist** (global or per-account). Nothing outside it gets mined.
- 🟣🟢 **Twitch and Kick together**, several accounts each, all on one page.
- ✅ **It checks the game** so you never burn watch-time on the wrong stream.
- 🔗 **It knows about account links** (Krafton, Embark, …) with a per-account "I've linked it" override.
- 🖥️ **A live console**: lifetime stats, current mining, drops catalog, claim history.
- 🔔 **Discord notifications**, toggle per event type.
- 🧪 **Browserless Kick by default**: Kick now starts on a WebSocket watch path (no Chrome, no Docker — light enough for any Pi) and **falls back to the Chrome sidecar automatically** if WS stops accruing. Force a specific path in Settings → Experimental.
- 🔒 **Your credentials stay yours**: Twitch uses the official device-code login, Kick uses a session you export. No passwords sent to GrubDrops.
- 🌐 **Multi-language support**: English and Chinese (中文) built-in. Add new languages by copying a JSON file.
- 🕐 **Timezone auto-detection**: Set `TZ` env var for server-side times; browser automatically shows local time.
- 🌍 **Proxy support**: HTTP/HTTPS/SOCKS5 proxy for all external requests. Configure in Settings → Proxy.

## Getting started

### Prerequisites

**Docker + Docker Compose** (quick path) or **Go 1.26+** (build from source).
What you need depends on which platform you're mining:

| | Twitch | Kick |
|---|---|---|
| **Login** | device-code (`twitch.tv/activate`) | `cookies.txt` export |
| **How it watches** | direct HTTP — no browser | Chrome **sidecar** (real IVS playback) |
| **Docker** | optional | **required** — the miner spawns the sidecar over the docker socket |
| **Run from source, no Docker** | ✅ a plain `go build` binary works | ❌ needs Docker for the sidecar |
| **CPU arch** | any — `amd64` + `arm64` | `amd64` + `arm64` (arm64 is heavy — see note) |

Twitch is direct HTTP — a plain Go binary mines it anywhere, no Docker. Kick watch-time needs a real player, so the miner runs a Chrome/Chromium sidecar over the docker socket (**Docker required for Kick**).

> **Raspberry Pi / ARM:** both images ship `arm64`; the sidecar uses Debian Chromium (keeps the H.264/AAC codecs for Kick's IVS stream). Heavy — ~4 GB RAM each.
>
> **Kick watch path:** defaults to *WS, fall back to Chrome* — tries the browserless WebSocket path first (no Docker) and switches to the Chrome sidecar only if WS stops accruing. Keep the docker-socket mount so the fallback works; force *Chrome sidecar* or *WebSocket only* in Settings → Experimental.

### Supported platforms

| Host | Twitch | Kick |
|---|---|---|
| Linux `x86-64` | ✅ | ✅ |
| Linux `arm64` / Raspberry Pi | ✅ | ✅ — Chromium sidecar, ~4 GB RAM each |
| macOS / Windows · Docker Desktop (Intel) | ✅ | ✅ |
| macOS / Windows · Apple Silicon | ✅ | ✅ — arm64 Chromium sidecar |
| `go build` from source (any OS) | ✅ | needs Docker for the sidecar |

### Run it

Compose with the published image — just the **miner**. It auto-creates a Chrome
**sidecar** per Kick account on demand over the mounted docker socket; you define
no sidecar services.

```yaml
# compose.yml
services:
  miner:
    image: ghcr.io/ab-code520/grubdrops:latest
    restart: unless-stopped
    ports: ["8080:8080"]
    environment:
      GRUB_MASTER_KEY: ${GRUB_MASTER_KEY:?run: head -c32 /dev/urandom | base64}
      GRUB_DB_PATH: /data/miner.db
      GRUB_SECURE_COOKIES: "0"   # plain-HTTP localhost; set 1 behind HTTPS
      TZ: Asia/Shanghai           # server-side timezone
    volumes:
      - ./data:/data
      - /var/run/docker.sock:/var/run/docker.sock
```

The image runs as distroless `nonroot` (**UID 65532**), so make a bind-mounted
`./data` writable first — otherwise it can't write `miner.db` and login fails
with *"failed to persist session"*. (Or use a named volume.)

```bash
mkdir -p data && sudo chown 65532:65532 data
GRUB_MASTER_KEY=$(head -c32 /dev/urandom | base64) docker compose up -d
```

Open **http://localhost:8080** and create the admin login.

- **Twitch only?** Drop the docker-socket mount — no sidecars get created.
- **Every knob?** Reference compose: [`deploy/docker-compose.yml`](deploy/docker-compose.yml).
- **Build it?** `docker build -f deploy/Dockerfile.miner .`, or `go build ./cmd/miner`.

## Adding accounts

Go to **Accounts** and add one per platform.

**Twitch.** Click add, then approve the code shown at `twitch.tv/activate`.
That's the official device-code flow; your password and cookies never touch
GrubDrops.

**Kick.** Kick has no public login API, so you hand GrubDrops your existing
kick.com session as a `cookies.txt` file exported from your browser:

1. Install a cookie-export extension:
   [Get cookies.txt LOCALLY](https://chromewebstore.google.com/detail/get-cookiestxt-locally/cclelndahbckbenkjhflpdbgdldlbecc)
   for Chrome/Edge/Brave, or
   [cookies.txt](https://addons.mozilla.org/en-US/firefox/addon/cookies-txt/) for Firefox.
2. Sign in at `kick.com`, click the extension icon, **Export** the current site.
3. In GrubDrops, open the account's **Authorize** page and upload (or paste) the export.

Channels auto-discover from each campaign's game, so there's nothing else to
configure. When the session goes stale (discovery logs Cloudflare or 401
errors), re-export and paste again.

## Pick what to mine

GrubDrops is whitelist-driven: it only discovers and mines games you opt into,
so **a fresh install mines nothing until you whitelist at least one game**. Until
then `/drops` shows a prompt pointing you here, and accounts sit in a *"no games
yet"* state (not an error).

Add games either way — by name, no need to wait for a campaign to appear first:

- **Global** (applies to every account): **Settings → Drop Priority → add by name**.
- **Per account** (overrides the global list): **Accounts → pick an account → add by name**.

Discovery starts crawling that game on the next tick and live campaigns show up
on `/drops`.

## How it works

- **Twitch:** device-code login, then GraphQL + PubSub to track progress and claim.
- **Kick:** detection/claims ride a Chrome-TLS-fingerprinted HTTP client (`utls`) —
  no Cloudflare dance, no browser. Watch-time needs a real player, so it runs in an
  on-demand per-account Chrome sidecar (IVS playback) the miner creates/stops over
  the docker socket; a sweep removes deleted accounts' containers.
- **Discovery** scrapes both catalogs into SQLite every few minutes.

## Priority logic

Each account mines one campaign at a time. When several whitelisted campaigns
are eligible, GrubDrops picks in this order:

```
1. Campaign, by your priority mode (Settings):
   ├─ ordered (default)  → your whitelist rank, top of the list first
   ├─ ending_soonest     → soonest deadline first
   └─ low_avbl_first     → fewest available channels first
2. Tiebreak: closest to a claim (fewest watch-minutes remaining)
3. Restricted (team) campaigns ahead of open ones (both platforms)
4. Channel: a live stream confirmed on the campaign's game,
   highest viewer count first (Twitch also probes for one
   actually serving the target drop)
```

Whitelist and priority are per-account, falling back to the global list. A
campaign with no live stream is skipped, not slept on.

## Configuration

Environment variables; only `GRUB_MASTER_KEY` is **required**, the rest take the
default shown.

| Var | Default | Purpose |
|-----|---------|---------|
| `GRUB_MASTER_KEY` | **required** | Key for the age-encrypted session store. |
| `GRUB_HTTP_ADDR` | `:8080` | Listen address. |
| `GRUB_DB_PATH` | `/data/miner.db` | SQLite path (use e.g. `./miner.db` outside Docker). |
| `GRUB_KICK_SIDECAR_IMAGE` | `ghcr.io/aalejandrofer/grubdrops-browser:latest` | Sidecar image the miner pulls per account. |
| `GRUB_KICK_SIDECAR_NETWORK` | auto-detected | Override the self-detected sidecar network. |
| `GRUB_KICK_SIDECAR_TEMPLATE` | `grubdrops-browser-{slug}` | Sidecar container-name template. |
| `GRUB_KICK_SIDECAR_PORT` | `9090` | Sidecar gRPC port. |
| `GRUB_BROWSER_URL` | none | Fixed sidecar address (legacy always-on). |
| `GRUB_BROWSER_URLS` | none | Always-on sidecar pool, comma-separated. |
| `GRUB_DISCOVERY_INTERVAL` | `60m` | Catalog-scrape cadence; also in Settings. |
| `GRUB_AUTHCHECK_INTERVAL` | `1h` | Auth-health sweep cadence. |
| `GRUB_DISCORD_WEBHOOK` | none | Global Discord webhook. |
| `GRUB_SECURE_COOKIES` | `0` | `1` marks cookies `Secure` (HTTPS only); keep `0` for plain HTTP — see note. |
| `GRUB_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error`. |

> **"Invalid CSRF token"?** `GRUB_SECURE_COOKIES` must match your scheme: `0`
> over plain HTTP, `1` over HTTPS (proxy must forward `X-Forwarded-Proto: https`).
> A mismatch marks cookies `Secure` over HTTP, so they drop and POSTs fail. A
> failed check logs `csrf check failed` with the likely cause.

### Single sign-on (OIDC)

Optional (password login stays as fallback). Any OIDC provider; switches on once
the first four are set:

| Var | Required | Purpose |
|-----|----------|---------|
| `GRUB_OIDC_ISSUER` | yes | Issuer URL. |
| `GRUB_OIDC_CLIENT_ID` | yes | OAuth client ID. |
| `GRUB_OIDC_CLIENT_SECRET` | yes | OAuth client secret. |
| `GRUB_OIDC_REDIRECT_URL` | yes | `https://<host>/auth/oidc/callback`, registered with the IdP. |
| `GRUB_OIDC_PROVIDER_NAME` | no | Button label (default `SSO`). |
| `GRUB_OIDC_ALLOWED_EMAILS` | no | Comma-separated email allowlist. |
| `GRUB_OIDC_ALLOWED_GROUPS` | no | Required group(s) on the `groups` claim. |

> **Heads up:** with no allowlist set, anyone the IdP authenticates becomes an
> admin. Scope membership in the IdP, or set an allowlist.

## The pages

| Page | What's on it |
|------|------|
| **Console** (`/`) | Lifetime stats, per-account mining, live event feed. |
| **Drops** (`/drops`) | Past / current / upcoming campaigns, items, connect chips, one-click whitelisting. |
| **History** (`/history`) | Claim log across every account. |
| **Settings** (`/settings`) | Priority list, intervals, Discord, log level, password. |
| **Accounts** | Add accounts, per-account whitelists, re-auth, auth health. |

## Architecture

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

Stands on the projects that cracked the hard parts first:

- **[DevilXD/TwitchDropsMiner](https://github.com/DevilXD/TwitchDropsMiner)** — Twitch device-code flow, GraphQL, watch-time mechanics.
- **[HyperBeats/KickDropsMiner](https://github.com/HyperBeats/KickDropsMiner)** — mapped how Kick drops work.

GrubDrops is its own Go rewrite (web UI, multi-account) but wouldn't exist without their groundwork.

## License

Released under the [MIT License](LICENSE).

## A note on responsible use

Self-hosted, single-tenant. `/healthz` for liveness; keep `/data` across
redeploys; reverse-proxy it if exposed. Stay within each platform's ToS, on your
own accounts, at your own risk.

---

<sub>Built by <a href="https://github.com/aalejandrofer">@aalejandrofer</a> with <a href="https://claude.com/claude-code">Claude Code</a>. See the <a href="docs/CHANGELOG.md">changelog</a> and <a href="docs/DESIGN.md">design notes</a>.</sub>
