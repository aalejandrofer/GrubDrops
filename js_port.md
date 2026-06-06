# Frontend Port — Go templates → JS (TanStack)

Handoff notes so this can be picked up in a fresh session. **Backend is NOT
changing** — only the presentation layer. The Go server keeps mining, scheduling,
the sidecar, DB, and gains a JSON API the JS app consumes.

## Decisions made
- **Stack: TanStack** (user's choice). Suggested concretely:
  - **TanStack Start** (React full-stack) OR plain **Vite + React** if Start is
    overkill. App is a single-operator dashboard, no SSR/SEO needs — Vite+React is
    lighter; TanStack Start fine if you want the batteries.
  - **TanStack Router** — typed routes (`/`, `/drops`, `/history`, `/accounts`, `/settings`).
  - **TanStack Query** — all server state (polling the API; staleTime ~2–5s for live panels).
  - **TanStack Table** — every table (drops, history, discoverable, per-drop items).
    The whole app is tables — this is the main win over the current HTMX pain.
- Keep the **terminal/monospace aesthetic** (JetBrains Mono, dark, orange accent).
  Port the look from `internal/web/static/css/app.css` (CSS vars: `--purple`
  #b079f0 = Twitch, `--kick` #53e7a4 = Kick, `--green`, `--accent` orange, `--red`,
  `--muted`). **Platform color rule: purple = Twitch, green = Kick — everywhere.**
- **Incremental migration**: stand up `/api/v1/*` alongside existing template
  routes; build the SPA; cut over page-by-page; delete templates last.
- Ship the built SPA static, served by the Go server (go:embed or static dir) so
  it stays ONE Docker container. Current server: `internal/api/server.go`
  (chi router); static served from `internal/web/static`, templates `internal/web/templates`.

## Architecture
```
Go backend (unchanged mining)  ──>  /api/v1/*.json  ──>  TanStack SPA (static, embedded)
```
- Auth: dashboard is admin-gated (session cookie + CSRF today, see server.go
  `authed` middleware). API should reuse the same session auth.
- Live updates: start with TanStack Query polling (2s like current HTMX
  `hx-trigger="every 2s/10s"`). Later: SSE/WebSocket — backend already has a log
  ring (`internal/log`), watcher snapshots, and Twitch PubSub.

## API surface to build (data already exists in handlers_*.go — just JSON-ify)
Map from current handlers:
- `GET /api/v1/telemetry` — watch-time today, claims 7d, active campaigns,
  in-progress, heartbeats/hr, uptime. (from `handlers_dashboard.go` dashTelemetry,
  `telemetryFrom` + `telemetryWithClaims`)
- `GET /api/v1/mining` — currently-mining per account: platform, login, channel,
  benefit, progress (X/Ym), %, ETA, state. (dashboard "Currently mining" /
  `/dashboard/cards`, watcher.Snapshot)
- `GET /api/v1/drops?tab=current|past|upcoming` — whitelisted rows + discoverable,
  each with: platform, game, campaign, when, **benefits[] (item name, image URL,
  requiredMinutes, drop TYPE, per-account collected state)**, account-linked
  (connection) state. (`handlers_drops.go` collectAll, `_drops_table.html`)
- `GET /api/v1/history` — claims (drop+reward, platform for color) + activity feed.
  (`handlers_history.go`)
- `GET /api/v1/accounts` — accounts w/ platform, login, whitelist + ranks,
  connection state, session validity.
- `POST /api/v1/accounts/{id}/games` (add), `DELETE` (remove), reorder (priority).
  **Per-account ONLY — no global whitelist add.** (`handlers_accounts.go`,
  `handlers_settings.go`, `loadAccountWhitelist`)
- `GET /api/v1/settings`, `POST` — priority mode, discord webhook, etc.

## FE feature backlog (the reason for the port — must all land)
1. **Per-drop ITEM table redesign** — the expanded campaign drop list
   (ITEM / REQUIRED columns + item icon). Redesign with TanStack Table.
2. **Per-account COLLECTED marks** — on each drop item, show which accounts have
   earned/claimed it. Backend source: see "backend prep" below.
3. **Drop TYPE display** — watch-time vs prime-sub / subscription / gift /
   action-required. We MINE only watch-time (`requiredMinutesWatched > 0`);
   surface the rest as "can't auto-do" badges.
4. **Discoverable expand shows CONTENTS first** — currently shows the
   add-to-account form; show drop benefits/type first, add control secondary.
5. **Consistent table styling** across ALL tables; platform-colored account names.
6. **Connection (account-linked) status** surfaced per campaign (we read
   `isAccountConnected` via gql already).
7. (from older backlog) dashboard "pass B": wire all panels to real data, activate
   dead filter buttons; drag-reorder priority that actually persists.

## Backend prep (being done NOW in the parallel A/B work — coordinate)
- **gameEventDrops claimed/owned detection** (in progress): `DropBenefit.RewardID`
  added (= benefitEdges[].benefit.id). Inventory parses `gameEventDrops[].id`
  (= benefit id, EXACT match per DevilXD inventory.py:74-90) → benefit owned.
  This gives per-account "what's collected" at the benefit level — the data source
  for feature #2 (collected marks). Once shipped, expose it in `/api/v1/drops`.
- **Claims table** has `(account_id, benefit_id, claimed_at)` — also usable for
  collected marks (what WE claimed). gameEventDrops is broader (includes
  externally/previously earned). Prefer gameEventDrops per-account for ownership.
- Drop TYPE: derive from `requiredMinutesWatched` (>0 = watch-time) + benefit name
  / precondition hints (sub/gift drops are the 0-minute ones we already skip in
  `campaigns.go fetchDetails`). Expose a `type` field in the drops API.

## Build/deploy
- Current deploy: build `deploy/Dockerfile.miner` → `dropsminer:latest`, compose
  at `~/projects/homelab/humblewhale/dropsminer` on host 10.10.2.40. Prod =
  drops.ryuzec.dev.
- Port build: add a JS build step (vite build) producing static assets embedded/
  served by Go. Multi-stage Dockerfile (node build → copy dist → go build).
- Module/identity: use `aalejandrofer` for any package/namespace. Never the other name.

## Pointers
- Existing CSS to mine for the aesthetic: `internal/web/static/css/app.css`.
- Existing templates to mirror: `internal/web/templates/{dashboard,drops,history,
  accounts_list,accounts_detail,settings}.html` + partials.
- Memory: project_frontend_port_plan.md, project_dashboard_backlog_2026-06-06.md,
  project_audit_2026-06-06.md.
