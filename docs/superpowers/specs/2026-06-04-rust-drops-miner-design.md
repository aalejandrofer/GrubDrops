# Rust Drops Miner — Design

**Status:** Approved 2026-06-04
**Owner:** aalejandrofer
**Inspirations:** [TwitchDropsMiner](https://github.com/DevilXD/TwitchDropsMiner), [KickDropsMiner](https://github.com/HyperBeats/KickDropsMiner)

## Goal

Headless, Docker-deployable drops miner for the game Rust (with general game support), targeting **Twitch** and **Kick** simultaneously. Multi-account. Web GUI for control and observability. Discord notifications. Self-hosted, single-user.

## Non-Goals

- Public/multi-tenant SaaS. Single-user self-hosted only.
- Mobile or desktop GUI. Web only.
- Mining of arbitrary streaming platforms beyond Twitch and Kick (architecture pluggable, but no other backend in v1).
- Watching the actual video stream. Only the minimal heartbeat/presence signals required for drop progress.

## Scope Decisions

| Decision | Choice |
|---|---|
| Language | Go |
| Primary game | Rust (configurable; any game supported) |
| Platforms | Twitch + Kick, pluggable `Backend` interface, both built v1 |
| Frontend | Server-rendered HTML + HTMX, single binary embed |
| GUI auth | Single admin password + session cookie |
| Auth flow | Hybrid: device code where supported (Twitch), browser sidecar fallback (Kick) |
| Multi-account | Yes; per-account fingerprint; optional per-account HTTP/SOCKS proxy |
| Campaign selection | Auto-discover + user-priority list (Rust ranked top by default) |
| Storage | SQLite (WAL); `sessions.ciphertext` age-encrypted with `MINER_MASTER_KEY` |
| Discord notify | Progress milestones (25/50/75/100) + claims + errors; per-account or global webhook |
| Browser | Sidecar container (`browser-sidecar`), lazy-started via compose profile |
| Architecture | Single Go binary; goroutine-per-account scheduler |

## Architecture

### Repo layout

```
rust-drops-miner/
├── cmd/
│   ├── miner/                # daemon entrypoint
│   └── browser-sidecar/      # rod/chromedp login service
├── internal/
│   ├── api/                  # HTMX handlers, session middleware, CSRF
│   ├── web/                  # templates (embed.FS), static assets
│   ├── scheduler/            # per-account goroutine supervisor
│   ├── watcher/              # state machine: pick → watch → claim
│   ├── platform/
│   │   ├── platform.go       # Backend interface + registry
│   │   ├── twitch/           # GraphQL + minute-watch impl
│   │   └── kick/             # REST + Pusher WS impl
│   ├── auth/
│   │   ├── devicecode.go     # OAuth device-code (Twitch)
│   │   └── browser/          # gRPC client to sidecar
│   ├── notify/               # Discord webhook fan-out (rate-limited)
│   ├── store/                # sqlite, sqlc queries, age-encrypted blob
│   ├── config/               # env + first-run wizard state
│   └── log/                  # structured logging, ring buffer for GUI tail
├── deploy/
│   ├── Dockerfile.miner
│   ├── Dockerfile.browser
│   └── docker-compose.yml
├── migrations/               # goose
└── docs/
    └── superpowers/specs/    # this file
```

### Process model

Inside `miner` binary:

- **HTTP server** goroutine — HTMX endpoints + `/ws` WebSocket
- **Scheduler** — one goroutine per enabled account, owning a `watcher` state machine
- **Discovery ticker** — every 15 min: refresh `campaigns` table per platform
- **Notifier** — channel-fed, single goroutine, token-bucket rate-limited
- **Log buffer** — bounded ring, drained over WS to GUI `/logs`

Shutdown: `SIGTERM` → root context cancel → each goroutine drains & exits.

### Account state machine (`watcher`)

```
              ┌──────────────┐
   start ───► │ Idle          │ ◄───────────┐
              └──────┬───────┘               │
                     │ tick                  │
                     ▼                       │
              ┌──────────────┐               │
              │ PickCampaign │               │
              └──────┬───────┘               │
       no eligible  │  eligible             │
              ┌─────┴────────┐               │
              ▼              ▼               │
        ┌─────────┐   ┌──────────────┐      │
        │ Sleeping│   │ PickStream    │      │
        └────┬────┘   └──────┬───────┘      │
             │ tick   no live│ live          │
             └───────►       ▼               │
                      ┌──────────────┐      │
                      │ Watching      │      │
                      └──────┬───────┘      │
                  progress  │ done           │
                             ▼               │
                      ┌──────────────┐      │
                      │ Claiming      │      │
                      └──────┬───────┘      │
                             └───────────────┘

   On any auth failure: → AuthRequired (terminal until user re-logs in)
   On 5x consecutive transient errors: → Paused (terminal until user resumes)
```

- **PickCampaign** — join `campaigns` × `progress` × `campaign_priorities`; pick highest-ranked unclaimed, in-window, account-eligible campaign.
- **PickStream** — `Backend.ListEligibleChannels`. Stickiness: keep current stream ≥30min unless it goes offline or off-campaign.
- **Watching** — `Backend.Heartbeat` per minute. Update `progress.minutes_watched` from server-truth when API returns it; fall back to local clock.
- **Claiming** — `Backend.Claim`. On success: write `claims` row, fire Discord claim notify, re-enter PickCampaign.

Backoff: per-account exponential on transient errors (jittered, capped 5min).

## Platform abstraction

```go
type Backend interface {
    Name() string  // "twitch" | "kick"

    StartDeviceLogin(ctx) (DeviceChallenge, error)
    PollDeviceLogin(ctx, DeviceChallenge) (Session, error)
    LoginViaBrowser(ctx, BrowserRPC) (Session, error)
    RefreshSession(ctx, Session) (Session, error)

    ListActiveCampaigns(ctx, Session) ([]Campaign, error)
    ListEligibleChannels(ctx, Session, Campaign) ([]Stream, error)
    InventoryProgress(ctx, Session) ([]Progress, error)

    StartWatch(ctx, Session, Stream) (WatchHandle, error)
    Heartbeat(ctx, WatchHandle) error
    StopWatch(ctx, WatchHandle) error
    Claim(ctx, Session, DropBenefit) error
}
```

### Twitch impl

- Endpoint: `gql.twitch.tv/gql` with web `Client-ID`
- Auth: device-code flow (`id.twitch.tv/oauth2/device`)
- Discovery: `DropsPage_ContentList` + per-campaign `DropCampaignDetails`
- Watch: `PlaybackAccessToken` + `MinuteWatched` mutation (no video pull)
- Claim: `DropsPage_ClaimDropRewards`

### Kick impl

- REST: `kick.com/api/v2/*`
- WS: Pusher cluster for presence (chatroom subscribe)
- Auth: no public OAuth → browser sidecar returns cookies + XSRF
- Discovery: campaigns endpoint (per KickDropsMiner reference)
- Watch: WS presence + per-minute REST viewer-heartbeat
- Claim: REST per reward

### Sessions

`sessions(account_id, ciphertext, expires_at)`. `ciphertext` = age-encrypted JSON `{access_token, refresh_token, cookies, csrf, expires_at, fingerprint}`. Key from `MINER_MASTER_KEY` env. Decrypted in memory only.

### Fingerprint

Per account, on create: stable `{user_agent, accept_lang, device_id, viewport}` generated and persisted. Reused on every request.

### Proxy

If `account.proxy_url` set → dedicated `http.Transport` + WS dialer scoped to that account.

## Browser sidecar

Separate container, `cmd/browser-sidecar`. Headless Chromium via `rod`. gRPC over compose-internal network.

RPCs:

- `LoginInteractive(platform) → stream<{status, screenshot_png?, url?, code?, cookies?, error?}>` — daemon proxies the page to the GUI; user clicks/types via overlay forwarded back to sidecar.
- `SolveCaptcha(platform, page_url) → cookies` — for mid-session challenges.

Fallback if proxy is too lossy: show "open URL on phone, complete login, paste session cookie" form in GUI.

Started only when an account login requires it. Docker compose profile `browser` keeps the image absent by default.

## Web GUI

Server-rendered Go templates + HTMX. Single binary embeds templates and static assets.

**Pages**

| Path | Purpose |
|---|---|
| `/setup` | First-run admin password creation |
| `/login` | Admin login |
| `/` | Dashboard — per-account cards, live progress (HTMX poll 2s) |
| `/accounts` | List + add |
| `/accounts/new` | Pick platform → trigger auth |
| `/accounts/:id` | Edit, pause/resume, set proxy, set webhook |
| `/accounts/:id/login` | Live auth flow (device code QR or browser-proxy view) |
| `/campaigns` | Discovered list, priority drag-reorder, enable/disable |
| `/drops` | Claim history, filter by account/campaign/game |
| `/settings` | Master key status, global webhook, log retention, browser-sidecar URL |
| `/logs` | Live tail via SSE |

`/ws` — single connection per tab. Event types: `watcher_state`, `progress_tick`, `claim`, `auth_event`, `log_line`.

**Security**

- First request with no admin → forced `/setup`
- Session cookie: httpOnly, SameSite=Strict, signed with key in sqlite `kv`
- CSRF token on mutating endpoints
- Master key required for daemon start; refuses boot if absent

## Storage (SQLite)

Tables:

```
accounts(id, platform, login, display_name, status, proxy_url, webhook_url,
         fingerprint_json, enabled, created_at, updated_at)

sessions(account_id PK, ciphertext BLOB, expires_at)

campaigns(id, platform, game, name, starts_at, ends_at, status,
          raw_json, discovered_at)

campaign_priorities(account_id NULL, campaign_id, rank)   -- NULL = global default

benefits(id, campaign_id, name, required_minutes, image_url)

progress(account_id, benefit_id, minutes_watched, claimed_at, updated_at)

claims(id, account_id, benefit_id, claimed_at, value_meta_json)

games(id, name, slug, priority)                            -- Rust pinned 0

notifications(id, account_id, kind, payload_json, status, created_at, sent_at)

logs(id, ts, level, account_id, msg, fields_json)          -- ring, trimmed

kv(key PK, value)                                           -- session signing key, etc.

admin(id=1, password_hash, created_at)
```

Migrations: `goose`. Mode: WAL. Path: `/data/miner.db`. Queries: `sqlc`-generated.

## Notifications

- Interface `Notifier`; v1 impl `DiscordWebhook`
- Routing: `account.webhook_url` → fallback `settings.global_webhook` → drop
- Verbosity per account: `silent | claims_errors | progress_claims_errors (default) | verbose`
- Progress milestones: 25/50/75/100 per benefit, deduped via `(account_id, benefit_id, bucket)`
- Rate limit: token-bucket respecting Discord (5 req / 2s), single delivery goroutine
- Retry: persisted in `notifications`, exponential backoff, dropped after 24h
- Embeds: drop image, campaign name, account, progress bar, color (green claim / yellow progress / red error)

## Deployment

Two deployment shapes, same image:

### Local development

`deploy/docker-compose.yml` (in this repo):

```yaml
services:
  miner:
    image: rust-drops-miner:dev
    build:
      context: ..
      dockerfile: deploy/Dockerfile.miner
    restart: unless-stopped
    ports: ["8080:8080"]
    environment:
      MINER_MASTER_KEY: ${MINER_MASTER_KEY:?MINER_MASTER_KEY required}
      MINER_BROWSER_URL: http://browser:9090
    volumes: ["./data:/data"]

  browser:
    image: rust-drops-miner-browser:dev
    build:
      context: ..
      dockerfile: deploy/Dockerfile.browser
    restart: unless-stopped
    profiles: ["browser"]
```

- `miner` base: `gcr.io/distroless/static-debian12`, target ~30MB
- `browser` base: `chromedp/headless-shell` (or rod equivalent), ~600MB, only fetched when `--profile browser` used
- Default `docker compose up` brings only miner

### Homelab production (10.10.2.40 → rdrops.ryuzec.dev)

Target lives in a sibling repo: `/Users/jandro/Library/CloudStorage/OneDrive-Personal/00ALX/02Projects/homelab/humblewhale/rust-drops-miner/compose.yml`. Inherits the homelab conventions:

- Docker Compose stack under `humblewhale/<service>/compose.yml`
- Image pulled (built and pushed separately, not built on host)
- Joins external network `traeky_proxynet`; Traefik routes `rdrops.ryuzec.dev` → port 8080
- TLS via Cloudflare DNS-01 challenge handled by Traefik
- Blocky DNS already maps `*.ryuzec.dev` → 10.10.2.40 by default
- `.env` next to compose file (plain, gitignored); references `${DOMAIN}` and `${PROXY_NETWORK}` from shared `humblewhale/.env`
- Bind-mount `/home/jandro/localConfig/rust-drops-miner/data` → `/data` for sqlite persistence
- Deployment via `update/` TUI (`homelab-update`) which SSHes to the server and runs `docker compose pull && docker compose up -d`

Sketch:

```yaml
services:
  rust-drops-miner:
    image: ghcr.io/aalejandrofer/rust-drops-miner:latest
    container_name: rust-drops-miner
    restart: unless-stopped
    env_file: ./.env
    volumes:
      - /home/jandro/localConfig/rust-drops-miner/data:/data
    networks: [traeky_proxynet]
    labels:
      - "traefik.enable=true"
      - "traefik.http.routers.rust-drops-miner.rule=Host(`rdrops.ryuzec.dev`)"
      - "traefik.http.routers.rust-drops-miner.entrypoints=websecure"
      - "traefik.http.routers.rust-drops-miner.tls.certresolver=cloudflare"
      - "traefik.http.services.rust-drops-miner.loadbalancer.server.port=8080"

networks:
  traeky_proxynet:
    external: true
```

Production deploy is covered by a dedicated plan (Plan 5: Production deploy) after the application work lands. The local compose lives inside this repo and never gets pushed to the homelab.

## Errors & resilience

- All goroutines supervised: `recover` → log → backoff → restart
- Per-account circuit breaker: 5 consecutive same-kind failures → `AuthRequired` or `Paused`, error notify, GUI flag
- Network errors: jittered exp retry, max 3, then escalate
- DB write errors: fail loud (sqlite "locked" almost always indicates a bug)
- Heartbeat schedule: server-returned `expected_next` preferred over local clock

## Testing

- **Unit** — pure logic (scheduler picking, priority calc, state transitions). Table-driven.
- **Backend contract** — golden-file replay of recorded real GraphQL/REST responses. Each `Backend` impl runs against the fixture HTTP server.
- **Integration** — sqlite in tmp dir, fake `Backend`, run full watcher loop end-to-end. Assert claim row + queued notification.
- **Browser sidecar smoke** — stub login page in container; CI-skippable.
- **E2E** — compose up in CI, drive GUI with `chromedp`, walk setup + add-fake-account.

## Open questions (deferred to plan)

- Exact GraphQL operations & persisted-query hashes for Twitch (lift from TwitchDropsMiner, track upstream)
- Kick anti-bot specifics (Cloudflare interstitial handling in sidecar)
- Whether to ship a Helm chart / k8s manifest in v1 (probably no)
- Backup/restore command surface
