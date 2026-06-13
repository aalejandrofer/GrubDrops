<p align="center">
  <img src="internal/web/static/img/logo.png" width="160" alt="GrubDrops">
</p>

<h3 align="center">Self-hosted, set-and-forget Twitch &amp; Kick drops miner.</h3>

<p align="center">
  <img alt="Go" src="https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white">
  <img alt="Twitch" src="https://img.shields.io/badge/Twitch-drops-9146FF?logo=twitch&logoColor=white">
  <img alt="Kick" src="https://img.shields.io/badge/Kick-drops-53FC18?logo=kick&logoColor=black">
  <img alt="UI" src="https://img.shields.io/badge/UI-HTMX%20%2B%20Go%20templates-2c2c2c">
  <img alt="Storage" src="https://img.shields.io/badge/DB-SQLite-003B57?logo=sqlite&logoColor=white">
  <img alt="Self-hosted" src="https://img.shields.io/badge/self--hosted-Docker-2496ED?logo=docker&logoColor=white">
  <a href="https://github.com/aalejandrofer/grubdrops/releases"><img alt="Latest release" src="https://img.shields.io/github/v/release/aalejandrofer/grubdrops?logo=github&label=release"></a>
  <a href="https://github.com/aalejandrofer/grubdrops/pkgs/container/grubdrops"><img alt="ghcr.io image" src="https://img.shields.io/badge/ghcr.io-grubdrops-2496ED?logo=github"></a>
  <img alt="License" src="https://img.shields.io/badge/license-MIT-green">
</p>

<p align="center">
  <img src="docs/screenshots/console.png" width="900" alt="GrubDrops console: watch-time stats, per-account mining across Twitch and Kick, and a live event feed">
</p>

**English** · [简体中文](README.zh-CN.md) · [Español](README.es.md)

---

GrubDrops watches the right Twitch and Kick streams for you, banks the
watch-time, and claims the in-game drops, across several accounts at once. One
small web app on your own box: ships as a Docker image, keeps everything in a
single SQLite file.

## Features

- 🎯 **You set a whitelist** (global or per-account). Nothing outside it gets mined.
- 🟣🟢 **Twitch and Kick together**, several accounts each, all on one page.
- ✅ **It checks the game** so you never burn watch-time on the wrong stream.
- 🔗 **It knows about account links** (Krafton, Embark, …) with a per-account "I've linked it" override.
- 🖥️ **A live console**: lifetime stats, current mining, drops catalog, claim history.
- 🔔 **Discord notifications**, toggle per event type.
- 🔒 **Your credentials stay yours**: Twitch uses the official device-code login, Kick uses a session you export. No passwords sent to GrubDrops.

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

Twitch runs over direct HTTP, so a plain Go binary mines it anywhere — Raspberry Pi included, no Docker. **Kick watch-time needs a real player**, so the miner runs a Chrome/Chromium sidecar over the docker socket — which makes **Docker required for Kick**.

> **Raspberry Pi / ARM:** both the miner *and* the Kick sidecar ship for
> `linux/amd64` **and** `linux/arm64`. The sidecar picks its browser per arch:
> amd64 uses Google Chrome, arm64 uses Debian's **Chromium** — which is built
> against system FFmpeg and so still carries the proprietary **H.264/AAC
> codecs** that decode Kick's IVS stream. Kick-on-ARM is **confirmed working**,
> but the arm64 sidecar is **resource heavy** (~4 GB RAM per live sidecar,
> roughly 2 concurrent on a small box), so a low-RAM Pi will struggle to watch
> more than a couple of Kick accounts at once.

### Supported platforms

| Host | Twitch | Kick |
|---|---|---|
| Linux `x86-64` | ✅ | ✅ |
| Linux `arm64` / Raspberry Pi | ✅ | ✅ — Chromium sidecar, ~4 GB RAM each |
| macOS / Windows · Docker Desktop (Intel) | ✅ | ✅ |
| macOS / Windows · Apple Silicon | ✅ | ✅ — arm64 Chromium sidecar |
| `go build` from source (any OS) | ✅ | needs Docker for the sidecar |

### Run it

Docker Compose with the published images is the fastest path — just the
**miner**. For Kick watch-time it auto-creates a codec-enabled Chrome
**sidecar** per account on demand (over the mounted docker socket), so you
don't define any sidecar services yourself. (Twitch-only? See below.)

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
      GRUB_SECURE_COOKIES: "0"   # plain-HTTP localhost; set 1 behind HTTPS
    volumes:
      # The container runs as nonroot (UID 65532); make ./data writable by it
      # first (see below) or use a named volume instead of a bind mount.
      - ./data:/data
      # lets the miner create/start/stop per-account browser sidecars on demand
      - /var/run/docker.sock:/var/run/docker.sock
```

**Make the data dir writable first.** The miner image runs as the distroless
`nonroot` user (**UID 65532**). A fresh bind-mounted `./data` is owned by your
host user, so the container can't write `miner.db` — sessions never persist and
login fails with *"failed to persist session"* right after a successful verify.
Give the dir to the container user before bringing it up:

```bash
mkdir -p data && sudo chown 65532:65532 data
```

(Or skip the bind mount entirely and use a named Docker volume — Docker creates
those already writable by the container.)

Bring it up. `GRUB_MASTER_KEY` encrypts the stored sessions, so generate a real one:

```bash
GRUB_MASTER_KEY=$(head -c32 /dev/urandom | base64) docker compose up -d
```

Open **http://localhost:8080**. The first visit asks you to create an admin login.

**Twitch only?** Drop the docker-socket mount and leave `GRUB_BROWSER_URL`
unset — no Kick sidecars are ever created (Kick simply has no accruing watch
path without one).

**Want every knob?** The full reference compose (sidecar profiles, OIDC, each
setting commented) lives in
[`deploy/docker-compose.yml`](deploy/docker-compose.yml).

**Build it yourself?** `docker build -f deploy/Dockerfile.miner .`, or plain
`go build ./cmd/miner` for a local binary.

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

## How it works

- **Twitch:** device-code login, then GraphQL and PubSub to track progress and
  fire claims in real time.
- **Kick:** detection and claims ride a Chrome-TLS-fingerprinted HTTP client
  (`utls`), so there's no Cloudflare dance and no browser to babysit. The
  watch-time itself needs a real player, so it runs in an on-demand, per-account
  Chrome sidecar that plays the IVS stream. The miner auto-creates, starts, and
  stops that container over the docker socket (pulling the sidecar image as
  needed), so Chrome only runs while watching and you define no sidecar services.
  A periodic sweep removes the containers of deleted accounts.
- **Discovery** sweeps both catalogs into SQLite every few minutes so the
  dashboard always reflects what's live.

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

All settings are environment variables. `GRUB_MASTER_KEY` is the only **required**
one. Every other variable below is **optional**: leave it unset to take the
default shown.

| Var | Default | Purpose |
|-----|---------|---------|
| `GRUB_MASTER_KEY` | **required** | Key for the age-encrypted session store. |
| `GRUB_HTTP_ADDR` | `:8080` | Listen address. |
| `GRUB_DB_PATH` | `/data/miner.db` | SQLite path (use e.g. `./miner.db` outside Docker). |
| `GRUB_KICK_SIDECAR_IMAGE` | `ghcr.io/aalejandrofer/grubdrops-browser:latest` | Image the miner pulls + runs for each auto-created sidecar. Kick watch always runs through a sidecar (the only accruing path); none configured = Kick can't watch. |
| `GRUB_KICK_SIDECAR_NETWORK` | auto-detected | Docker network to attach sidecars to. Defaults to the miner's own network (self-detected); set to override. |
| `GRUB_KICK_SIDECAR_TEMPLATE` | `grubdrops-browser-{slug}` | Per-account sidecar container-name template. |
| `GRUB_KICK_SIDECAR_PORT` | `9090` | Sidecar gRPC port. |
| `GRUB_BROWSER_URL` | none | Fixed sidecar address (legacy always-on mode). |
| `GRUB_BROWSER_URLS` | none | Comma-separated always-on sidecar pool (one Chrome per Kick account). |
| `GRUB_DISCOVERY_INTERVAL` | `60m` | Catalog-scrape cadence (e.g. `30m`, `2h`); also editable in Settings. |
| `GRUB_AUTHCHECK_INTERVAL` | `12h` | Auth-health sweep cadence. |
| `GRUB_DISCORD_WEBHOOK` | none | Optional global Discord webhook. |
| `GRUB_SECURE_COOKIES` | `0` | Secure session cookies + CSRF same-origin scheme. Leave `0` for plain HTTP (`http://pi:8080`); set `1` only when reached over HTTPS (directly or behind a TLS-terminating proxy that sets `X-Forwarded-Proto: https`). See note below. |
| `GRUB_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error`. |

> **Self-host / "invalid CSRF token":** `GRUB_SECURE_COOKIES` must match how you
> reach the app. Over **plain HTTP** (the default, e.g. a Raspberry Pi at
> `http://pi:8080`) keep it `0` — `1` makes the browser mark the session/CSRF
> cookie `Secure`, so it is silently dropped over HTTP and every form POST then
> fails the CSRF check. Behind a **reverse proxy that terminates TLS**, set `1`
> and have the proxy forward `X-Forwarded-Proto: https`. A failed check now logs
> a `csrf check failed` line and returns a hint pointing at the likely
> mismatch.

### Single sign-on (OIDC)

Optional; password login stays as a fallback. Works with any OIDC provider
(authentik, Auth0, Keycloak, Google, Okta, …). SSO switches on once the first
four variables are set:

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

GrubDrops stands on the shoulders of the projects that figured out the hard
parts first:

- **[DevilXD/TwitchDropsMiner](https://github.com/DevilXD/TwitchDropsMiner)**:
  the Twitch device-code flow, GraphQL queries, and watch-time mechanics.
- **[HyperBeats/KickDropsMiner](https://github.com/HyperBeats/KickDropsMiner)**:
  mapped out how Kick drops work in the first place.

GrubDrops is its own Go rewrite with a web UI and multi-account support, but it
wouldn't exist without their groundwork. Thank you.

## License

Released under the [MIT License](LICENSE).

## A note on responsible use

Self-hosted, single-tenant, actively developed. `/healthz` answers liveness
checks; keep `/data` across redeploys; put it behind a reverse proxy if you
expose it. Use it within each platform's Terms of Service, against your own
accounts, at your own risk.

---

<sub>Built by <a href="https://github.com/aalejandrofer">@aalejandrofer</a> with <a href="https://claude.com/claude-code">Claude Code</a>. See the <a href="docs/CHANGELOG.md">changelog</a> and <a href="docs/DESIGN.md">design notes</a>.</sub>
