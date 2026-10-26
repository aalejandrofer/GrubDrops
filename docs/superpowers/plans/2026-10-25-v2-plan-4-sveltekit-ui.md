# v2 Plan 4: SvelteKit UI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** GrubDrops v2 gets its web UI: a SvelteKit static SPA in `web/`, built into `internal/web/dist` and embedded in the Go binary, with Home, Games, Rewards and Settings driven by the plan 3 JSON API and live SSE.

**Architecture:** `web/` is a SvelteKit app on `@sveltejs/adapter-static` with `fallback: 'index.html'` and `ssr = false`. One typed API client and two rune stores (session, live) feed every page; the live store holds one `EventSource` on `/api/events`, upserts runner and claim events, and refetches on `games`/`accounts` events and on reconnect. Pure logic (formatters, i18n, Up next reorder, card merge, device-code flow) lives in plain `.ts` files with vitest tests; components stay thin. Go serves the build through the existing SPA handler, which shows a built-in "UI not built" page when `dist/index.html` is missing, so `go build` and `go test` never need Node.

**Tech Stack:** SvelteKit 3.1.0, Svelte 5.57.2 (runes), TypeScript 6.0.3, Vite 8.3.5, `@sveltejs/adapter-static` 4.0.0, `@sveltejs/vite-plugin-svelte` 7.3.1, svelte-check 4.7.7, vitest 4.1.12, `@types/node` 25.9.9, npm on Node 25. Go side: chi, nosurf, `//go:embed all:dist`. Browser checks: `playwright-cli` (shell).

**Spec:** `docs/superpowers/specs/2026-10-25-grubdrops-v2-rebuild-design.md`. The API contract is the "API contract" section of `docs/superpowers/plans/2026-10-25-v2-plan-3-api-sse-wiring.md`, corrected by "Code vs contract" below. Visual reference: `sandbox/home-rebuild/r4/` (`index.html`, `states.html`, `games.html`, `rewards.html`, `settings.html`, `_assets/base.css`).

This is plan 4 of 5. Plans 1 to 3 are done: the miner runs v2 and serves JSON under `/api/*` with a placeholder at `/`. Plan 5 migrates 1.x data. Until plan 5 runs, a 1.x DB boots with no chosen games.

## Global Constraints

- Branch `rebuild/v2`, local only: no push, no PR. Go is at `/home/jandro/.local/go/bin`; call `go` and `gofmt` by full path. `gofmt -l .`, `go build ./...` and `go test ./...` stay green on every commit, with or without a UI build present.
- App lives in `web/`. npm only (pnpm is not installed). Pin every dependency to the exact versions in Tech Stack (no `^` or `~`), commit `web/package-lock.json`. Rejected on purpose: TypeScript 7 (kit peer is `^6`), vitest 5.x (engines exclude Node 25), jsdom 30 (same). Tests run in vitest's `node` environment; no DOM library.
- No other runtime or dev dependencies: no UI kit, router, CSS framework, date or i18n library, no Google Fonts. Fonts are the stacks `Inter, system-ui, -apple-system, "Segoe UI", sans-serif` and `"JetBrains Mono", ui-monospace, SFMono-Regular, Menlo, monospace`, unloaded.
- Svelte 5 runes only: `$state`, `$derived`, `$effect`, `$props`. No `export let`, no `svelte/store`. TypeScript `strict: true`.
- Never use `{@html}`. Release notes, game names, drop titles, log messages and every other server string render as text. A vitest test enforces this (Task 2).
- Every user-facing string goes through `t()`/`tn()`. Locale JSON stays in `internal/i18n/locales/{en,es,zh-CN}.json`; every key goes into all three files; `npm run check` fails on unequal sets, placeholder mismatches or unused keys.
- Copy is facts only: "Last scan 41 s ago", never "We check on every scan". No filler lines, no reassurance.
- `npm --prefix web run check` (svelte-kit sync, svelte-check, locale check) reports 0 errors and 0 warnings; `npm --prefix web test` is green.
- Build output goes to `internal/web/dist/`. Git tracks only `internal/web/dist/.gitkeep`.
- CSS: tokens in `web/src/app.css` on `:root` (dark, from r4 `_assets/base.css`) and `:root[data-theme="light"]`; components port r4 page CSS into their own `<style>` blocks. Breakpoints 960, 720, 420 px; top bar 52 px; container max 1160 px.
- Out of scope: setters for proxy, canary interval or Kick watch mode; 1.x data migration. Telegram shows disabled with "Coming in v1.5.0".
- Browser checks use a fresh `playwright-cli` session with no profile (`-s=grub-dev`, never `--profile` or `--persistent`), never `state-save`, never print cookies. Seed data comes from the local miner (fresh temp DB) and the fixture files in `web/src/lib/fixtures/`, never from real Twitch or Kick accounts. Never paste a real cookie or complete a real device login.
- Add one line per task to `docs/CHANGELOG.md` under `## [Unreleased]` in the style of the existing v2 lines. Tasks in parallel worktrees skip the changelog; the controller adds their lines.

## Code vs contract (code wins)

- Runner `state`: `idle`, `picking`, `starting`, `watching`, `claiming`, `sleeping`, `awaiting_connect`, `needs_login`, `no_games`.
- Overview `status.state`: `no_accounts`, `no_games`, `mining`, `nothing_live`, `idle`.
- Game `state`: `mining`, `waiting`, `no_campaign`. Blocked `reason`: `not_linked`, `precondition`, `skipped`.
- Health event `kind`: `claim`, `progress`, `state`, `discovery`, `error`, `auth`, `info`. Problems filter = level `WARN`/`ERROR` or kind `error`/`auth`. At most 50 events. Events carry no `code`; `msg` is a fixed English literal.
- `POST /api/accounts/{id}/kick/session` 400 also returns `"message"` (the cookies.txt parse error).
- There is no game DELETE: removing a game is `PUT /api/games` with its id left out.
- Overview `problems` come from runner snapshots only; `/api/accounts` `needs_login` is also true for an account with no session.
- `/api/games/search` returns no campaign info.
- `POST /api/accounts` defaults the name to "Twitch account" / "Kick account".
- `GET /api/settings` `timezone: ""` means the server zone (`TZ`, else UTC); the client cannot see it until Task 10 adds `zone_effective`.
- Runner recheck is every 5 min (`recheckInterval`); a full rescan is every 15 min.

## Calls made in this plan (check these)

1. **Settings order is General, Accounts, Notifications, Health, Updates**, as in the r4 HTML sub-nav and the spec. The r4 brief text lists Accounts first; the HTML wins.
2. **CSRF and auth.** The client reads `csrf_token` from `GET /api/session` and sends `X-CSRF-Token` on POST, PUT, PATCH and DELETE. A 403 `csrf` refreshes the session and retries once. Any 401 (except from `POST /api/login`) sends the user to `/login`, once.
3. **Unreachable** means a fetch `TypeError`, a 5xx response without a JSON `error` body, or an SSE error followed by a failing `GET /api/session`. A 502 `{"error":"upstream"}` is an ordinary `ApiError`, shown inline, not the red chip.
4. **SSE reconnect** backs off 1, 2, 4, 8, 16, 30, 30 … seconds, shows the countdown in the red chip, and on open refetches overview, runners, games, accounts and recent claims.
5. **Dev loop.** `npm --prefix web run dev` proxies `/api` and `/auth` to `GRUB_DEV_API` (default `http://localhost:8080`). The proxy rewrites `Origin` to the target origin and strips `Referer`, because nosurf checks Origin against Host.
6. **Dist tracking.** `adapter-static` wipes its output dir, so `npm run build` ends with `node scripts/keep-dist.mjs`, which rewrites `internal/web/dist/.gitkeep`.
7. **Health messages** are localised on the client by `kind` plus an exact `msg` lookup (`health.msg.*`, about 15 to 20 common lines); unknown messages show the raw English `msg`. Task 12 adds an allowlisted `fields` object to Health events so localised lines can name the game, drop or channel.
8. **Next scan** is computed client-side: `last_scan_at + 300 s`.
9. **Up next is one queue per account**: one sub-list per account with an account header when more than one account has a queue. "Reset to Games order" DELETEs the order of every account with `order_custom`.
10. **Search suggestions** show name plus "Already in your list, priority N." for chosen games; no "Campaign live now" lines (the API has no campaign info). Free text that matches no hit offers "Add “q”".
11. **Timezone row** is added to Settings General (not in r4), listing `Intl.supportedValuesOf('timeZone')` plus a "Server default (zone)" option.
12. **Canary status** shows as a fourth Health stat with a "Run now" button (spec requires it; r4 has no slot).
13. **Sign out** lives at the bottom of Settings General (r4 has no logout control; logout is public in the API).
14. **Version** shows `dev` when `session.version` is empty.
15. **Fonts** are not loaded, so screenshot comparisons against r4 ignore font differences.
16. **Health problem chip in the header** follows overview `problems`, so a sessionless account shows only on Settings Accounts. Go follow-up for plan 5 (listed at the end).

## Review Focus

1. **Upgrade with a tab open, or a fresh image with stale cached HTML:** expect `index.html` never cached and a missing hashed asset to return 404, not `index.html` parsed as JS. Tests: `TestSPA_CacheHeaders`, `TestSPA_MissingAssetIs404` (Task 1).
2. **The session expires while the UI is open:** expect one redirect to `/login`, no request storm, and SSE not reconnecting forever. Tests: `401 calls onUnauthorized once` (Task 4), `sse error with unauthed session stops reconnecting` (Task 4).
3. **The miner restarts while a tab is open:** expect the red chip with a countdown, then a full refetch, and the session-only Up next order gone (Reset hidden). Tests: `reconnect backoff 1,2,4,8,16,30` and `reconnect refetches everything` (Task 4), `reset hidden when refetched runners have order_custom false` (Task 6).
4. **The configured timezone differs from the browser's near midnight:** expect Today/Yesterday buckets and clock times in the settings zone. Test: `dayBucket uses the settings zone` (Task 3).
5. **A translator drops or renames a placeholder:** expect `npm run check` to fail, not a raw `{n}` on screen. Test: `placeholder mismatch fails` (Task 3).

---

## Browser check (BC)

Every shell and page task (2, 5 to 12) ends with this procedure; the task lists only its page-specific checks. Task 2 creates the two scripts. Run from the repo root.

```bash
# Build and boot a fresh local miner (background; prints GRUB_DEV_DIR=...)
npm --prefix web run build
web/scripts/dev-miner.sh                     # fresh temp DB on 127.0.0.1:18080
export GRUB_DEV_DIR=<printed dir>
web/scripts/dev-seed.sh                      # admin devpass123, 4 games, 2 sessionless accounts
#   web/scripts/dev-seed.sh --admin-only     # admin only: first-run state
#   (no seed at all)                         # setup screen

# Or the dev server against the same miner (live reload)
GRUB_DEV_API=http://127.0.0.1:18080 npm --prefix web run dev -- --port 5173 --strictPort

# Drive a fresh browser session (no profile)
playwright-cli -s=grub-dev open http://127.0.0.1:18080/login
playwright-cli -s=grub-dev resize 1280 900
playwright-cli -s=grub-dev snapshot                       # get refs
playwright-cli -s=grub-dev fill <password-ref> devpass123 --submit
playwright-cli -s=grub-dev goto http://127.0.0.1:18080/<page>
playwright-cli -s=grub-dev console warning                # expect: no errors, no warnings
playwright-cli -s=grub-dev requests                       # expect: no 4xx/5xx except ones the check provokes

# Fixture data the local miner cannot produce (runners mid-watch, claims)
playwright-cli -s=grub-dev route "**/api/runners" --content-type=application/json --body="$(cat web/src/lib/fixtures/runners.mining.json)"
playwright-cli -s=grub-dev reload
playwright-cli -s=grub-dev unroute                        # back to live data

# Screenshots: ours, then the mockup, then compare
playwright-cli -s=grub-dev screenshot --full-page --filename=web/.shots/<name>.png
playwright-cli -s=grub-dev goto "file://$PWD/sandbox/home-rebuild/r4/<mock>.html"
playwright-cli -s=grub-dev screenshot --full-page --filename=web/.shots/<name>.r4.png
playwright-cli -s=grub-dev resize 400 860                 # repeat the page screenshot at phone width
# Read both PNGs with the Read tool. List every difference except fonts and
# deliberate rulings in this plan; fix and repeat until none remain.

# Offline check (where a task asks for it)
kill "$(cat "$GRUB_DEV_DIR/miner.pid")"                   # red chip within 2 s, countdown ticks
web/scripts/dev-miner.sh --reuse "$GRUB_DEV_DIR"          # same DB; chip clears, data refetches

# Teardown
playwright-cli -s=grub-dev close
kill "$(cat "$GRUB_DEV_DIR/miner.pid")"
```

The check passes when the page renders, `console warning` is empty, the listed live data and actions work, and the screenshot differences are resolved. Record the screenshot paths and remaining deliberate differences in the task's report. `web/.shots/` is gitignored.

---

## File Structure

| File | Responsibility |
|---|---|
| `internal/api/server.go` | SPA handler: not-built page, cache headers, `/_app/` 404 |
| `internal/api/settings.go` | `zone_effective` (Task 10) |
| `internal/api/health.go` | allowlisted `fields` on events (Task 12) |
| `internal/web/embed.go`, `internal/web/dist/.gitkeep` | embed; only tracked dist file |
| `internal/i18n/locales/*.json`, `internal/i18n/i18n_test.go` | shared locales; 1.x keys removed |
| `web/package.json`, `package-lock.json`, `svelte.config.js`, `vite.config.ts`, `tsconfig.json` | app config |
| `web/dev-proxy.ts` | Vite proxy config with Origin rewrite |
| `web/scripts/keep-dist.mjs`, `locales-lib.mjs`, `check-locales.mjs`, `dev-miner.sh`, `dev-seed.sh` | build and dev tooling |
| `web/src/app.html`, `app.css`, `app.d.ts` | shell HTML, tokens |
| `web/src/lib/i18n/index.svelte.ts` | `t`, `tn`, language state |
| `web/src/lib/format.ts` | time, number, duration formatting |
| `web/src/lib/api/types.ts`, `client.ts` | API types and client |
| `web/src/lib/stores/session.svelte.ts`, `live.svelte.ts` | session and live data |
| `web/src/lib/status.ts` | header tone, status text, problem chip |
| `web/src/lib/upnext.ts`, `home.ts`, `games.ts`, `rewards.ts`, `accounts.ts`, `health.ts` | per-page pure logic |
| `web/src/lib/components/*.svelte` | TopBar, PageHead, VersionPopover, CopyCommand, PlatformMark, Icon, BoxArt, UpNext, GameCard, FirstRun |
| `web/src/lib/fixtures/*.json` | shared by vitest and BC routes |
| `web/src/routes/+layout.ts`, `+layout.svelte` | `ssr=false`, global CSS, theme |
| `web/src/routes/(auth)/login/+page.svelte`, `(auth)/setup/+page.svelte` | auth screens |
| `web/src/routes/(app)/+layout.svelte` | auth gate, live store start, shell |
| `web/src/routes/(app)/+page.svelte`, `games/`, `rewards/`, `settings/` | pages |
| `web/src/routes/(app)/settings/*.svelte` | one component per Settings section |
| `deploy/Dockerfile.miner`, `.dockerignore`, `.github/workflows/ci.yml`, `.gitignore` | build and CI |
| `README.md`, `docs/translations/README.*.md`, `docs/DESIGN.md`, `docs/index.html`, `AGENTS.md` | docs (Tasks 2, 13) |

## Parallelism

- **Sequential:** Task 1 and Task 2 can run in parallel (disjoint files). Then 3, 4, 5 in order.
- **Group B** (parallel worktrees from the Task 5 commit): 6 Up next, 8 Games, 9 Rewards, 10 Settings frame.
- **Group C** (parallel, after group B is merged): 7 Home (needs 6), 11 Settings Accounts and 12 Settings Health (need 10).
- **Then** 13 Docs, then 14 Final walk.
- Parallel tasks add locale keys only in their own namespace (`upnext.*`, `home.*`, `games.*`, `rewards.*`, `settings.*`, `accounts.*`, `health.*`). The controller merges locale JSON conflicts (keep both sides, keep key order sorted) and adds changelog lines. Each worktree runs `npm --prefix web ci` first. BC runs use port 18080 + group index (18081 to 18084) and session `grub-dev-<task>` so parallel workers do not collide; pass the port with `GRUB_DEV_PORT=1808N` to both scripts.

---

### Task 1: SPA build output in Go

**Files:**
- Modify: `internal/api/server.go` (`spa()`), `internal/web/embed.go`, `.gitignore`
- Delete: `internal/web/dist/index.html`. Create: `internal/web/dist/.gitkeep` (empty)
- Test: `internal/api/server_test.go`, `internal/web/embed_test.go`

**Interfaces:**
- Produces: `const notBuiltHTML` in `server.go`; SPA rules below. `web.Dist()` unchanged.

- [ ] **Step 1: Write the failing tests**
  - `TestSPA_NotBuilt`: SPA `fstest.MapFS{".gitkeep": {}}`. `GET /` and `GET /games` give 503, `Content-Type: text/html; charset=utf-8`, `Cache-Control: no-store`, body contains `UI not built, run <code>npm --prefix web run build</code>`. `GET /api/nope` still gives 404 JSON.
  - `TestSPA_CacheHeaders`: MapFS with `index.html` and `_app/immutable/entry/app.abc123.js`. `GET /` and `GET /rewards` give `Cache-Control: no-cache`; the JS file gives `Cache-Control: public, max-age=31536000, immutable` and `Content-Type` starting `text/javascript`.
  - `TestSPA_MissingAssetIs404`: `GET /_app/immutable/chunks/gone.js` gives 404, body not `index.html`.
  - `TestSPA_Fallback` (existing) still passes.
  - In `embed_test.go`, replace `TestDistHasIndex` with `TestDistEmbeds`: `fs.Stat(web.Dist(), ".gitkeep")` succeeds.
- [ ] **Step 2:** Run `/home/jandro/.local/go/bin/go test ./internal/api ./internal/web -run 'TestSPA|TestDist' -v`. Expected: FAIL (`TestSPA_NotBuilt` gets 404/200, cache headers missing).
- [ ] **Step 3: Implement.** In `spa()`: if `fs.Stat(files, "index.html")` fails, serve `notBuiltHTML` (a self-contained page: title "GrubDrops", the sentence above, no external assets). Paths under `/_app/` never fall back. Set headers before `http.FileServerFS`. `.gitignore`: replace the `!internal/web/dist/` line with `!internal/web/dist/`, `internal/web/dist/*`, `!internal/web/dist/.gitkeep` (in that order, after `dist/`), and add `web/node_modules/`, `web/.svelte-kit/`, `web/.shots/`.
- [ ] **Step 4:** Run `/home/jandro/.local/go/bin/go test ./... && /home/jandro/.local/go/bin/gofmt -l . && git status --short internal/web/dist`. Expected: PASS; no gofmt output; status shows only the deleted `index.html` and new `.gitkeep`.
- [ ] **Step 5:** Changelog: `- v2 SPA serving: a "UI not built" page when the build is missing, no-cache index.html, immutable hashed assets, and 404 for missing assets (rebuild/v2).` Commit `feat(api): SPA not-built page and cache headers`.

---

### Task 2: Scaffold `web/`, build, Docker, CI and dev loop

**Files:**
- Create: `web/package.json`, `web/package-lock.json`, `web/svelte.config.js`, `web/vite.config.ts`, `web/tsconfig.json`, `web/dev-proxy.ts`, `web/src/app.html`, `web/src/app.css`, `web/src/app.d.ts`, `web/src/routes/+layout.ts`, `web/src/routes/+layout.svelte`, `web/src/routes/+page.svelte` (temporary "GrubDrops" heading, replaced in Task 5), `web/scripts/keep-dist.mjs`, `web/scripts/dev-miner.sh`, `web/scripts/dev-seed.sh`
- Modify: `deploy/Dockerfile.miner`, `.dockerignore`, `.github/workflows/ci.yml`, `AGENTS.md` (Build/Run locally), `README.md` (development section)
- Test: `web/src/lib/no-html.test.ts`, `web/dev-proxy.test.ts`, `web/scripts/keep-dist.test.ts`

**Interfaces:**
- Produces:
  - npm scripts: `dev` = `vite dev`; `build` = `vite build && node scripts/keep-dist.mjs`; `test` = `vitest run`; `check` = `svelte-kit sync && svelte-check --tsconfig ./tsconfig.json --fail-on-warnings` (Task 3 appends `&& node scripts/check-locales.mjs`).
  - `svelte.config.js`: `adapter({ pages: '../internal/web/dist', assets: '../internal/web/dist', fallback: 'index.html', strict: true })`, `kit.alias: { $locales: '../internal/i18n/locales' }`.
  - `vite.config.ts`: `server.proxy = devProxy(process.env.GRUB_DEV_API ?? 'http://localhost:8080')`, `server.fs.allow: ['../internal/i18n/locales', '../assets/img']`, vitest `environment: 'node'`, `include: ['src/**/*.test.ts', 'scripts/**/*.test.ts', '*.test.ts']`.
  - `web/dev-proxy.ts`: `export function devProxy(target: string): Record<string, ProxyOptions>` for `/api` and `/auth` (`changeOrigin: true`, SSE-safe: no buffering), and `export function rewriteOrigin(headers: { setHeader(k: string, v: string): void; removeHeader(k: string): void; getHeader(k: string): unknown }, target: string): void`, called from `configure: proxy => proxy.on('proxyReq', ...)`.
  - `web/scripts/keep-dist.mjs`: `export function ensureGitkeep(dir)`; run as a script it calls `ensureGitkeep('../internal/web/dist')` resolved from the script's own URL.
  - `web/src/routes/+layout.ts`: `export const ssr = false; export const prerender = false;`.
  - `app.css` tokens (dark, `:root`): `--bg:#0d1117; --surface:#161b22; --surface-2:#1c2129; --surface-3:#262c36; --border:#30363d; --border-soft:#21262d; --text:#e6edf3; --muted:#8b949e; --dim:#6e7681; --t2:#a3acb7; --t3:#8b949e; --accent:#a371f7; --accent-strong:#8957e5; --accent-soft:rgba(163,113,247,.15); --green:#3fb950; --amber:#d29922; --red:#f85149; --blue:#58a6ff;` each status colour with `-soft` at `.15` alpha; `--twitch:#9146ff; --kick:#53fc18; --radius:8px`; body `14px/1.5`. Light (`:root[data-theme="light"]`): `--bg:#ffffff; --surface:#f6f8fa; --surface-2:#eaeef2; --surface-3:#d0d7de; --border:#d0d7de; --border-soft:#d8dee4; --text:#1f2328; --muted:#59636e; --dim:#6e7781; --t2:#424a53; --t3:#59636e; --accent:#8250df; --accent-strong:#6639ba; --accent-soft:rgba(130,80,223,.12); --green:#1a7f37; --amber:#9a6700; --red:#d1242f; --blue:#0969da`.
  - `web/scripts/dev-miner.sh [--reuse DIR]`: env `GRUB_DEV_PORT` (default 18080). Without `--reuse`: `D=$(mktemp -d -t grub-dev.XXXXXX)`, `go build -o "$D/miner" ./cmd/miner`, `GRUB_MASTER_KEY=$("$D/miner" keygen)` saved to `$D/key` (mode 600). Starts `GRUB_MASTER_KEY=... GRUB_DB_PATH=$D/miner.db GRUB_SECURE_COOKIES=0 GRUB_HTTP_ADDR=127.0.0.1:$PORT GRUB_UPDATE_CHECK=0 "$D/miner"` in the background with output to `$D/miner.log`, writes `$D/miner.pid`, waits up to 15 s for `curl -sf http://127.0.0.1:$PORT/healthz`, prints `GRUB_DEV_DIR=$D`. With `--reuse DIR`: same, reusing `DIR/miner`, `DIR/key` and `DIR/miner.db`.
  - `web/scripts/dev-seed.sh [--admin-only]`: env `GRUB_DEV_PORT`, `GRUB_DEV_URL` (default `http://127.0.0.1:$PORT`). Uses a curl cookie jar in `$GRUB_DEV_DIR/jar`; reads `csrf_token` from `GET /api/session` (with `node -e` JSON parse); `POST /api/setup` `{"password":"devpass123","confirm":"devpass123"}`; then unless `--admin-only`: `POST /api/games` for `Rust`, `Marvel Rivals`, `Apex Legends`, `Delta Force`; `POST /api/accounts` `{"platform":"twitch","name":"seed_twitch"}` and `{"platform":"kick","name":"seed_kick"}`. Fails loudly on any non-2xx.
  - Dockerfile: new first stage `FROM --platform=$BUILDPLATFORM node:25-alpine AS web`, `WORKDIR /src/web`, copy `web/package.json web/package-lock.json`, `npm ci`, copy `web/` and `internal/i18n/locales/` and `assets/img/`, `npm run build`. The Go stage copies `--from=web /src/internal/web/dist ./internal/web/dist` after `COPY . .` and before `go build`.
  - `.dockerignore`: add `web/node_modules`, `web/.svelte-kit`, `web/.shots`, `internal/web/dist/*` with `!internal/web/dist/.gitkeep`.
  - CI: new job `web` with `actions/checkout@v7`, `actions/setup-node@v7` (`node-version: 25`, `cache: npm`, `cache-dependency-path: web/package-lock.json`), then `npm ci`, `npm run check`, `npm test`, `npm run build`, all with `working-directory: web`. The Go job stays unchanged and Node-free.

- [ ] **Step 1: Write the failing tests**
  - `no-html.test.ts` / `no {@html} anywhere in src`: walk `web/src` for `*.svelte`; assert no file contains `{@html`.
  - `dev-proxy.test.ts` / `rewriteOrigin sets target origin and drops referer`: fake headers `{origin:'http://localhost:5173', referer:'http://localhost:5173/games'}`, target `http://127.0.0.1:18080` gives `origin === 'http://127.0.0.1:18080'` and no `referer`. `/ devProxy covers /api and /auth`: keys are exactly `['/api','/auth']`, both `target` equal the argument.
  - `keep-dist.test.ts` / `ensureGitkeep recreates .gitkeep`: temp dir without the file; after the call `.gitkeep` exists and is empty; calling it twice does not throw.
- [ ] **Step 2:** Run `npm --prefix web install` (exact pins), then `npm --prefix web test`. Expected: FAIL (`dev-proxy`, `keep-dist` modules missing).
- [ ] **Step 3: Implement** the files above.
- [ ] **Step 4:** Run `npm --prefix web test && npm --prefix web run check && npm --prefix web run build && ls internal/web/dist/index.html internal/web/dist/.gitkeep && git status --short internal/web/dist && /home/jandro/.local/go/bin/go build ./... && /home/jandro/.local/go/bin/go test ./...`. Expected: all pass; `git status` prints nothing for `internal/web/dist`. Then `docker build -f deploy/Dockerfile.miner .` succeeds (if Docker is not available in the environment, say so in the report; CI covers it).
- [ ] **Step 5: BC.** Boot with `dev-miner.sh`, no seed. Checks: `open http://127.0.0.1:18080/` renders the temporary heading; `eval "getComputedStyle(document.body).backgroundColor"` is `rgb(13, 17, 23)`; `console warning` empty. Start the dev server against the miner and run `curl -s -o /dev/null -w '%{http_code}' -H 'Origin: http://localhost:5173' -H "X-CSRF-Token: $T" -b jar -c jar -X POST -d '{"password":"devpass123","confirm":"devpass123"}' http://localhost:5173/api/setup` (with `$T` from `curl -s -c jar http://localhost:5173/api/session`): expect `204`, proving the Origin rewrite. `curl -sN http://localhost:5173/api/events -b jar` streams (`: ping` within 25 s).
- [ ] **Step 6:** AGENTS.md "Run locally": replace the placeholder sentence with the build command, the `GRUB_DEV_API` dev loop, and the two dev scripts. README: add a "Development" subsection with the same three commands. Changelog: `- v2 web app scaffold in web/ (SvelteKit static SPA), built into internal/web/dist by npm, the Docker image and CI (rebuild/v2).` Commit `feat(web): scaffold SvelteKit app, build, Docker and CI`.

---

### Task 3: i18n, formatters and the locale check

**Files:**
- Create: `web/src/lib/i18n/index.svelte.ts`, `web/src/lib/format.ts`, `web/scripts/locales-lib.mjs`, `web/scripts/check-locales.mjs`
- Modify: `internal/i18n/locales/{en,es,zh-CN}.json` (delete 1.x keys, add `time.*`, `common.*`), `internal/i18n/i18n_test.go` (`TestSpanishLoaded` uses `summary.title` = `Resumen diario`), `web/package.json` (`check` appends `&& node scripts/check-locales.mjs`)
- Test: `web/src/lib/i18n/i18n.test.ts`, `web/src/lib/format.test.ts`, `web/scripts/locales.test.ts`

**Interfaces:**
- Produces:
  - `type Lang = 'en' | 'es' | 'zh-CN'`; `const LANGS: { code: Lang; label: string }[]` = `en` English, `es` Español, `zh-CN` 简体中文.
  - `const i18n: { lang: Lang }` (`$state`); `setLang(l: Lang): void` (also sets `document.documentElement.lang` when a document exists).
  - `t(key: string, vars?: Record<string, string | number>): string`: current lang, then `en`, then the key itself; replaces `{name}`.
  - `tn(key: string, n: number, vars?: Record<string, string | number>): string`: `Intl.PluralRules(lang).select(n)` picks `key.<category>`, falling back to `key.other`; `{n}` is `n` formatted with `formatInt`.
  - `matchLang(prefs: readonly string[]): Lang`: first of `prefs` starting `zh` gives `zh-CN`, `es` gives `es`, `en` gives `en`; else `en`.
  - `format.ts` (all take explicit `now: number` ms and IANA `zone`):
    - `relTime(iso: string | null, now: number): string`: `{n} s ago` under 60 s, `{n} min ago` under 60 min, `{n} h ago` under 48 h, then `{n} d ago`; `null` gives `t('time.never')`. Negative ages clamp to 0.
    - `formatInt(n: number): string` (Intl in current lang).
    - `etaShort(min: number): string`: `18m`, `1h 15m`, `0m`.
    - `uptime(startedIso: string, now: number): string`: `6d 4h`, `3h 12m`, `4m`.
    - `clock(iso: string, zone: string, fmt: '24h' | '12h'): string`: `09:12` or `9:12 AM`; U+202F and U+00A0 replaced by a space.
    - `dayBucket(iso: string, now: number, zone: string): 'today' | 'yesterday' | 'earlier'`.
    - `shortDate(iso: string, zone: string): string`: `Oct 23` (en), via `{month:'short', day:'numeric'}`.
    - `claimStamp(iso: string, now: number, zone: string, fmt: '24h' | '12h'): string`: `Today 09:12`, `Yest. 22:05`, else `shortDate`.
    - `nextScanMin(lastScanIso: string | null, now: number, everySec = 300): number`: whole minutes until `last + everySec`, rounded up, minimum 0; `null` gives 0.
  - `locales-lib.mjs`: `placeholders(s: string): string[]` (sorted `{name}` names plus printf verbs `%s %d %v %q %%`), `diffKeys(locales: Record<string, Record<string,string>>): string[]` (problems as text), `referencedKeys(sources: string[]): Set<string>` (every quoted string literal in `web/src/**/*.{ts,svelte}` and the first argument of Go `i18n.T(` in `**/*.go` outside `web/`; for `tn('x', ...)` both `x.one` and `x.other`), `unusedKeys(keys: string[], refs: Set<string>): string[]`.
  - `check-locales.mjs`: loads the three files, prints every problem, exits 1 on any.
  - Keys added here (en values): `time.never` "never", `time.s_ago` "{n} s ago", `time.min_ago` "{n} min ago", `time.h_ago` "{n} h ago", `time.d_ago` "{n} d ago", `time.today_at` "Today {time}", `time.yesterday_at` "Yest. {time}", `common.copy` "Copy", `common.copied` "Copied", `common.cancel` "Cancel", `common.save` "Save", `common.remove` "Remove", `common.retry` "Retry". es and zh-CN get real translations.
- 1.x key removal: delete every key in all three locales except `summary.title` and `summary.message`, then add the keys above. Go keeps using only `summary.*`.

- [ ] **Step 1: Write the failing tests**
  - `i18n.test.ts`: `t interpolates {n}` (`t('time.s_ago',{n:41})` is `41 s ago`); `t falls back to en then key` (unknown es key gives en value; unknown key gives the key); `tn picks one/other` (en 1 and 2, es 1 and 2, zh-CN 1 gives `other`); `tn falls back to other` (es 1000000 has category `many`, result uses `.other`); `matchLang` (`['zh-Hans-CN']` gives `zh-CN`, `['es-MX','en']` gives `es`, `['fr']` gives `en`).
  - `format.test.ts` (lang `en`): `relTime` 41 s gives `41 s ago`, 3 min, 2 h, 4 d, null gives `never`; `formatInt(18240)` is `18,240` (en) and `18.240` (es); `etaShort(18)` `18m`, `etaShort(75)` `1h 15m`; `uptime` 6d 4h; `clock('2026-10-25T09:12:00Z','UTC','24h')` `09:12` and `'12h'` gives `9:12 AM` (plain space); **`dayBucket uses the settings zone`**: `dayBucket('2026-10-24T23:30:00Z', Date.parse('2026-10-25T10:00:00Z'), 'Asia/Shanghai')` is `today` and with `'UTC'` is `yesterday`, and `'2026-10-23T10:00:00Z'` with `'UTC'` is `earlier`; `claimStamp` gives `Today 09:12`, `Yest. 22:05`, `Oct 23`; `nextScanMin` with last scan 120 s ago gives 3, 400 s ago gives 0, null gives 0.
  - `locales.test.ts`: `unequal key sets fail` (`es` missing `a.b` reports `es: missing a.b`); **`placeholder mismatch fails`** (`en` `"{n} s ago"` vs `es` `"hace {m} s"` reports `es: a.b placeholders`); `printf verbs compared` (`%s %d` vs `%d` fails); `unused key fails` (key `x.y` not in refs reported); `tn refs cover .one and .other`; `the real locales pass` (run against `internal/i18n/locales` and the current sources: zero problems).
- [ ] **Step 2:** Run `npm --prefix web test`. Expected: FAIL (modules missing).
- [ ] **Step 3: Implement**, prune the locale files, update `TestSpanishLoaded`.
- [ ] **Step 4:** Run `npm --prefix web test && npm --prefix web run check && /home/jandro/.local/go/bin/go test ./internal/i18n ./...`. Expected: PASS; each locale file has the same key count.
- [ ] **Step 5:** Changelog: `- v2 shared locales: the SvelteKit app reads internal/i18n/locales, 1.x-only keys are gone, and npm run check fails on missing, unused or mismatched keys.` Commit `feat(web): i18n, formatters and locale check`.

---

### Task 4: API client, types and stores

**Files:**
- Create: `web/src/lib/api/types.ts`, `web/src/lib/api/client.ts`, `web/src/lib/stores/session.svelte.ts`, `web/src/lib/stores/live.svelte.ts`, `web/src/lib/fixtures/{session,overview.mining,overview.nothing_live,overview.first_run,runners.mining,runners.many,games,claims,accounts,health,update,settings,device}.json`
- Test: `web/src/lib/api/client.test.ts`, `web/src/lib/stores/live.test.ts`

**Interfaces:**
- Consumes: the API contract (plan 3) and "Code vs contract".
- Produces:
  - `types.ts`: `Session`, `Overview`, `OverviewState`, `Problem`, `Runner`, `RunnerState`, `Target`, `Blocked`, `GamesResp`, `Game`, `GameState`, `Campaign`, `SearchHit`, `ClaimsQuery {game?: string; account?: string; limit?: number; offset?: number}`, `ClaimsResp`, `Claim`, `Account`, `Settings` (with optional `zone_effective`), `SettingsPatch` (any subset plus `discord_webhook?`), `Health`, `HealthEvent` (with optional `fields?: Record<string,string>`), `Canary`, `UpdateInfo`, `DeviceStart`, `DeviceStatus`, `ApiErrorBody {error: string; message?: string; priority?: number}`. Field names exactly as the JSON.
  - `class ApiError extends Error { status: number; code: string; body: ApiErrorBody }`; `class Unreachable extends Error`.
  - `createClient(opts: { fetch?: typeof fetch; csrf: () => string; refreshCsrf: () => Promise<void>; onUnauthorized: () => void; onUnreachable: (down: boolean) => void }): Api`. `Api` methods: `session()`, `setup(password, confirm)`, `login(password)`, `logout()`, `overview()`, `runners()`, `setOrder(accountId, benefitIds)`, `resetOrder(accountId)`, `games()`, `addGame(name)`, `setGames(ids)`, `searchGames(q)`, `claims(q: ClaimsQuery)`, `accounts()`, `addAccount(platform, name?)`, `renameAccount(id, name)`, `removeAccount(id)`, `twitchStart(id)`, `twitchStatus(id)`, `kickSession(id, cookiesTxt)` (sends `channels: []`), `settings()`, `saveSettings(p: SettingsPatch)`, `notifyTest()`, `health(filter: 'all' | 'problems' | 'claims')`, `runCanary()`, `update()`, `checkUpdate()`. Each returns the typed body (`void` for 204). Every successful response calls `onUnreachable(false)`.
  - `session.svelte.ts`: `createSession(api: Api)` returning `{ loaded, authed, setupNeeded, oidc, csrf, version }` (`$state`) plus `load(): Promise<void>`; the app exports a singleton `session` and `api` from `web/src/lib/app.ts` (created here) wiring `csrf: () => session.csrf`, `refreshCsrf: session.load`, `onUnauthorized: () => goto('/login')` (only when not already on `/login` or `/setup`).
  - `live.svelte.ts`: `createLive(deps: { api: Api; eventSource: (url: string) => EventSourceLike; schedule?: (fn: () => void, ms: number) => () => void; now?: () => number; onUnauthorized: () => void })` returning `$state` fields `overview: Overview | null`, `runners: Runner[]`, `games: GamesResp | null`, `accounts: Account[]`, `recent: Claim[]` (newest first, at most 10), `log: HealthEvent[]` (newest first, at most 50), `unreachable: boolean`, `retryIn: number` (seconds, ticks down), `updatedAt: number | null`; methods `start()`, `stop()`, `refetchAll(): Promise<void>`, `seedLog(events: HealthEvent[])`. `EventSourceLike = { onopen, onerror, addEventListener(name, fn), close() }`.
  - `upsertBy<T>(list: T[], item: T, key: (x: T) => string): T[]` (replace in place, else prepend), exported from `live.svelte.ts`.
  - Event handling: `runner` upserts `runners` by `account_id`; `claim` upserts `recent` by `account_id + ':' + benefit_id`, trims to 10, and refetches `overview`; `games` refetches `games` and `overview`; `accounts` refetches `accounts`, `runners` and `overview`; `log` prepends to `log`, trims to 50. Every event sets `updatedAt`.
  - Reconnect: on `onerror` close the source, call `api.session()`; if it returns `authed:false` (or 401), call `onUnauthorized` and stop for good; otherwise set `unreachable` and schedule a reconnect after `BACKOFF = [1, 2, 4, 8, 16, 30]` seconds (last value repeats); reset the index on `onopen`, which also clears `unreachable` and calls `refetchAll()`.
  - Fixture rules: account names `hJune`, `grub_alt`, `kick_main`, and `seed_*`; games Rust, Marvel Rivals, Apex Legends, Delta Force; no real people's names. `runners.mining.json` has 3 runners (2 watching Rust at 42/60 and 18/60, 1 `needs_login`), each watching runner with 2 `up_next`. `runners.many.json` has 10 runners on Rust (8 watching, 2 idle). `claims.json` has `total: 27` and 10 items spanning today, yesterday and earlier relative to a fixed date: tests pass `now` explicitly.

- [ ] **Step 1: Write the failing tests** (fake `fetch` recording calls; fake `EventSourceLike`; fake `schedule` that records delays)
  - `client.test.ts`: `sends X-CSRF-Token on unsafe methods only` (`games()` no header; `addGame`, `setGames`, `renameAccount`, `removeAccount` carry `X-CSRF-Token: tok`); `json body and content-type on writes`; **`401 calls onUnauthorized once`** (five parallel calls all 401 give one `onUnauthorized` call within one tick, and each rejects with `ApiError` status 401); `login 401 does not redirect`; `csrf 403 refreshes session and retries once` (first 403 `{"error":"csrf"}`, `refreshCsrf` called once, second attempt 204 resolves; a second 403 rejects); `network failure marks unreachable` (fetch throws `TypeError`, rejects with `Unreachable`, `onUnreachable(true)`); `502 upstream JSON is an ApiError, not unreachable` (`onUnreachable` not called with true); `500 HTML is unreachable`; `409 keeps body.priority` (`addGame` rejects with `code:'conflict'`, `body.priority === 4`); `kick 400 keeps message`; `claims builds query string` (`{game:'Rust', account:'a1', limit:10, offset:10}` gives `/api/claims?game=Rust&account=a1&limit=10&offset=10`, empty values omitted).
  - `live.test.ts`: `runner event upserts by account_id` (existing replaced in place, order kept; unknown appended); `claim event upserts by account_id + benefit_id` (same pair twice gives one entry; 11 distinct give 10, newest first); `games event refetches games and overview`; `accounts event refetches accounts, runners and overview`; `log event prepends, capped at 50`; **`reconnect backoff 1,2,4,8,16,30`** (seven consecutive errors schedule 1000, 2000, 4000, 8000, 16000, 30000, 30000 ms; `unreachable` true; `retryIn` starts at the delay in seconds); **`reconnect refetches everything`** (after `onopen` following an error: `overview`, `runners`, `games`, `accounts` and `claims({limit:10})` each called once more, `unreachable` false, backoff index reset); **`sse error with unauthed session stops reconnecting`** (`session()` returns `authed:false`: `onUnauthorized` called once, nothing scheduled, a later error does nothing).
- [ ] **Step 2:** Run `npm --prefix web test`. Expected: FAIL.
- [ ] **Step 3: Implement.** Deduplicate `onUnauthorized` with a flag reset by `session.load()` returning `authed:true`.
- [ ] **Step 4:** Run `npm --prefix web test && npm --prefix web run check`. Expected: PASS, 0 errors.
- [ ] **Step 5:** Changelog: `- v2 web data layer: typed API client with CSRF, 401 and offline handling, and one SSE store that upserts runners and claims and refetches on reconnect.` Commit `feat(web): API client, session and live stores`.

---

### Task 5: Shell, header, version popover and auth screens

**Files:**
- Create: `web/src/lib/status.ts`, `web/src/lib/components/{TopBar,PageHead,VersionPopover,CopyCommand,PlatformMark,Icon,BoxArt}.svelte`, `web/src/routes/(app)/+layout.svelte`, `web/src/routes/(app)/+page.svelte` (Home skeleton: `PageHead` plus empty main/aside columns that Tasks 6 and 7 fill), `web/src/routes/(app)/{games,rewards,settings}/+page.svelte` (heading-only stubs), `web/src/routes/(auth)/login/+page.svelte`, `web/src/routes/(auth)/setup/+page.svelte`
- Delete: `web/src/routes/+page.svelte` (temporary)
- Modify: `web/src/routes/+layout.svelte` (load session, apply `data-theme` and language from settings once authed), locales (`app.*`, `nav.*`, `status.*`, `version.*`, `auth.*`)
- Test: `web/src/lib/status.test.ts`

**Interfaces:**
- Consumes: `api`, `session` (`app.ts`), `createLive`, `t`/`tn`, `relTime`, fixtures.
- Produces:
  - `headerTone(o: Overview | null, unreachable: boolean): 'green' | 'amber' | 'red' | 'grey'`: unreachable gives red; `mining` with no problems gives green; any problem gives amber; else grey.
  - `statusText(o: Overview | null, unreachable: boolean): string`: `Miner offline`; `Not set up` (no_accounts, no_games); `Mining · {m} of {n} accounts` (`status.mining`, plural via `tn`); `Idle · watching for campaigns` (nothing_live); `Idle` (idle).
  - `problemChip(o: Overview | null): { text: string; href: string } | null`: one `needs_login` gives `1 account needs login`, n gives `{n} accounts need login` (`tn('status.needs_login', n)`), `href: '/settings#accounts'`; none gives null. One chip only.
  - `PageHead` props: `{ title: string; stamp?: string }`; renders dot (tone), status text, problem chip (amber link), and when `live.unreachable` the red chip `Can't reach the miner · retrying in {s} s` plus stamp `Last updated {time}` (`clock(updatedAt)`). Offline pages render their last data inside a wrapper with `inert` and opacity `.55`, plus the help line `Check the container is running:` with `CopyCommand` `docker compose ps`.
  - `TopBar`: logo `assets/img/logo.png` (imported via Vite), nav Home `/`, Games `/games`, Rewards `/rewards`, Settings `/settings` with `aria-current="page"`, and `VersionPopover` at right.
  - `VersionPopover` props `{ current: string; update: UpdateInfo | null }`: label `current || 'dev'`; white when no update, `--blue` when `update_available`. Latest: `You're on the latest version, {v}` plus `Release notes` link (`notes_url`). Update: `{v} is available`, notes as plain-text bullets, `CopyCommand` with `command` (default `docker compose pull && docker compose up -d`), `Full release notes`. Closes on outside click and Escape; focus returns to the trigger.
  - `CopyCommand` props `{ command: string }`: button text `Copy` then `Copied` for 2 s.
  - `BoxArt` props `{ src: string | null; name: string; size: 'lg' | 'sm' }`: `<img>` with `onerror` switching to the patterned fallback (CSS stripes in `--surface-3`, initial letter).
  - `(app)/+layout.svelte`: calls `session.load()`; `setupNeeded` goes to `/setup`, `!authed` goes to `/login`; else creates and starts one live store (exported via context key `'live'`) and fetches `api.update()` once for the popover; stops it on destroy.
  - Login: password field, submit `api.login`, on 204 `session.load()` then `goto('/')`; wrong password shows `Wrong password.`; when `session.oidc.enabled`, a button `Sign in with {name}` linking to `/auth/oidc/login`; `?login_error=oidc` shows the banner `Single sign-on failed. Try again or use the password.` Setup: password and confirm, client checks `>= 8` characters (`At least 8 characters.`) and equality (`Passwords don't match.`), `api.setup` then `session.load()` then `goto('/')` (setup logs in).
- [ ] **Step 1: Write the failing tests** (`status.test.ts`, fixtures): `headerTone` cases (unreachable red; mining fixture with one problem amber; mining with problems removed green; `nothing_live` grey); `statusText` for each overview state and offline; `problemChip one and many` (one gives `1 account needs login`, two give `2 accounts need login`, href `/settings#accounts`; none gives null).
- [ ] **Step 2:** Run `npm --prefix web test -- status`. Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4:** Run `npm --prefix web test && npm --prefix web run check`. Expected: PASS.
- [ ] **Step 5: BC.** No seed: `open http://127.0.0.1:18080/` redirects to `/setup`; a 7-character password shows the length error; matching `devpass123` lands on `/` logged in. `goto /login?login_error=oidc` shows the banner and no SSO button. Tear down, boot a fresh miner with the full seed (`dev-miner.sh`, then `dev-seed.sh`), and log in through the form, wrong password first. Header: dot and status text; route `**/api/overview` to `overview.mining.json`, reload: amber chip `1 account needs login` links to `/settings#accounts`. Version popover: click opens, Escape closes, outside click closes; with `**/api/update` routed to `update.json` (`update_available: true`) the label is blue and Copy turns to Copied. Offline check: red chip with a ticking countdown, page content dimmed and not clickable; after `--reuse` restart the chip clears without a reload. Screenshots: header of `/` against `r4/index.html`, offline against `r4/states.html#offline`, login and setup at 1280 and 400 px.
- [ ] **Step 6:** Changelog: `- v2 web shell: top bar, status header with problem and offline chips, version popover, login with SSO and first-run setup.` Commit `feat(web): shell, header, version popover and auth screens`.

---

### Task 6: Up next (group B)

**Files:**
- Create: `web/src/lib/upnext.ts`, `web/src/lib/components/UpNext.svelte`
- Modify: `web/src/routes/(app)/+page.svelte` (mount `UpNext` in the aside), locales `upnext.*`
- Test: `web/src/lib/upnext.test.ts`

**Interfaces:**
- Consumes: `live.runners`, `api.setOrder`, `api.resetOrder`, `api.runners`.
- Produces:
  - `moveItem<T>(list: readonly T[], from: number, to: number): T[]` (new array; out-of-range is a no-op copy).
  - `type Queue = { accountId: string; accountName: string; platform: 'twitch' | 'kick'; items: Target[]; custom: boolean }`; `queues(runners: Runner[]): Queue[]` (runners with non-empty `up_next`, in runner order).
  - `afterLabel(r: Runner, index: number): string | null`: item 0 gives `current?.benefit_name ?? null`, others the previous item's `benefit_name`.
  - `anyCustom(runners: Runner[]): boolean`.
  - `UpNext.svelte`: header `Up next` with total count and hint `Order for this session only`; per queue an account header (only when 2 or more queues); items: grip, number, drop, `{game} · {campaign}`, `{account} · {min} min · after {drop}`; a `Move up` button (hidden on item 0) and pointer drag. A move updates the local list, then `setOrder(accountId, benefitIds)`; on failure refetch `api.runners()` and show `Couldn't save the order.`. Foot: `Permanent order lives on Games.` (link `/games`) and `Reset to Games order`, rendered only when `anyCustom`; it calls `resetOrder` for each custom queue. Empty: `Nothing queued. Next scan in {n} min.` (`nextScanMin`).
- [ ] **Step 1: Write the failing tests**: `moveItem up and down` (`[a,b,c]` 2 to 0 gives `[c,a,b]`; 0 to 2 gives `[b,c,a]`; 5 to 0 unchanged); `queues skips runners without up_next` (runners.mining gives 2 queues, names `hJune`, `grub_alt`); `afterLabel` (item 0 gives current drop, item 1 gives item 0's drop); **`reset hidden when refetched runners have order_custom false`** (`anyCustom` true for a runner with `order_custom:true`, false after replacing it with the refetched fixture).
- [ ] **Step 2:** Run `npm --prefix web test -- upnext`. Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4:** Run `npm --prefix web test && npm --prefix web run check`. Expected: PASS.
- [ ] **Step 5: BC.** Seeded miner; route `**/api/runners` to `runners.mining.json` and `**/api/runners/*/order` with `--status=204`. On `/`: two queues with account headers; `Move up` on item 2 reorders it; `requests` shows `PUT /api/runners/<id>/order` with the new `benefit_ids`; Reset appears, click sends `DELETE`. Drag item 1 onto item 0 with `playwright-cli drag`. Unroute: empty queue line `Nothing queued. Next scan in N min.` Screenshot the aside against the Up next panel of `r4/index.html`.
- [ ] **Step 6:** Commit `feat(web): Up next queues with session reorder` (changelog line added by the controller: `- v2 Up next: one queue per account, reorder by button or drag for this session, Reset to Games order.`).

---

### Task 7: Home (group C, needs 6)

**Files:**
- Create: `web/src/lib/home.ts`, `web/src/lib/components/{GameCard,FirstRun}.svelte`
- Modify: `web/src/routes/(app)/+page.svelte`, locales `home.*`
- Test: `web/src/lib/home.test.ts`

**Interfaces:**
- Consumes: `live.overview`, `live.games`, `live.runners`, `live.recent`, `live.unreachable`, `api.searchGames`, `api.addGame`, `queues`, formatters, `PageHead`, `BoxArt`, `UpNext`.
- Produces:
  - `normName(s: string): string` (trim, lowercase, collapse spaces).
  - `type AccountRow = { runner: Runner; progress: number; muted: boolean }` (`progress` = `current.minutes / current.required_minutes`, 0 without current; `muted` when `needs_login`).
  - `type GameCard = { game: Game; rank: number; size: 'lg' | 'sm'; rows: AccountRow[]; queued: Target[]; moreInUpNext: number }`; `gameCards(g: Game[], r: Runner[]): GameCard[]`: games in priority order, rank from 1, `lg` for rank 1 and 2; `rows` = runners whose `current.game` matches by `normName`, plus `needs_login` runners on rank 1 only, sorted by `progress` descending; `queued` = up_next targets for that game across runners (for `waiting` cards); `moreInUpNext` = count of that game's up_next targets.
  - `foldRows(rows: AccountRow[], limit = 3): { shown: AccountRow[]; hidden: number; mining: number; idle: number }`.
  - `homeState(o: Overview | null, unreachable: boolean): 'first_run' | 'healthy' | 'nothing_live' | 'offline'` (first_run for `no_accounts`/`no_games`; offline when unreachable, which keeps the last data).
  - `groupClaims(c: Claim[], now: number, zone: string): { today: Claim[]; yesterday: Claim[]; earlier: Claim[] }`.
  - Page: KPIs `Total Minutes Watched` (`{n} min`, sub `{today} today · {n} accounts`), `Drops claimed today` (sub `{n} all time`, links `/rewards`), `Accounts healthy` (`{h}/{n}`, amber when `h < n`, sub names the first unhealthy account `{name} signed out`). `Your games` with count and `Edit games` (`/games`). Card rows: name, drop, `· {channel}`, `{m}/{req}`, bar, `{eta} left`; muted row `Signed out. Not mining.`; `waiting` cards `Starts when {account} is free. Queued:` plus list; `no_campaign` cards `No campaign` and `Last scan {rel}`; foot `{n} more {game} drops in Up next`. Many accounts: summary `{m} mining · {i} idle`, top 3, toggle `Show all {n}` / `Show fewer`, foot `Most advanced first`. Claimed panel: `last 10 · {n} today`, groups Today / Yesterday / Earlier with `clock`, foot `{n} all time` and `All rewards`. Nothing live: `All {n} accounts are ready. Mining starts on the next scan that finds a campaign.` First run (`FirstRun`): 3 steps `Add an account` (Twitch and Kick buttons linking `/settings#accounts` with `?add=twitch|kick`), `Pick your games` (search with suggestions and Add, ticks when `games.length > 0`), `Done`; `How it works` panel with three fact lines. Zone = settings `timezone || zone_effective || 'UTC'`.
- [ ] **Step 1: Write the failing tests** (fixtures, fixed `now`): `gameCards ranks and sizes` (4 games give sizes `lg,lg,sm,sm`); `rows matched by normalised name` (`" rust "` matches `Rust`); `needs_login muted on card 1 only`; `rows sorted most advanced first` (42/60 before 18/60); `moreInUpNext counts that game's queued drops`; `foldRows on 10 runners` (`shown.length 3`, `hidden 7`, `mining 8`, `idle 2`); `homeState` each case; `groupClaims buckets in the settings zone`.
- [ ] **Step 2:** Run `npm --prefix web test -- home`. Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4:** Run `npm --prefix web test && npm --prefix web run check`. Expected: PASS.
- [ ] **Step 5: BC.** First run (`--admin-only` seed): steps render; type `Rus` in step 2 shows suggestions from the local catalog or `Add “Rus”`; adding `Rust` ticks step 2 without a reload (`games` SSE event); step 1 buttons open `/settings?add=twitch#accounts`. Healthy: full seed plus routes `overview.mining.json`, `runners.mining.json`, `**/api/claims**` to `claims.json`; check KPIs, card 1 rows with the muted `grub_alt`, card 2 queued list, cards 3 and 4 small with `Last scan`, claimed groups. Nothing live: route `overview.nothing_live.json`, runners `[]`. Many accounts: `runners.many.json`, toggle `Show all 10` and back. Offline: kill and `--reuse` as in BC. Screenshots against `r4/index.html` and `r4/states.html#first-run`, `#nothing-live`, `#offline`, `#many-accounts`, at 1280 and 400 px.
- [ ] **Step 6:** Commit `feat(web): Home with game cards, KPIs, claimed and states` (controller changelog: `- v2 Home: KPIs, ranked game cards with per-account progress, Up next, recent claims, and first-run, nothing-live, offline and many-account states.`).

---

### Task 8: Games (group B)

**Files:**
- Create: `web/src/lib/games.ts`
- Modify: `web/src/routes/(app)/games/+page.svelte`, locales `games.*`
- Test: `web/src/lib/games.test.ts`

**Interfaces:**
- Consumes: `api.games`, `api.searchGames`, `api.addGame`, `api.setGames`, `live.games` (refetched on `games` events), `moveItem` (re-implement locally if Task 6 is not merged yet: same signature, then the controller dedupes to `upnext.ts` at merge), `BoxArt`, `PlatformMark`.
- Produces:
  - `withoutId(ids: string[], id: string): string[]`.
  - `type Suggestion = { kind: 'hit'; hit: SearchHit } | { kind: 'add'; name: string }`; `suggestions(hits: SearchHit[], q: string): Suggestion[]`: hits first; an `add` entry for the trimmed `q` when no hit matches it by `normName`-equivalent compare and `q` is non-empty.
  - `campaignLine(c: Campaign, zone: string): string`: `{n} drops · ends {shortDate}` (`tn('games.drops', n)`).
  - Page: head `{n} games · last scan {rel}`, stamp `GrubDrops only mines these`. Search input (debounced 200 ms) with suggestions; a chosen hit shows `Already in your list, priority {n}.` and a disabled `Added`; 409 on add shows the same line from `body.priority`. Hint `Each extra game adds a little time to every scan.` List hint `Top game mines first. Changes here are permanent.` Rows: up/down buttons, grip drag, art with rank and title, state chip (`Mining`, `Waiting`, `No campaign`), `Remove` (inline confirm `Remove {game}?` with Remove/Cancel), campaigns with platform mark and `campaignLine`; no campaigns: `Nothing live. Last scan {rel}.` Every reorder or remove sends the full order with `setGames(ids)`; on failure refetch and show `Couldn't save. Reloaded the list.` Empty: `No games yet. Add one above.`
- [ ] **Step 1: Write the failing tests**: `withoutId`; `suggestions adds free text when no exact hit` (`[{name:'Rust'}]`, `q 'Rus'` gives hit plus `add 'Rus'`; `q ' rust '` gives the hit only; empty `q` gives no `add`); `campaignLine` (`3 drops · ends Oct 31`, one drop uses `.one`).
- [ ] **Step 2:** Run `npm --prefix web test -- games`. Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4:** Run `npm --prefix web test && npm --prefix web run check`. Expected: PASS.
- [ ] **Step 5: BC.** Full seed. `/games` lists the 4 seeded games in seed order. Add `Valorant`: appears last without a reload. Type `rust`: `Already in your list, priority 1.` and `Added` disabled. Move `Delta Force` up; `reload` keeps the new order. Remove `Apex Legends` with confirm; reload confirms it is gone. Remove all: empty-state line. Route `**/api/games` to `games.json` for the campaign lines and state chips. Screenshot against `r4/games.html` at 1280 and 400 px.
- [ ] **Step 6:** Commit `feat(web): Games search, add, reorder and remove` (controller changelog: `- v2 Games page: search or type a game to add it, reorder priority by button or drag, remove with confirm.`).

---

### Task 9: Rewards (group B)

**Files:**
- Create: `web/src/lib/rewards.ts`
- Modify: `web/src/routes/(app)/rewards/+page.svelte`, locales `rewards.*`
- Test: `web/src/lib/rewards.test.ts`

**Interfaces:**
- Consumes: `api.claims`, `live.runners`, `live.recent` (new claims prepend while on the page), `live.accounts`, `claimStamp`, `etaShort`.
- Produces:
  - `type InProgress = { target: Target; runner: Runner; pct: number; left: number }`; `inProgress(r: Runner[]): InProgress[]` (runners with `current`, sorted `pct` descending, `pct` rounded to whole percent).
  - `queuedCount(r: Runner[]): number` (sum of `up_next` lengths).
  - `PAGE = 10`; `nextQuery(q: ClaimsQuery, loaded: number): ClaimsQuery` (`limit: PAGE, offset: loaded`).
  - Page: head `{n} today · {total} all time`. `In progress`: drop, `{game} · {campaign}`, account, `{m} / {req} min`, `{pct}%`, bar, `~{min} min on {account}`; foot `{n} more drops queued · See Up next` (link `/`). `Claimed`: filter chips Game (from `games`) and Account (from `accounts`), each with an `All` chip; table Reward (image plus name, code when present as copyable text), Game, Account (with platform mark from `live.accounts`; removed accounts show `Removed account`), Claimed (`claimStamp`); `Showing {n} of {total}` and `Load more` (hidden when `n >= total`). Changing a filter resets to offset 0. Empty: `No drops claimed yet.`
- [ ] **Step 1: Write the failing tests**: `inProgress sorts by pct desc` (runners.mining gives 70 then 30, needs_login runner excluded); `left minutes` (42/60 gives 18); `queuedCount` (4 for runners.mining); `nextQuery pages by 10` (`loaded 10` gives `offset 10, limit 10`, filters kept).
- [ ] **Step 2:** Run `npm --prefix web test -- rewards`. Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4:** Run `npm --prefix web test && npm --prefix web run check`. Expected: PASS.
- [ ] **Step 5: BC.** Full seed, live data: empty claimed line, no In progress section. Route `**/api/runners` to `runners.mining.json` and `**/api/claims**` to `claims.json`: In progress shows 2 rows; `Load more` sends `offset=10` (`requests`); clicking Game `Rust` sends `game=Rust&limit=10&offset=0`. Screenshot against `r4/rewards.html` at 1280 and 400 px.
- [ ] **Step 6:** Commit `feat(web): Rewards in progress and claim history` (controller changelog: `- v2 Rewards page: drops in progress with time left, claim history filtered by game or account, 10 at a time.`).

---

### Task 10: Settings frame, General, Notifications, Updates (group B)

**Files:**
- Modify: `internal/api/settings.go` (`zone_effective`), `web/src/routes/(app)/settings/+page.svelte`, locales `settings.*`
- Create: `web/src/routes/(app)/settings/{General,Notifications,Updates,Accounts,Health}.svelte` (Accounts and Health are heading-plus-placeholder stubs filled by Tasks 11 and 12), `web/src/lib/settings.ts`
- Test: `internal/api/settings_test.go`, `web/src/lib/settings.test.ts`

**Interfaces:**
- Consumes: `api.settings`, `api.saveSettings`, `api.notifyTest`, `api.update`, `api.checkUpdate`, `api.logout`, `setLang`, `CopyCommand`, `relTime`, `clock`.
- Produces:
  - Go: `settingsBody.ZoneEffective string \`json:"zone_effective"\``, set from `s.overviewZone().String()`, with `"Local"` reported as `"UTC"`.
  - `SECTIONS = ['general', 'accounts', 'notifications', 'health', 'updates'] as const`; `type Section = typeof SECTIONS[number]`; `sectionFromHash(hash: string): Section` (unknown gives `general`).
  - `zoneOptions(effective: string): { value: string; label: string }[]`: first `{ value: '', label: 'Server default ({effective})' }`, then `Intl.supportedValuesOf('timeZone')`.
  - `timeExample(fmt: '24h' | '12h', zone: string, now: number): string` (`clock` of `now`).
  - Page: sticky sub-nav in `SECTIONS` order (labels General, Accounts, Notifications, Health, Updates), active item follows scroll (IntersectionObserver) and the hash; stamp `Changes save automatically`. Each change calls `saveSettings` with only the changed field and applies the returned body; a failure shows `Couldn't save.` beside the control.
  - General: Language select (`LANGS`), applying `setLang` immediately; Time format 24h/12h with example; Theme Dark/Light setting `data-theme` on `<html>`; Timezone select (`zoneOptions`); `Sign out` button (`api.logout`, then `goto('/login')`).
  - Notifications: Discord: not configured shows a webhook URL input saved on blur or Enter (`discord_webhook`); configured shows `Connected`, `webhook_hint`, `Send test`, `Change`, and feedback `Test message sent to Discord.` or the error (`Discord rejected the message.` for 502, `Add a webhook first.` for 400). Telegram row disabled with `Coming in v1.5.0`. `Notify me when` switches: `Drop claimed` (sub `When a drop is claimed on any account.`), `Account needs login` (`When a session expires.`), `Errors` (`When mining stops on an error.`), `Daily summary` (`Minutes watched and drops claimed, at midnight.`).
  - Updates: `GrubDrops never updates itself. Run the command below on your server.`; `Check for updates` (`checkUpdate`); Current and Latest versions with `checked {rel}`; notes as plain-text bullets; `CopyCommand` with `command`; switch `Check automatically` with sub `Once a day.` (`update_auto`).
- [ ] **Step 1: Write the failing tests**
  - Go `TestSettings_ZoneEffective`: with `Zone` returning `Asia/Shanghai` and stored timezone empty, `GET /api/settings` has `zone_effective:"Asia/Shanghai"`; with `time.Local` named `Local`, it is `"UTC"`.
  - `settings.test.ts`: `sectionFromHash` (`#health` gives `health`, `#nope` and empty give `general`); `SECTIONS order is general first`; `zoneOptions starts with server default` (label `Server default (Asia/Shanghai)`, value `''`, and includes `Europe/Madrid`); `timeExample` both formats.
- [ ] **Step 2:** Run `/home/jandro/.local/go/bin/go test ./internal/api -run TestSettings_ZoneEffective -v` and `npm --prefix web test -- settings`. Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4:** Run `/home/jandro/.local/go/bin/go test ./... && /home/jandro/.local/go/bin/gofmt -l . && npm --prefix web test && npm --prefix web run check`. Expected: PASS.
- [ ] **Step 5: BC.** Full seed. `/settings`: sub-nav order, scroll highlights each section, `/settings#updates` opens at Updates. Switch Language to Español: text changes at once and stays after `reload`; switch back. 12h shows `9:12 AM`-style example; Theme Light flips `data-theme` (eval `document.documentElement.dataset.theme`) and back; Timezone `Asia/Shanghai` persists. Discord: enter `https://discord.com/api/webhooks/1/dev-fake` and blur: shows `Connected` and `https://discord.com/api/webhooks/••••`; route `**/api/settings/notify-test` `--status=204` before clicking `Send test` (never send to Discord). Toggle each notify switch; reload keeps them. Updates: `Check for updates` returns (update check is disabled in the dev miner, so expect the no-latest state); route `**/api/update**` to `update.json` for the notes view. `Sign out` lands on `/login`. Screenshots against `r4/settings.html` (General, Notifications, Updates sections) at 1280 and 400 px.
- [ ] **Step 6:** Commit `feat(web): Settings General, Notifications and Updates` (controller changelog: `- v2 Settings: language, time format, theme and timezone, Discord and notification toggles, update check and command, sign out.`).

---

### Task 11: Settings Accounts (group C, needs 10)

**Files:**
- Create: `web/src/lib/accounts.ts`
- Modify: `web/src/routes/(app)/settings/Accounts.svelte`, locales `accounts.*`
- Test: `web/src/lib/accounts.test.ts`

**Interfaces:**
- Consumes: `api.accounts`, `api.addAccount`, `api.removeAccount`, `api.renameAccount`, `api.twitchStart`, `api.twitchStatus`, `api.kickSession`, `live.accounts`, `live.runners`, `PlatformMark`.
- Produces:
  - `createDeviceFlow(deps: { api: Api; accountId: string; schedule: (fn: () => void, ms: number) => () => void; isNew: boolean })` returning `$state`-free plain object with `state: 'idle' | 'code' | 'done' | 'expired' | 'error'`, `code: DeviceStart | null`, `start(): Promise<void>`, `cancel(): Promise<void>`, and an `onChange(fn)` hook. Polls `twitchStatus` every `POLL_MS = 3000` while `code`; `done` stops polling; `expired` stops and offers `Get a new code` (calls `start()` again); `cancel()` stops polling and, when `isNew`, calls `removeAccount(accountId)`.
  - `accountLine(a: Account, r: Runner | undefined): string`: mining gives `{game} · {drop} on {channel} · {m} / {req} min`; needs login gives `Session expired at {time}. Mining stopped.` when `auth.checked_at` is set, else `Not signed in.`; otherwise the runner state label (`accounts.state.<state>`).
  - `accountStatus(a: Account, r: Runner | undefined): 'mining' | 'signed_out' | 'idle'`.
  - UI: intro `Every account here mines your chosen games.`; `Add account` menu (`Sign in with` Twitch, Kick); `?add=twitch|kick` in the URL opens that flow on load. Twitch: create account (`addAccount('twitch')`), show `Go to {verification_url} and enter {user_code}` with the code large and a `CopyCommand` for the code, `Waiting for Twitch…` while polling, `Signed in.` on done, `The code expired.` plus `Get a new code`. Kick: create account, form with `cookies.txt` textarea and a file input (reads the file as text into the textarea), help `Export cookies for kick.com in Netscape format.`, `Save`; a 400 shows `body.message`; `verified:false` shows `Saved, but Kick didn't accept the session.`. Rows: avatar (image or initials) with platform mark, name (click to rename inline, Enter saves), `accountLine`, status pill `Mining` / `Signed out` / `Idle`, `Log in again` on signed-out rows (Twitch device flow or Kick form for the same account, `isNew: false`), overflow menu `Remove account` with inline confirm `Remove {name}? Its claim history stays.`.
- [ ] **Step 1: Write the failing tests** (fake api, fake schedule): `device flow polls every 3 s until done` (statuses pending, pending, done give three `twitchStatus` calls, delays all 3000, final state `done`, nothing scheduled after); `expired offers a new code` (state `expired`, `start()` again calls `twitchStart` twice in total); `cancel on a new account deletes it` (`removeAccount('a9')` called once; with `isNew:false` not called); `accountLine` mining, needs-login with time, needs-login without check; `accountStatus`.
- [ ] **Step 2:** Run `npm --prefix web test -- accounts`. Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4:** Run `npm --prefix web test && npm --prefix web run check`. Expected: PASS.
- [ ] **Step 5: BC.** Full seed: rows `seed_twitch` and `seed_kick` show `Signed out` and `Log in again`. Add Twitch with `**/api/accounts/*/twitch/device` routed to `device.json` (holds both the start fields and `"status":"pending"`): the code shows and `requests` shows a poll every 3 s; `Cancel` sends `DELETE /api/accounts/<new id>` and the row disappears. Never complete a real device login. Add Kick: paste the line `not a cookies file` and Save: the 400 message from the local miner shows; stop there, never paste real cookies. Rename `seed_kick` to `seed_kick2`; reload keeps it. Remove `seed_twitch` with confirm: row gone via the `accounts` event. `/settings?add=kick#accounts` opens the Kick form. Screenshot against `r4/settings.html#accounts` at 1280 and 400 px.
- [ ] **Step 6:** Commit `feat(web): Settings accounts with Twitch device code and Kick cookies` (controller changelog: `- v2 Settings accounts: add Twitch with a device code or Kick with cookies.txt, log in again, rename and remove.`).

---

### Task 12: Settings Health (group C, needs 10)

**Files:**
- Modify: `internal/api/health.go` (`fields`), `web/src/routes/(app)/settings/Health.svelte`, locales `health.*`
- Create: `web/src/lib/health.ts`
- Test: `internal/api/health_test.go`, `web/src/lib/health.test.ts`

**Interfaces:**
- Consumes: `api.health`, `api.runCanary`, `live.log`, `live.seedLog`, `uptime`, `relTime`, `clock`.
- Produces:
  - Go: `healthEvent.Fields map[string]string \`json:"fields,omitempty"\`` with keys from `healthFieldAllow = []string{"game", "campaign", "benefit_name", "channel", "state", "prev", "err"}`, values `fmt.Sprint` of the ring line field, cut to 200 runes. Other fields never appear.
  - `MSG_KEYS: Record<string, string>`: exact English `msg` literal to `health.msg.<id>` for the most frequent lines (pick 15 to 20 by grepping the `slog` calls in `internal/runner`, `internal/collect`, `internal/api` and `cmd/miner`; claim, progress, state change, scan done, login needed, canary ok/failed, sidecar start/stop). Each localised value may use `{game}`, `{benefit_name}`, `{channel}`, `{state}`, `{prev}`, `{err}`.
  - `eventText(e: HealthEvent): string`: mapped key via `t(key, e.fields ?? {})` when every placeholder has a value, else the raw `msg`; appends ` · {err}` when `fields.err` exists and the key has no `{err}`.
  - `kindLabel(kind: string): string` (`health.kind.<kind>`, unknown gives the raw kind).
  - `isProblem(e: HealthEvent): boolean` (level `WARN`/`ERROR` or kind `error`/`auth`), used to filter SSE `log` events live under the Problems chip; Claims chip = kind `claim`.
  - UI: stats Miner `Running {uptime}`, Kick sidecar `{n} running` (`tn`), Last scan `{rel}`, Canary per platform `OK {rel}` or `Failing: {detail}` with `Run now` (`runCanary`, then refetch after 5 s). Chips All / Problems / Claims with counts. Log in mono: time (`clock`), level dot, account name, `kindLabel`, `eventText`. Foot `Showing the last {n} events` and `Download full log` (link `/api/health/log`, `download`).
- [ ] **Step 1: Write the failing tests**
  - Go `TestHealth_EventFieldsAllowlisted`: ring line with fields `game=Rust`, `benefit_name=Hazmat`, `err=<300 x's>`, `cookie=secret`, `account=a1` gives `fields` with exactly `game`, `benefit_name`, `err` (200 runes), no `cookie` and no `account`.
  - `health.test.ts`: `mapped msg is localised with fields` (es, a mapped msg with `fields.game` renders the es value with `Rust`); `missing field falls back to raw msg`; `unknown msg shows raw`; `err appended once`; `isProblem` (WARN, ERROR, kind auth true; INFO progress false); `every MSG_KEYS literal exists in Go source` (read the repo's `*.go` files outside `web/` and assert each key string appears quoted).
- [ ] **Step 2:** Run `/home/jandro/.local/go/bin/go test ./internal/api -run TestHealth_EventFieldsAllowlisted -v` and `npm --prefix web test -- health`. Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4:** Run `/home/jandro/.local/go/bin/go test ./... && /home/jandro/.local/go/bin/gofmt -l . && npm --prefix web test && npm --prefix web run check`. Expected: PASS.
- [ ] **Step 5: BC.** Full seed: `/settings#health` shows boot and seed events from the local miner (account added, game added, scan lines), counts on chips, Problems filter. Add a game in another tab (`tab-new`, `/games`): a new log line appears live on the Health tab. Switch language to Español: mapped lines are Spanish, unmapped stay English. `Run now` sends `POST /api/health/canary` (202). `Download full log` returns 200 `text/plain` (`requests`). Screenshot against `r4/settings.html#health` at 1280 and 400 px.
- [ ] **Step 6:** Commit `feat(web): Settings health with localised events and canary` (controller changelog: `- v2 Settings health: miner, sidecar, scan and canary status, filtered live event log in your language, full log download.`).

---

### Task 13: Docs for v2

**Files:**
- Modify: `README.md` (line 3 whitelist warning, screenshot alt text at lines 29 and 33, the Kick "Authorize" step near line 156, Development section from Task 2), `docs/translations/README.es.md`, `docs/translations/README.zh-CN.md` (same sections), `docs/DESIGN.md` (rewrite for the v2 tokens, light theme, breakpoints and component list from `app.css`; drop `internal/web/static/css/app.css`), `docs/index.html:102` (chip `HTMX web UI` becomes `SvelteKit web UI`), `AGENTS.md` (Layout gains `web/` and drops `internal/web templates/static`; i18n section says Svelte `t()` plus the locale check; Architecture constraints keep the sidecar rule and state "UI is a SvelteKit static SPA embedded in the Go binary")
- Test: none new; the gate is the grep below.

**Interfaces:**
- Consumes: final page names, the Kick flow (cookies.txt on Settings Accounts), the Twitch device code flow.

- [ ] **Step 1:** Run `grep -nE 'HTMX|html/template|whitelist|Authorize|static/css|templates/' README.md docs/translations/*.md docs/DESIGN.md docs/index.html AGENTS.md`. Expected: hits (the failing state).
- [ ] **Step 2:** Rewrite each hit for v2: chosen games instead of whitelists, the device code and cookies.txt flows, the four pages. Translations say the same facts in their language. No filler copy.
- [ ] **Step 3:** Run the grep again. Expected: no hits except a deliberate historical mention in `docs/CHANGELOG.md` (not grepped). Run `npm --prefix web run check` (unchanged) and `/home/jandro/.local/go/bin/go test ./...`.
- [ ] **Step 4:** Changelog: `- v2 docs: README, translations, DESIGN.md, site and AGENTS.md describe the SvelteKit UI, chosen games and the new login flows.` Commit `docs: describe the v2 web UI`.

---

### Task 14: Full end-to-end walk

**Files:**
- Modify: whatever the walk shows broken (each fix carries its own unit test where the bug is in logic); `docs/screenshots/` (replace `console.png` and `drops-whitelist.png` with `home.png`, `games.png`, `rewards.png`, `settings.png`), `README.md` image paths and translated READMEs

**Interfaces:**
- Consumes: everything.

- [ ] **Step 1: Clean build.** `rm -rf web/node_modules internal/web/dist/* && npm --prefix web ci && npm --prefix web run check && npm --prefix web test && npm --prefix web run build && /home/jandro/.local/go/bin/gofmt -l . && /home/jandro/.local/go/bin/go build ./... && /home/jandro/.local/go/bin/go test ./...`. Expected: all green; `git status --short internal/web/dist` empty.
- [ ] **Step 2: Walk on the embedded build** (BC commands, port 18080, session `grub-dev`, nothing routed unless stated), in this order, checking `console warning` empty after each page:
  1. No seed: `/` goes to `/setup`; short and mismatched passwords rejected; setup lands on Home first-run.
  2. First run: add `Rust` from step 2; Home shows the card; header text `Not set up` until an account exists.
  3. Settings: every section in order; Language es and back; theme light and back; timezone; Discord fake webhook with `notify-test` routed 204; switches persist after reload.
  4. Accounts: Add Twitch with `device.json` routed, cancel deletes it; Add Kick, paste `not a cookies file`, see the 400 message, stop; `seed`-style account rename and remove.
  5. Games: add, duplicate, reorder, remove, empty state.
  6. Health: events, chips, live line on a game add, download, canary run-now.
  7. Rewards: empty state live, then `runners.mining.json` and `claims.json` routed: paging and filters.
  8. Home with fixtures: healthy, nothing live, many accounts; then offline by killing the miner and recovery with `--reuse`; Up next reorder and Reset.
  9. Sign out; `/games` goes to `/login`; `/login?login_error=oidc` banner; log in again.
  10. Phone width 400 px: Home, Games, Rewards, Settings have no horizontal scroll (`eval "document.documentElement.scrollWidth <= innerWidth"` is `true`).
- [ ] **Step 3: Screenshots and compare.** For each page and state, screenshot ours and the r4 mockup into `web/.shots/` and compare them (Read both PNGs). List every difference beyond fonts and the rulings in "Calls made".
- [ ] **Step 4: Loop.** Fix each failure from Steps 2 and 3 (unit test first where it is logic), rebuild, and rerun the affected walk steps, then the whole walk once more. Repeat until a full walk passes with an empty console and no unexplained screenshot difference.
- [ ] **Step 5: Dev loop smoke.** `GRUB_DEV_API=http://127.0.0.1:18080 npm --prefix web run dev -- --port 5173 --strictPort`; log in at `http://localhost:5173/login` and change one setting: proves the proxy and CSRF work from a real browser.
- [ ] **Step 6:** Copy the healthy Home, Games, Rewards and Settings screenshots (1280 px, dark) to `docs/screenshots/`, delete the 1.x images, update README image paths and alt text (and the translations). Run Step 1 again. Changelog: `- v2 UI verified end to end in a browser; README screenshots show the new pages.` Commit `test(web): end-to-end walk fixes and new screenshots`.

---

## Left for plan 5 (and follow-ups)

- Overview `problems` should also list accounts with no session (today only `/api/accounts` `needs_login` shows them), so the header chip covers a never-logged-in account.
- Expose `next_scan_at` from the runner so "Next scan in N min" stops guessing 300 s.
- Health events could carry a stable `code` instead of matching English `msg` literals.
- 1.x data migration (chosen games, claim log, minutes).

## Self-review

1. **Spec coverage:** pages Home/Games/Rewards/Settings (Tasks 5 to 12); Settings sections in spec order (10); Up next per account, session-only, Reset (6); KPIs, card sizes 1-2 large, box art fallback (5, 7); Claimed last 10 grouped (7); version white/blue (5); canary in Health (12); Telegram disabled (10); i18n with equal sets (3); SPA embedded, Go without Node (1, 2); AGENTS.md rule and docs (2, 13). Mockup-only states (first run, nothing live, offline, many accounts) are in Task 7.
2. **Step scan:** every code step names its signature and file; every test step names the test and its values; every UI task has a BC step with commands and page-specific checks.
3. **Type consistency:** `Api` method names (Task 4) are the ones Tasks 5 to 12 consume; `moveItem` (6, reused by 8), `queues`, `nextScanMin`, `dayBucket`, `clock`, `claimStamp`, `createLive` fields `overview/runners/games/accounts/recent/log/unreachable/retryIn/updatedAt` match across tasks; `zone_effective` is added in 10 and typed optional in 4.
4. **Review Focus:** each of the five lines has its named test in Tasks 1, 3, 4 and 6.
5. **Proportion:** signatures, test names, values and copy only; no component bodies.
