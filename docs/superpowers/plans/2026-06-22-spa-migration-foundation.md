# SPA Migration — Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stand up a Svelte+Vite+TypeScript SPA embedded in the Go binary, served alongside the existing HTMX templates via path-based routing, and prove the harness by porting the dashboard as a static read-only page backed by a new JSON endpoint.

**Architecture:** Strangler migration. The Go binary keeps serving `html/template` for every route except those opted into `spaRoutes`. The SPA compiles to static assets embedded via `go:embed` (Node is build-time only, never a runtime process). The dashboard route serves the SPA only when an env-gated flag is on (local dev), so prod keeps the live HTMX dashboard until the live-state pass (sub-project #2) lands. Auth is unchanged: the SPA calls `/api/*` with the existing httpOnly session cookie.

**Tech Stack:** Go 1.26, chi router, `go:embed`; Svelte 5 + Vite + TypeScript, Vitest for component tests. Module path `github.com/aalejandrofer/grubdrops`.

## Global Constraints

- **Single-binary deploy is preserved.** SPA builds to static files embedded via `go:embed`. No Node runtime in the deployed image.
- **Prod guard:** the dashboard template stays prod source-of-truth. SPA serving of the dashboard is gated by env var `GRUB_SPA_DASHBOARD` (default off). Foundation work is local-only; do not deploy to staging or prod.
- **Auth stays server-side.** No client-side tokens. `/api/*` reuses the existing session-cookie + `RequireAdmin` middleware.
- **Keep the same style.** Migrate `internal/web/static/css/app.css` (3,523 lines) verbatim as the SPA global baseline so the ported dashboard renders identically.
- **gofmt gate:** run `gofmt -w .` before every commit. CI fails on unformatted Go.
- **CodeQL zero alerts:** no outside-controlled data into any eval; JSON handlers use `encoding/json`, never string concatenation.
- **Build + test gate:** `go build ./...` and `go test ./...` must pass before each commit.
- Existing template routes and `internal/web/templates/` are **untouched** in this sub-project (no template deleted).

---

## File Structure

**New — SPA source (`web/`):**
- `web/package.json` — Vite + Svelte + TS + Vitest deps and scripts.
- `web/vite.config.ts` — build output to `internal/web/spa/dist`, dev proxy for `/api`, `/assets`, `/static` → `:8080`.
- `web/tsconfig.json`, `web/svelte.config.js` — TS + Svelte config.
- `web/index.html` — SPA entry, references `/assets/*` (built).
- `web/src/main.ts` — mounts the root component.
- `web/src/app.css` — verbatim copy of `internal/web/static/css/app.css`.
- `web/src/lib/api.ts` — typed `fetch` wrapper (credentials: 'include').
- `web/src/lib/types.ts` — TS types mirroring the `/api/dashboard` JSON shape.
- `web/src/routes/Dashboard.svelte` — static read-only dashboard view.
- `web/src/routes/Dashboard.test.ts` — Vitest component test.

**New — Go embed + JSON:**
- `internal/web/spa.go` — `//go:embed spa/dist` + `SPA() fs.FS` accessor.
- `internal/web/spa/dist/` — build output (gitignored except a committed `.gitkeep`; a placeholder `index.html` is committed so `go:embed` always has a target).
- `internal/api/handlers_dashboard_api.go` — `GET /api/dashboard` JSON handler + `spaFileServer` helper.
- `internal/api/handlers_dashboard_api_test.go` — JSON shape + SPA file-server tests.

**Modified:**
- `internal/api/server.go` — register `/api/dashboard`, `/assets/*`, and the env-gated dashboard→SPA dispatch.
- `cmd/miner/main.go` (or wherever `Deps` is populated) — read `GRUB_SPA_DASHBOARD`, set `Deps.SPADashboard`.
- `.gitignore` — ignore `web/node_modules`, `web/dist`, `internal/web/spa/dist/assets`, keep `internal/web/spa/dist/index.html` + `.gitkeep`.
- `AGENTS.md` — add the `build-spa` step; note the SPA migration is in progress (the JS-port rule is intentionally suspended for this work).

---

## Task 1: SPA scaffold + Go embed plumbing

Stand up the Vite+Svelte+TS project and the Go embed accessor, with a committed placeholder so `go:embed` always compiles. No dashboard yet.

**Files:**
- Create: `web/package.json`, `web/vite.config.ts`, `web/tsconfig.json`, `web/svelte.config.js`, `web/index.html`, `web/src/main.ts`, `web/src/App.svelte`
- Create: `internal/web/spa.go`
- Create: `internal/web/spa/dist/index.html` (placeholder), `internal/web/spa/dist/.gitkeep`
- Test: `internal/web/spa_test.go`
- Modify: `.gitignore`

**Interfaces:**
- Produces: `web.SPA() fs.FS` — an `fs.FS` rooted at the embedded `spa/dist` directory, mirroring the existing `web.Static()`.

- [ ] **Step 1: Scaffold the Vite project**

Create `web/package.json`:

```json
{
  "name": "grubdrops-web",
  "private": true,
  "type": "module",
  "scripts": {
    "dev": "vite",
    "build": "vite build",
    "test": "vitest run"
  },
  "devDependencies": {
    "@sveltejs/vite-plugin-svelte": "^4.0.0",
    "@testing-library/svelte": "^5.2.0",
    "jsdom": "^25.0.0",
    "svelte": "^5.0.0",
    "typescript": "^5.6.0",
    "vite": "^5.4.0",
    "vitest": "^2.1.0"
  }
}
```

Create `web/vite.config.ts`:

```ts
import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';

export default defineConfig({
  plugins: [svelte()],
  build: {
    outDir: '../internal/web/spa/dist',
    emptyOutDir: true,
    assetsDir: 'assets',
  },
  server: {
    proxy: {
      '/api': 'http://localhost:8080',
      '/static': 'http://localhost:8080',
    },
  },
  test: {
    environment: 'jsdom',
  },
});
```

Create `web/svelte.config.js`:

```js
import { vitePreprocess } from '@sveltejs/vite-plugin-svelte';
export default { preprocess: vitePreprocess() };
```

Create `web/tsconfig.json`:

```json
{
  "compilerOptions": {
    "target": "ESNext",
    "module": "ESNext",
    "moduleResolution": "bundler",
    "strict": true,
    "skipLibCheck": true,
    "isolatedModules": true,
    "verbatimModuleSyntax": true
  },
  "include": ["src/**/*.ts", "src/**/*.svelte"]
}
```

Create `web/index.html`:

```html
<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>GrubDrops</title>
  </head>
  <body>
    <div id="app"></div>
    <script type="module" src="/src/main.ts"></script>
  </body>
</html>
```

Create `web/src/App.svelte`:

```svelte
<script lang="ts">
</script>

<main>
  <p data-testid="spa-boot">GrubDrops SPA</p>
</main>
```

Create `web/src/main.ts`:

```ts
import { mount } from 'svelte';
import App from './App.svelte';

const app = mount(App, { target: document.getElementById('app')! });
export default app;
```

- [ ] **Step 2: Install deps and run the build once**

Run:
```bash
cd web && npm install && npm run build
```
Expected: `vite build` writes files into `internal/web/spa/dist/` (an `index.html` plus an `assets/` directory). PASS = exit 0.

- [ ] **Step 3: Commit a placeholder dist so `go:embed` always has a target**

The real `dist/assets` is gitignored, but `go:embed spa/dist` fails to compile if the directory is empty. Commit a placeholder `index.html` and a `.gitkeep`.

Create `internal/web/spa/dist/index.html`:
```html
<!doctype html><html><head><meta charset="utf-8"><title>GrubDrops</title></head><body><div id="app"></div></body></html>
```

Create `internal/web/spa/dist/.gitkeep` (empty file).

Append to `.gitignore`:
```
# SPA build
web/node_modules/
internal/web/spa/dist/assets/
internal/web/spa/dist/*.js
internal/web/spa/dist/*.css
```
(Note: `internal/web/spa/dist/index.html` and `.gitkeep` stay tracked.)

- [ ] **Step 4: Write the failing Go embed test**

Create `internal/web/spa_test.go`:

```go
package web

import (
	"io/fs"
	"testing"
)

func TestSPAEmbedsIndex(t *testing.T) {
	f, err := SPA().Open("index.html")
	if err != nil {
		t.Fatalf("SPA() must embed index.html: %v", err)
	}
	defer f.Close()
}

func TestSPAIsFS(t *testing.T) {
	var _ fs.FS = SPA()
}
```

- [ ] **Step 5: Run the test to verify it fails**

Run: `go test ./internal/web/ -run TestSPA -v`
Expected: FAIL — `undefined: SPA`.

- [ ] **Step 6: Implement the embed accessor**

Create `internal/web/spa.go`:

```go
package web

import (
	"embed"
	"io/fs"
)

//go:embed spa/dist
var spaFS embed.FS

// SPA returns an fs.FS rooted at the embedded SPA build output
// (internal/web/spa/dist). Mirrors Static(): callers serve it with
// http.FileServer(http.FS(web.SPA())). The dist directory always
// contains at least a placeholder index.html (committed) so this
// compiles even before `vite build` has run in a fresh checkout.
func SPA() fs.FS {
	sub, err := fs.Sub(spaFS, "spa/dist")
	if err != nil {
		panic(err)
	}
	return sub
}
```

- [ ] **Step 7: Run the test to verify it passes**

Run: `go test ./internal/web/ -run TestSPA -v`
Expected: PASS.

- [ ] **Step 8: Format and commit**

```bash
gofmt -w .
git add web/package.json web/vite.config.ts web/tsconfig.json web/svelte.config.js web/index.html web/src/main.ts web/src/App.svelte web/package-lock.json internal/web/spa.go internal/web/spa_test.go internal/web/spa/dist/index.html internal/web/spa/dist/.gitkeep .gitignore
git commit -m "feat(spa): scaffold Svelte+Vite SPA and go:embed accessor"
```

---

## Task 2: Path-routed coexistence (SPA file server + env-gated dashboard dispatch)

Serve the SPA's built assets at `/assets/*`, and serve the SPA `index.html` for the dashboard route **only when `GRUB_SPA_DASHBOARD` is on**. Otherwise the existing template dashboard renders unchanged.

**Files:**
- Create: `internal/api/handlers_dashboard_api.go` (the `spaFileServer` helper lands here; the JSON handler is added in Task 3)
- Modify: `internal/api/server.go` (register routes + dispatch)
- Modify: `cmd/miner/main.go` (read env, set `Deps.SPADashboard`)
- Test: `internal/api/handlers_dashboard_api_test.go`

**Interfaces:**
- Consumes: `web.SPA() fs.FS` (Task 1).
- Produces:
  - `func spaFileServer() http.Handler` — serves embedded SPA files (used for `/assets/*`).
  - `func spaIndex(w http.ResponseWriter, r *http.Request)` — writes the SPA `index.html` body with `Content-Type: text/html`.
  - `Deps.SPADashboard bool` — when true, `GET /` serves the SPA instead of the template dashboard.

- [ ] **Step 1: Write the failing test for the SPA file server + index**

Create `internal/api/handlers_dashboard_api_test.go`:

```go
package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSPAIndexServesHTML(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	spaIndex(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "text/html")
	assert.Contains(t, rec.Body.String(), `id="app"`)
}

func TestSPAFileServerServesIndex(t *testing.T) {
	h := spaFileServer()
	req := httptest.NewRequest(http.MethodGet, "/index.html", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.True(t, strings.Contains(rec.Body.String(), "<html"))
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/api/ -run TestSPA -v`
Expected: FAIL — `undefined: spaIndex`, `undefined: spaFileServer`.

- [ ] **Step 3: Implement `spaIndex` and `spaFileServer`**

Create `internal/api/handlers_dashboard_api.go`:

```go
package api

import (
	"io"
	"net/http"

	"github.com/aalejandrofer/grubdrops/internal/web"
)

// spaFileServer serves the embedded SPA build output (JS/CSS under
// /assets, plus index.html). Mounted at /assets/* in the router. CSS/JS
// filenames are content-hashed by Vite, so they cache aggressively.
func spaFileServer() http.Handler {
	return http.FileServer(http.FS(web.SPA()))
}

// spaIndex writes the SPA shell (index.html). The client-side router
// then renders the requested view. Used for routes opted into the SPA.
func spaIndex(w http.ResponseWriter, _ *http.Request) {
	f, err := web.SPA().Open("index.html")
	if err != nil {
		http.Error(w, "spa index missing", http.StatusInternalServerError)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = io.Copy(w, f)
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/api/ -run TestSPA -v`
Expected: PASS.

- [ ] **Step 5: Add the `SPADashboard` field to `Deps`**

In `internal/api/server.go`, add to the `Deps` struct (after `StartTime time.Time`):

```go
	// SPADashboard, when true, serves the Svelte SPA at "/" instead of
	// the html/template dashboard. Gated by GRUB_SPA_DASHBOARD (default
	// off) so prod keeps the live HTMX dashboard until the SPA live-state
	// pass lands. Local-dev only for now.
	SPADashboard bool
```

- [ ] **Step 6: Register `/assets/*` and the gated dashboard dispatch**

In `internal/api/server.go`, register the assets route next to the existing `/static/*` line (after line 130):

```go
	// SPA build output (content-hashed JS/CSS). Served at /assets/* to
	// match Vite's default assetsDir.
	r.Handle("/assets/*", spaFileServer())
```

Then change the authed dashboard route. Replace:

```go
	authed.Get("/", dash.page)
```

with:

```go
	if d.SPADashboard {
		authed.Get("/", spaIndex)
	} else {
		authed.Get("/", dash.page)
	}
```

- [ ] **Step 7: Wire the env flag in cmd/miner**

In `cmd/miner/main.go`, where `Deps{...}` is constructed, add the field (find the struct literal passed to `api.NewRouter`):

```go
		SPADashboard: os.Getenv("GRUB_SPA_DASHBOARD") == "1",
```

Ensure `os` is imported (it almost certainly already is).

- [ ] **Step 8: Build, test, format**

Run:
```bash
go build ./... && go test ./internal/api/ ./internal/web/ -v
```
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
gofmt -w .
git add internal/api/handlers_dashboard_api.go internal/api/handlers_dashboard_api_test.go internal/api/server.go cmd/miner/main.go
git commit -m "feat(spa): serve /assets and env-gated SPA dashboard route"
```

---

## Task 3: `GET /api/dashboard` JSON endpoint

Expose the dashboard snapshot as JSON, reusing the existing `dashboardDeps.collectPage`. Auth via the existing `RequireAdmin` middleware (it's registered inside the authed router).

**Files:**
- Modify: `internal/api/handlers_dashboard_api.go` (add the handler + method)
- Modify: `internal/api/server.go` (register `/api/dashboard`)
- Test: `internal/api/handlers_dashboard_api_test.go` (add JSON test)

**Interfaces:**
- Consumes: `dashboardDeps.collectPage(r *http.Request) dashPage` (existing, `internal/api/handlers_dashboard.go:257`).
- Produces: `func (d dashboardDeps) apiPage(w http.ResponseWriter, r *http.Request)` — writes `dashPage` as JSON with `Content-Type: application/json`. The JSON keys are the exported Go field names of `dashPage` and its nested types (PascalCase, e.g. `Tele.ClaimsTotal`, `Mining.Twitch[].State`).

- [ ] **Step 1: Write the failing JSON handler test**

Add to `internal/api/handlers_dashboard_api_test.go`:

```go
func TestAPIDashboardReturnsJSON(t *testing.T) {
	// collectPage tolerates nil scheduler/store; a zero-value
	// dashboardDeps yields an empty-but-valid snapshot.
	d := dashboardDeps{}
	req := httptest.NewRequest(http.MethodGet, "/api/dashboard", nil)
	rec := httptest.NewRecorder()
	d.apiPage(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "application/json")
	// Body is a JSON object with the dashPage top-level keys.
	body := rec.Body.String()
	assert.True(t, strings.HasPrefix(strings.TrimSpace(body), "{"))
	assert.Contains(t, body, `"Tele"`)
	assert.Contains(t, body, `"Mining"`)
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/api/ -run TestAPIDashboard -v`
Expected: FAIL — `d.apiPage undefined`.

- [ ] **Step 3: Implement `apiPage`**

Add to `internal/api/handlers_dashboard_api.go` (add `encoding/json` to imports):

```go
// apiPage serves the dashboard snapshot as JSON for the SPA. It reuses
// the same collectPage projection the html/template dashboard renders,
// so the SPA and the legacy page show identical data. JSON keys are the
// exported Go field names of dashPage (PascalCase).
func (d dashboardDeps) apiPage(w http.ResponseWriter, r *http.Request) {
	page := d.collectPage(r)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(page); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
```

Update the import block in `handlers_dashboard_api.go`:

```go
import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/aalejandrofer/grubdrops/internal/web"
)
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/api/ -run TestAPIDashboard -v`
Expected: PASS.

- [ ] **Step 5: Register the route**

In `internal/api/server.go`, inside the authed router (next to the other `authed.Get("/dashboard/...")` lines, around line 225):

```go
	authed.Get("/api/dashboard", dash.apiPage)
```

(Registering inside `authed` means `RequireAdmin` already gates it — an unauthenticated request is redirected to `/login`, same as every other authed route. No new auth code.)

- [ ] **Step 6: Build, test, format, commit**

Run:
```bash
go build ./... && go test ./internal/api/ -v
```
Expected: PASS.

```bash
gofmt -w .
git add internal/api/handlers_dashboard_api.go internal/api/handlers_dashboard_api_test.go internal/api/server.go
git commit -m "feat(spa): add GET /api/dashboard JSON snapshot endpoint"
```

---

## Task 4: Static dashboard Svelte view + CSS migration

Build the read-only dashboard view in Svelte, fetching `/api/dashboard` and rendering the telemetry tiles + mining columns, styled with the migrated `app.css`. Vitest covers the render.

**Files:**
- Create: `web/src/app.css` (verbatim copy of `internal/web/static/css/app.css`)
- Create: `web/src/lib/types.ts`, `web/src/lib/api.ts`
- Create: `web/src/routes/Dashboard.svelte`
- Modify: `web/src/App.svelte` (render Dashboard), `web/src/main.ts` (import css)
- Test: `web/src/routes/Dashboard.test.ts`

**Interfaces:**
- Consumes: `GET /api/dashboard` JSON (Task 3) — top-level keys `Tele`, `Mining`, `ActiveCamps`, `UpdatedAt`, `Uptime`, `Alerts`.
- Produces: a static `Dashboard.svelte` that takes an optional `snapshot` prop (for tests) and otherwise fetches on mount.

- [ ] **Step 1: Migrate the stylesheet**

Run:
```bash
cp internal/web/static/css/app.css web/src/app.css
```
This is a verbatim copy — same style, no edits. (The legacy `static/css/app.css` stays in place; the HTMX pages still use it.)

- [ ] **Step 2: Define the API types**

Create `web/src/lib/types.ts` (only the fields the static view reads — extend later passes as needed):

```ts
export interface Telemetry {
  WatchTimeTotal: string;
  ClaimsTotal: number;
  ClaimsToday: number;
  ActiveCamps: number;
  Completed: number;
  TotalDrops: number;
  NextClaimETA: string;
  NextClaimName: string;
}

export interface MineCard {
  ID: string;
  Name: string;
  Platform: string;
  State: string;
  StateSub: string;
  Channel: string;
  DropName: string;
  DropPercent: number;
  Enabled: boolean;
}

export interface MiningColumns {
  Twitch: MineCard[] | null;
  Kick: MineCard[] | null;
  KickWatchMode: string;
}

export interface DashboardSnapshot {
  Tele: Telemetry;
  Mining: MiningColumns;
  UpdatedAt: string;
  Uptime: string;
}
```

- [ ] **Step 3: Write the typed fetch wrapper**

Create `web/src/lib/api.ts`:

```ts
import type { DashboardSnapshot } from './types';

export async function fetchDashboard(): Promise<DashboardSnapshot> {
  const res = await fetch('/api/dashboard', { credentials: 'include' });
  if (!res.ok) {
    throw new Error(`/api/dashboard returned ${res.status}`);
  }
  return res.json() as Promise<DashboardSnapshot>;
}
```

- [ ] **Step 4: Write the failing component test**

Create `web/src/routes/Dashboard.test.ts`:

```ts
import { render, screen } from '@testing-library/svelte';
import { expect, test } from 'vitest';
import Dashboard from './Dashboard.svelte';
import type { DashboardSnapshot } from '../lib/types';

const snapshot: DashboardSnapshot = {
  Tele: {
    WatchTimeTotal: '12:34',
    ClaimsTotal: 7,
    ClaimsToday: 2,
    ActiveCamps: 3,
    Completed: 1,
    TotalDrops: 5,
    NextClaimETA: '00:13',
    NextClaimName: 'Wolf Helmet',
  },
  Mining: {
    Twitch: [
      { ID: 'a1', Name: 'acc-one', Platform: 'twitch', State: 'watching', StateSub: 'live', Channel: 'somechan', DropName: 'Helmet', DropPercent: 42, Enabled: true },
    ],
    Kick: null,
    KickWatchMode: 'browser',
  },
  UpdatedAt: '1.2s ago',
  Uptime: '17h 42m',
};

test('renders telemetry tiles from the snapshot', () => {
  render(Dashboard, { props: { snapshot } });
  expect(screen.getByText('Wolf Helmet')).toBeTruthy();
  expect(screen.getByText('acc-one')).toBeTruthy();
});
```

- [ ] **Step 5: Run the test to verify it fails**

Run:
```bash
cd web && npm test
```
Expected: FAIL — `Dashboard.svelte` does not exist.

- [ ] **Step 6: Implement the static dashboard view**

Create `web/src/routes/Dashboard.svelte`:

```svelte
<script lang="ts">
  import { onMount } from 'svelte';
  import { fetchDashboard } from '../lib/api';
  import type { DashboardSnapshot } from '../lib/types';

  let { snapshot = $bindable<DashboardSnapshot | null>(null) } = $props();
  let error = $state<string | null>(null);

  onMount(async () => {
    if (snapshot) return; // test-injected
    try {
      snapshot = await fetchDashboard();
    } catch (e) {
      error = (e as Error).message;
    }
  });
</script>

{#if error}
  <p class="error">{error}</p>
{:else if snapshot}
  <section class="dash-telemetry">
    <div class="tile"><span class="label">Watch time</span><span class="value">{snapshot.Tele.WatchTimeTotal}</span></div>
    <div class="tile"><span class="label">Claims total</span><span class="value">{snapshot.Tele.ClaimsTotal}</span></div>
    <div class="tile"><span class="label">Active campaigns</span><span class="value">{snapshot.Tele.ActiveCamps}</span></div>
    <div class="tile"><span class="label">Next claim</span><span class="value">{snapshot.Tele.NextClaimETA} {snapshot.Tele.NextClaimName}</span></div>
  </section>

  <section class="mining-columns">
    <div class="col twitch">
      <h3>TWITCH</h3>
      {#each snapshot.Mining.Twitch ?? [] as card (card.ID)}
        <article class="mine-card">
          <span class="name">{card.Name}</span>
          <span class="state">{card.State}</span>
          <span class="channel">{card.Channel}</span>
          <span class="drop">{card.DropName} {card.DropPercent}%</span>
        </article>
      {/each}
    </div>
    <div class="col kick">
      <h3>KICK · {snapshot.Mining.KickWatchMode}</h3>
      {#each snapshot.Mining.Kick ?? [] as card (card.ID)}
        <article class="mine-card">
          <span class="name">{card.Name}</span>
          <span class="state">{card.State}</span>
          <span class="channel">{card.Channel}</span>
          <span class="drop">{card.DropName} {card.DropPercent}%</span>
        </article>
      {/each}
    </div>
  </section>

  <footer class="dash-footer">updated {snapshot.UpdatedAt} · uptime {snapshot.Uptime}</footer>
{:else}
  <p class="loading">Loading…</p>
{/if}
```

- [ ] **Step 7: Wire App + global CSS**

Replace `web/src/App.svelte`:

```svelte
<script lang="ts">
  import Dashboard from './routes/Dashboard.svelte';
</script>

<Dashboard />
```

Prepend the CSS import to `web/src/main.ts`:

```ts
import './app.css';
import { mount } from 'svelte';
import App from './App.svelte';

const app = mount(App, { target: document.getElementById('app')! });
export default app;
```

- [ ] **Step 8: Run the test to verify it passes**

Run:
```bash
cd web && npm test
```
Expected: PASS — both telemetry and mining card text found.

- [ ] **Step 9: Build the SPA**

Run:
```bash
cd web && npm run build
```
Expected: exit 0; `internal/web/spa/dist/` updated with hashed assets.

- [ ] **Step 10: Commit**

```bash
cd ..
git add web/src/app.css web/src/lib/types.ts web/src/lib/api.ts web/src/routes/Dashboard.svelte web/src/routes/Dashboard.test.ts web/src/App.svelte web/src/main.ts
git commit -m "feat(spa): static dashboard view + app.css migration + vitest"
```

---

## Task 5: Dev loop, docs, and local parity verification

Document the build step, prove the whole harness runs locally end-to-end, and confirm visual parity with the HTMX dashboard.

**Files:**
- Modify: `AGENTS.md` (build-spa step + migration note)

**Interfaces:**
- Consumes: everything from Tasks 1–4.

- [ ] **Step 1: Document the build + dev loop in AGENTS.md**

In `AGENTS.md`, under "Build, test, format", add:

```markdown
### SPA (frontend migration in progress)

The Svelte+Vite SPA lives in `web/` and compiles into the Go binary via
`go:embed` (`internal/web/spa/dist`). Node is build-time only.

```bash
cd web && npm install        # once
cd web && npm run build      # build SPA into internal/web/spa/dist (before `go build`)
cd web && npm test           # Vitest component tests
cd web && npm run dev        # hot-reload dev server on :5173, proxies /api + /static to :8080
```

Local dev loop: run the Go binary on :8080 (see "Run locally"), then `npm run dev`
in `web/` and open http://localhost:5173. To serve the SPA dashboard from the Go
binary itself, set `GRUB_SPA_DASHBOARD=1`.

**Migration status:** a strangler port from html/template+HTMX to the SPA is in
progress (spec: `docs/superpowers/specs/2026-06-22-spa-migration-foundation-design.md`).
The "stay Go/HTMX, no JS port" rule is intentionally suspended for this work.
```

- [ ] **Step 2: Full build + test gate**

Run:
```bash
cd web && npm run build && npm test && cd .. && gofmt -w . && go build ./... && go test ./...
```
Expected: all PASS.

- [ ] **Step 3: Manual local parity check**

Run the binary locally with the flag on:
```bash
mkdir -p data
GRUB_SPA_DASHBOARD=1 GRUB_MASTER_KEY=$(head -c32 /dev/urandom | base64) \
  GRUB_DB_PATH=./data/miner.db GRUB_SECURE_COOKIES=0 \
  go run ./cmd/miner
```
Then in a browser: log in, open `/` (SPA dashboard). Confirm:
- telemetry tiles show the same numbers as the HTMX dashboard (open `/` with `GRUB_SPA_DASHBOARD` unset in a second run to compare),
- mining columns list the same accounts/states,
- styling matches (same fonts, colors, layout from the migrated `app.css`).

Record the result. This is the foundation gate (a live drop is not required — no accrual/claim code changed).

- [ ] **Step 4: Commit docs**

```bash
git add AGENTS.md
git commit -m "docs(agents): document SPA build + dev loop and migration status"
```

---

## Self-Review

**Spec coverage:**
- Vite+Svelte+TS scaffold → Task 1. ✓
- `go:embed` single-binary preservation → Task 1 (`spa.go`), placeholder dist. ✓
- Path-routed coexistence (`spaRoutes`/assets) → Task 2. ✓
- Prod guard (env-gated dashboard, no template deleted) → Task 2 (`GRUB_SPA_DASHBOARD`, default off). ✓
- Cookie auth, no client tokens → Task 3 (route inside `authed`, `RequireAdmin`). ✓
- JSON snapshot endpoint reusing `collectPage` → Task 3. ✓
- CSS migrated verbatim → Task 4 (Step 1). ✓
- Dashboard ported static read-only; SSE/modals/mutations deferred → Task 4 renders read-only only. ✓
- Dev proxy (`/api`, `/static`) → Task 1 `vite.config.ts`; documented Task 5. ✓
- Tests: Go embed, SPA file server/index, JSON shape, Vitest component → Tasks 1–4. ✓
- Local-only, no staging → stated in Global Constraints; manual check is local (Task 5 Step 3). ✓

**Placeholder scan:** No TBD/TODO; every code step has concrete content. ✓

**Type consistency:** `web.SPA()` used identically in Tasks 1–3. `dashboardDeps.collectPage`/`apiPage` names consistent Task 3. `DashboardSnapshot`/`Telemetry`/`MineCard`/`MiningColumns` consistent across `types.ts`, `api.ts`, `Dashboard.svelte`, test. JSON keys are PascalCase (no json tags on `dashPage`) — types.ts matches (`Tele`, `Mining`, `ClaimsTotal`, etc.). ✓

**Note on `/assets` proxy:** the Vite dev server (:5173) serves `/assets` itself during `npm run dev` (it owns the built/served assets), so only `/api` and `/static` are proxied to Go — correct in `vite.config.ts`.
