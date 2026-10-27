<p align="center">
  <img src="assets/img/logo.png" width="160" alt="GrubDrops">
</p>

<p align="center"><sub><strong>English</strong> · <a href="docs/translations/README.zh-CN.md">简体中文</a> · <a href="docs/translations/README.es.md">Español</a></sub></p>

<h3 align="center">Self-hosted Twitch and Kick drops miner.</h3>

<p align="center">
  <a href="https://github.com/aalejandrofer/GrubDrops/releases"><img alt="Latest release" src="https://img.shields.io/github/v/release/aalejandrofer/GrubDrops?label=release&color=2c2c2c&logo=github"></a>
  <a href="https://github.com/aalejandrofer/GrubDrops/pkgs/container/grubdrops"><img alt="ghcr.io image" src="https://img.shields.io/badge/ghcr.io-grubdrops-2496ED?logo=github"></a>
  <img alt="Twitch" src="https://img.shields.io/badge/Twitch-drops-9146FF?logo=twitch&logoColor=white">
  <img alt="Kick" src="https://img.shields.io/badge/Kick-drops-53FC18?logo=kick&logoColor=black">
  <img alt="License" src="https://img.shields.io/badge/license-MIT-green">
</p>

<p align="center">
  <img src="docs/screenshots/home.png" width="900" alt="GrubDrops Home: what each account is mining, the per-account Up next queue and recent claims">
</p>

GrubDrops watches Twitch and Kick streams for the games you pick, earns the
watch time and claims the drops, on several accounts at once. It runs as one
Docker container with a web UI and keeps everything in one SQLite file.

## Features

- **Twitch and Kick, many accounts.** Each account mines in parallel and shows on one Home page.
- **Discover.** Lists every current campaign on both platforms. Add a game to your list in one click.
- **Priority you control.** Order your games on Games. Each account has its own Up next queue you can reorder, plus a Mine next button.
- **Auto-claim.** Drops are claimed when done. If a drop needs a linked publisher account, link it and press Check now.
- **Alerts and sign-in.** Discord and Telegram notifications, optional OIDC single sign-on, and a first-run wizard.

## Quick start

```yaml
# compose.yml
services:
  miner:
    image: ghcr.io/aalejandrofer/grubdrops:latest
    restart: unless-stopped
    ports: ["8080:8080"]
    environment:
      GRUB_SECURE_COOKIES: "0"   # 1 behind HTTPS
      TZ: Europe/London
    volumes:
      - ./data:/data
      - /var/run/docker.sock:/var/run/docker.sock   # Kick only
```

```bash
mkdir -p data && sudo chown 65532:65532 data
docker compose up -d
```

Open http://localhost:8080 and create the admin login. A wizard then walks you
through adding an account and picking games.

- On first start the miner creates `data/master.key` and reuses it after that. It encrypts the stored sessions, so keep it with `miner.db`. To keep the key outside `./data`, set `GRUB_MASTER_KEY` to the output of `docker run --rm ghcr.io/aalejandrofer/grubdrops:latest keygen`; it wins over the file.
- The container runs as UID 65532, so `./data` must be writable by that user.
- `GRUB_SECURE_COOKIES` must match how you reach the UI: `0` for plain HTTP, `1` for HTTPS. A mismatch causes "Invalid CSRF token".
- The docker socket is only needed for Kick. The miner uses it to start a Chrome sidecar per Kick account. Drop the mount if you only mine Twitch.

Images are built for `amd64` and `arm64`. On arm64 each Kick sidecar needs about 4 GB of RAM.

## Adding accounts

Open **Settings > Accounts**, or use the first-run wizard.

**Twitch, Android login (recommended).** Sign in to Twitch in the tab GrubDrops
opens. You land on a URL like `https://www.twitch.tv/#access_token=XXX&scope=&token_type=bearer`.
Copy the whole URL and paste it into GrubDrops. Discover needs an Android login to list Twitch campaigns.

**Twitch, TV code.** Open the link shown and enter the code.

**Kick.** Export your kick.com cookies in Netscape format with
[Get cookies.txt LOCALLY](https://chromewebstore.google.com/detail/get-cookiestxt-locally/cclelndahbckbenkjhflpdbgdldlbecc)
(Chrome, Edge, Brave) or
[cookies.txt](https://addons.mozilla.org/en-US/firefox/addon/cookies-txt/)
(Firefox), then upload or paste the file. When the session expires, export and
paste again.

## Configuration

No variable is required. Every variable is listed in
[`deploy/docker-compose.yml`](deploy/docker-compose.yml).

| Var | Default | Purpose |
|-----|---------|---------|
| `GRUB_MASTER_KEY` | `master.key` next to the DB | age key that encrypts stored sessions; generated on first start when unset |
| `GRUB_HTTP_ADDR` | `0.0.0.0:8080` | Listen address |
| `GRUB_DB_PATH` | `/data/miner.db` | SQLite file |
| `GRUB_SECURE_COOKIES` | `0` | `1` when served over HTTPS |
| `GRUB_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `GRUB_DISCORD_WEBHOOK` | none | Discord webhook, used when none is saved in Settings |
| `GRUB_TELEGRAM_BOT_TOKEN` | none | Telegram bot token, used when none is saved in Settings |
| `GRUB_TELEGRAM_CHAT_ID` | none | Telegram chat ID, used when none is saved in Settings |
| `GRUB_UPDATE_CHECK` | on | `0` turns off the GitHub release check |
| `GRUB_UPDATE_INTERVAL` | `1h` | How often the release check runs |
| `GRUB_AUTHCHECK_INTERVAL` | `1h` | How often every account's login is checked |
| `GRUB_CANARY_INTERVAL` | Settings value | Overrides the accrual canary interval set in Settings |
| `GRUB_LOG_RING` | `1000` | Recent log lines kept in memory |
| `GRUB_KICK_SIDECAR_IMAGE` | `ghcr.io/aalejandrofer/grubdrops-browser:latest` | Kick sidecar image |
| `GRUB_KICK_SIDECAR_NETWORK` | auto-detected | Docker network for Kick sidecars |
| `GRUB_KICK_SIDECAR_TEMPLATE` | `grubdrops-browser-{slug}` | Kick sidecar container name; `{slug}` is the Kick username |
| `GRUB_KICK_SIDECAR_PORT` | `9090` | gRPC port inside each Kick sidecar |
| `GRUB_BROWSER_URL` | none | Always-on browser sidecar (`host:port`) for login and display |
| `GRUB_BROWSER_URLS` | none | Comma-separated sidecar pool; Kick accounts are spread across it |
| `GRUB_TWITCH_BROWSER` | off | `1` mines Twitch through the browser sidecar instead of direct HTTP. Needs `GRUB_BROWSER_URL` |
| `GRUB_AUTH_BYPASS_LOCAL` | off | `1` skips login for requests from loopback |
| `GRUB_AUTHBYPASS` | off | `1` turns off all login. Use only on a network you trust |

Durations use Go syntax: `90s`, `30m`, `6h`.

### Single sign-on (OIDC)

SSO turns on when the first four variables are set. Password login still works.

| Var | Required | Purpose |
|-----|----------|---------|
| `GRUB_OIDC_ISSUER` | yes | Issuer URL |
| `GRUB_OIDC_CLIENT_ID` | yes | Client ID |
| `GRUB_OIDC_CLIENT_SECRET` | yes | Client secret |
| `GRUB_OIDC_REDIRECT_URL` | yes | `https://<host>/auth/oidc/callback` |
| `GRUB_OIDC_PROVIDER_NAME` | no | Button label, default `SSO` |
| `GRUB_OIDC_ALLOWED_EMAILS` | no | Comma-separated email allowlist |
| `GRUB_OIDC_ALLOWED_GROUPS` | no | Required groups on the `groups` claim |

> [!WARNING]
> With no allowlist, anyone your identity provider signs in becomes an admin.

## How it works

- **Twitch** runs over direct HTTP with no browser: GraphQL for campaigns and claims, a watch beacon every minute.
- **Kick** finds and claims drops over HTTP. Watch time goes over WebSocket first and falls back to the Chrome sidecar if that stops.
- **Each account** has one runner. It mines the highest-priority drop it can make progress on, claims it, then moves to the next.

## Screenshots

<p align="center">
  <img src="docs/screenshots/discover.png" width="32%" alt="Discover: current Twitch and Kick campaigns grouped by game">
  <img src="docs/screenshots/games.png" width="32%" alt="Games: chosen games in priority order, with a Lists rail for per-account game lists">
  <img src="docs/screenshots/rewards.png" width="32%" alt="Inventory: drops in progress, Up next and claim history">
</p>

## Development

```bash
npm --prefix web ci && npm --prefix web run build   # SPA into internal/web/dist
web/scripts/dev-miner.sh                            # miner on a temp DB, prints GRUB_DEV_DIR
GRUB_DEV_DIR=<dir> web/scripts/dev-seed.sh          # admin, sample games and accounts
GRUB_DEV_API=http://127.0.0.1:18080 npm --prefix web run dev
```

## Credits, license and use

Built on what [DevilXD/TwitchDropsMiner](https://github.com/DevilXD/TwitchDropsMiner) and [HyperBeats/KickDropsMiner](https://github.com/HyperBeats/KickDropsMiner) worked out about Twitch and Kick drops.
Released under the [MIT License](LICENSE). Use it on your own accounts and within each platform's terms, at your own risk.

<sub>See the <a href="docs/CHANGELOG.md">changelog</a> and <a href="docs/DESIGN.md">design notes</a>.</sub>
