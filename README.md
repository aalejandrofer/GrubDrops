> [!WARNING]
> **Drop discovery is limited.** 
Twitch no longer lists campaigns for new logins, so GrubDrops only mines the games you choose.

> [!CAUTION]
> **GrubDrops is currently not working as expected. Critical Auth Issues**
<p align="center">
  <img src="assets/img/logo.png" width="160" alt="GrubDrops">
</p>

<p align="center"><sub><strong>English</strong> · <a href="docs/translations/README.zh-CN.md">简体中文</a> · <a href="docs/translations/README.es.md">Español</a></sub></p>

<h3 align="center">Self-hosted, set-and-forget Twitch &amp; Kick drops miner.</h3>

<p align="center">
  <img alt="Go" src="https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white">
  <img alt="Twitch" src="https://img.shields.io/badge/Twitch-drops-9146FF?logo=twitch&logoColor=white">
  <img alt="Kick" src="https://img.shields.io/badge/Kick-drops-53FC18?logo=kick&logoColor=black">
  <img alt="UI" src="https://img.shields.io/badge/UI-SvelteKit%20SPA-2c2c2c">
  <img alt="Storage" src="https://img.shields.io/badge/DB-SQLite-003B57?logo=sqlite&logoColor=white">
  <img alt="Self-hosted" src="https://img.shields.io/badge/self--hosted-Docker-2496ED?logo=docker&logoColor=white">
  <img alt="Tested browser" src="https://img.shields.io/badge/tested-Chrome%20149-4285F4?logo=googlechrome&logoColor=white">
  <a href="https://github.com/aalejandrofer/GrubDrops/releases"><img alt="Latest release" src="https://img.shields.io/github/v/release/aalejandrofer/GrubDrops?label=release&color=2c2c2c&logo=github"></a>
  <a href="https://github.com/aalejandrofer/GrubDrops/pkgs/container/grubdrops"><img alt="ghcr.io image" src="https://img.shields.io/badge/ghcr.io-grubdrops-2496ED?logo=github"></a>
  <img alt="License" src="https://img.shields.io/badge/license-MIT-green">
</p>

<p align="center">
  <img src="docs/screenshots/home.png" width="900" alt="GrubDrops Home page: what each account is mining across Twitch and Kick, with the up-next queue and recent claims">
</p>

<p align="center">
  <img src="docs/screenshots/games.png" width="900" alt="GrubDrops Games page: chosen games in a priority list you reorder, with search to add more">
</p>

---

Watches the right Twitch and Kick streams, banks the watch-time, and claims the
drops — across several accounts at once. One small self-hosted web app: a Docker
image and a single SQLite file.

## Features

- 🟣🟢 **Twitch and Kick, side by side** — many accounts, one dashboard.
- 🎯 **Set and forget** — pick your games; it discovers, watches, banks the time, and claims for you.
- 🔒 **Self-hosted and private** — one container, your accounts, your data.
- ⚡ **Reliable on both platforms** — handles the awkward parts (logins, links, Kick playback) so you don't.
- 🛠️ **Built to run unattended** — Discord alerts, update checks, multi-language, proxy, and SSO.

## Getting started

### Prerequisites

**Docker + Docker Compose** (quick path) or **Go 1.26+** (build from source).
What you need depends on which platform you're mining:

| | Twitch | Kick |
|---|---|---|
| **Login** | device-code (`twitch.tv/activate`) | `cookies.txt` export |
| **How it watches** | direct HTTP — no browser | WebSocket; Chrome **sidecar** fallback (IVS playback) |
| **Docker** | optional | **strongly recommended** — WS works without it, but the Chrome IVS fallback needs the docker socket |
| **Run from source, no Docker** | ✅ build the UI, then `go build` (see [Development](#development)) | ⚠️ WS works; the Chrome fallback needs Docker |
| **CPU arch** | any — `amd64` + `arm64` | `amd64` + `arm64` (arm64 is heavy — see note) |

Twitch is direct HTTP — a Go binary mines it anywhere, no Docker. Kick mines browserless over WebSocket by default; the dependable Chrome IVS fallback runs as a sidecar over the docker socket, so **Docker is strongly recommended for Kick**.

> **Raspberry Pi / ARM:** both images ship `arm64`; the sidecar uses Debian Chromium (keeps the H.264/AAC codecs for Kick's IVS stream). Heavy — ~4 GB RAM each.
>
> **Kick watch path:** defaults to *WS, fall back to Chrome* — tries the browserless WebSocket path first (no Docker) and switches to the Chrome sidecar only if WS stops accruing. Keep the docker-socket mount so the fallback works.

### Supported platforms

| Host | Twitch | Kick |
|---|---|---|
| Linux `x86-64` | ✅ | ✅ |
| Linux `arm64` / Raspberry Pi | ✅ | ✅ — Chromium sidecar, ~4 GB RAM each |
| macOS / Windows · Docker Desktop (Intel) | ✅ | ✅ |
| macOS / Windows · Apple Silicon | ✅ | ✅ — arm64 Chromium sidecar |
| From source: UI build, then `go build` (any OS) | ✅ | ✅ WS; Chrome fallback needs Docker |

### Run it

Compose with the published image — just the **miner**. It auto-creates a Chrome
**sidecar** per Kick account on demand over the mounted docker socket; you define
no sidecar services.

```yaml
# compose.yml
services:
  miner:
    image: ghcr.io/aalejandrofer/grubdrops:latest
    restart: unless-stopped
    ports: ["8080:8080"]
    environment:
      GRUB_MASTER_KEY: "${GRUB_MASTER_KEY:?generate one with docker run --rm ghcr.io/aalejandrofer/grubdrops:latest keygen}"
      GRUB_DB_PATH: /data/miner.db
      GRUB_SECURE_COOKIES: "0"   # plain-HTTP localhost; set 1 behind HTTPS
      TZ: Europe/London           # server-side timezone
    volumes:
      - ./data:/data
      - /var/run/docker.sock:/var/run/docker.sock # Kick only, if WS Breaks

  # Optional auto-update: pulls a new image and recreates the miner only.
  # watchtower:
  #   image: containrrr/watchtower:latest
  #   restart: unless-stopped
  #   volumes:
  #     - /var/run/docker.sock:/var/run/docker.sock
  #   command: --interval 21600 --cleanup grubdrops
```

`GRUB_MASTER_KEY` must be an **age X25519 identity** (`AGE-SECRET-KEY-1…`) — it
encrypts the stored session tokens. A random string will not parse and the miner
crashes at startup. Generate a valid one with just Docker:

```bash
docker run --rm ghcr.io/aalejandrofer/grubdrops:latest keygen
# → AGE-SECRET-KEY-1... (keep it; reuse the SAME key on every restart)
```

The image runs as distroless `nonroot` (**UID 65532**), so make a bind-mounted
`./data` writable first — otherwise it can't write `miner.db` and login fails
with *"failed to persist session"*. (Or use a named volume.)

```bash
mkdir -p data && sudo chown 65532:65532 data
GRUB_MASTER_KEY="$(docker run --rm ghcr.io/aalejandrofer/grubdrops:latest keygen)" docker compose up -d
```

Open **http://localhost:8080** and create the admin login.

- **Portainer / GUI deploy?** There's no shell to set the variable, so add
  `GRUB_MASTER_KEY` (value from `keygen` above) in the stack's **Environment
  variables** section before deploying. Use plain `docker compose`, not a Swarm
  stack, on a current Docker Engine.
- **Twitch only?** Drop the docker-socket mount — no sidecars get created.
- **Every knob?** Reference compose: [`deploy/docker-compose.yml`](deploy/docker-compose.yml).
- **Build it?** `docker build -f deploy/Dockerfile.miner .`. From source, build the UI first:
  `npm --prefix web ci && npm --prefix web run build`, then `go build ./cmd/miner`.
  Without the UI build the miner serves a "UI not built" page.

## Adding accounts

Go to **Settings → Accounts** and add one per platform.

**Twitch.** Click add, open the verification URL shown and enter the code.
That's the official device-code flow; your password and cookies never touch
GrubDrops.

**Kick.** Kick has no public login API, so you hand GrubDrops your existing
kick.com session as a `cookies.txt` file exported from your browser:

1. Install a cookie-export extension:
   [Get cookies.txt LOCALLY](https://chromewebstore.google.com/detail/get-cookiestxt-locally/cclelndahbckbenkjhflpdbgdldlbecc)
   for Chrome/Edge/Brave, or
   [cookies.txt](https://addons.mozilla.org/en-US/firefox/addon/cookies-txt/) for Firefox.
2. Sign in at `kick.com`, click the extension icon, **Export** the current site.
3. In GrubDrops, open **Settings → Accounts**, add a Kick account and upload (or paste) the Netscape-format `cookies.txt` export.

Channels auto-discover from each campaign's game, so there's nothing else to
configure. When the session goes stale (discovery logs Cloudflare or 401
errors), re-export and paste again.

## Pick what to mine

GrubDrops mines only the games you choose. A fresh install mines nothing until
you add at least one game, and until then every account shows a *no games*
state, which is not an error.

The chosen games form one global priority list that every account shares. On
**Games**, type a game name to add it; matching games you added before show as
suggestions. A game doesn't need a live campaign to be added. Drag the list to
change what mines first. Each change reloads the runners in the background.

## How it works

- **Twitch:** device-code login, then GraphQL and PubSub to track progress and claim.
- **Kick:** detection and claims use a Chrome-TLS-fingerprinted HTTP client
  (`utls`) with no browser. Watch time needs a real player, so it runs in an
  on-demand Chrome sidecar per account (IVS playback). The miner starts and
  stops these over the docker socket, and a sweep removes the containers of
  deleted accounts.
- **Runner:** each account has one runner. It scans the account's campaigns and
  inventory, asks the planner for a queue, watches the top drop and claims it
  when done. It rebuilds the plan every 15 minutes while it watches.

## Priority logic

The planner orders each account's drops like this:

```
1. Game rank in your chosen-games list, top first
2. Restricted (team) campaigns ahead of open ones
3. Closest to a claim (fewest watch minutes remaining)
4. Within a campaign, the shortest drop first
```

Drops that cannot progress go to a blocked list with a reason: the account is
not linked, an earlier drop must be claimed first, or the runner gave up on it.
A campaign with no live stream is passed over for the next one in the queue.
You can reorder an account's up-next list for the current session.

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
| `GRUB_AUTHCHECK_INTERVAL` | `1h` | Auth-health sweep cadence. |
| `GRUB_DISCORD_WEBHOOK` | none | Global Discord webhook. |
| `GRUB_SECURE_COOKIES` | `0` | `1` marks cookies `Secure` (HTTPS only); keep `0` for plain HTTP — see note. |
| `GRUB_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error`. |
| `GRUB_AUTHBYPASS` | `false` | **Disables all auth** when truthy (`1`/`true`). |
| `GRUB_TWITCH_BROWSER` | `0` | `1` routes Twitch through the browser sidecar instead of direct HTTP. Experimental; the default direct-HTTP path is recommended. |
| `GRUB_CANARY_INTERVAL` | stored value, else `1h` | Overrides the accrual-canary run cadence (e.g. `6h`); falls back to the stored value, else 1 h. |

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

## The web UI

The UI is a SvelteKit static SPA embedded in the binary; the Go side serves a
JSON API under `/api` and live updates as server-sent events on `/api/events`.
The source is in `web/`; `npm --prefix web run build` writes it to
`internal/web/dist`, which Go embeds.

Pages:

| Page | What it shows |
|------|------|
| **Home** | What each account is mining, the up-next queue, recent claims. |
| **Games** | Your chosen games, added by name, in a priority order you drag. |
| **Rewards** | Claimed drops across accounts, with filters. |
| **Settings** | General, Accounts, Notifications, Updates and Health. |

Login and first-run setup are separate pages. Go strings (notifications) use
`i18n.T`; the UI reads the same locale files in `internal/i18n/locales` with `t()`.

## Architecture

```
cmd/miner               main daemon
internal/platform/...   per-platform backends (twitch, kick)
internal/plan           pure planner: picks what each account mines next
internal/runner         per-account runner (watch, mine, claim) and the pool
internal/collect        runner hooks into the store (claims, minutes, scans)
internal/summary        daily watch summary at local midnight
internal/dockerctl      on-demand sidecar start/stop over the docker socket
internal/api            JSON API + SSE for the SPA
internal/web            embedded SPA build (dist/)
internal/store          SQLite (sqlc + goose), age-encrypted sessions
```

## Development

```bash
npm --prefix web ci && npm --prefix web run build   # SPA into internal/web/dist
web/scripts/dev-miner.sh                            # local miner on a temp DB
GRUB_DEV_DIR=<dir> web/scripts/dev-seed.sh          # admin, sample games and accounts
GRUB_DEV_API=http://127.0.0.1:18080 npm --prefix web run dev
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
