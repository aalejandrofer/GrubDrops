# SPA Migration — Foundation (sub-project #0)

**Date:** 2026-06-22
**Status:** Design approved, pending spec review
**Decision owner:** Alejandro

## Context

The view layer is `html/template` + HTMX served by the Go binary (`internal/web/templates`, ~2,279 LOC; `internal/web/static/css/app.css`, 3,523 LOC). `internal/api` (~6,410 LOC) renders HTML — there is **no JSON API today**.

A React/Vite SPA port was tried and scrapped 2026-06-06, and `CLAUDE.md` carries a rule against re-proposing a JS port. The user has **knowingly overturned that rule** for this work.

**Stated pain:** runtime feel / interactivity — server-round-trip dashboard, no live state, dead drag-reorder priority stub, no client-side filters. The user wants a **full SPA port, eventually all pages**, keeping the **same style and features**.

This document specifies only **sub-project #0 (Foundation)**. The full port is decomposed below; each later sub-project gets its own spec.

## Locked decisions

- **Framework:** Svelte + Vite, **client-rendered** (no SSR framework). TypeScript.
- **Deploy model preserved:** SPA compiles to static `dist/`, embedded in the Go binary via `go:embed`. Node is **build-time only, never a runtime process**. Single-binary + chromedp-sidecar deploy unchanged.
- **Migration strategy:** strangler, **page by page**, path-routed in one Go binary on one domain.
- **Auth:** keep server-side OIDC/SSO sessions + httpOnly cookies. SPA calls `/api/*` with the existing session cookie. **No client-side token handling.** OIDC/SSO flow untouched.
- **Style:** migrate existing `app.css` wholesale so ported pages look identical.
- **Environment:** **local Mac only** for this migration (full Vite hot-reload dev loop). No staging deploys for this work — this explicitly overrides the usual "verify on staging" rule.

## Decomposition of the full port (roadmap, not all in scope here)

0. **Foundation** ← THIS SPEC. Scaffold, embed wiring, dev proxy, path-routed coexistence, CSS migration, and the **dashboard ported as a static read-only page** (pass 1) as proving ground.
1. **JSON API conventions** — shared response/error shape, cookie-auth middleware on `/api/*`, the pattern every later endpoint follows.
2. **Dashboard live state** (pass 2) — SSE live mining updates + account/campaign modals layered onto the static dashboard from sub-project #0. This is the top pain.
3. **/drops + filters** — client-side filtering / density.
4. **/priority** — drag-reorder (currently dead stub).
5. **Accounts** — list / detail / new / toggle.
6. **Settings** — 431-line page, subnav, health tab.
7. **Auth flows** — login, Twitch device-code, Twitch cookie import, Kick cookie, setup. Ported last (highest regression risk).
8. **i18n + cutover** — en/es/zh port; delete HTMX templates; remove the dual-serve path.

Order is lowest-risk-first; fragile auth/device-code flows come after the harness is proven.

## Foundation design

### Goal

Prove the strangler harness end-to-end by porting the dashboard as a static read-only page, so every later page port is a repeat of a known-good pattern. Static-first keeps harness-proving (embed/routing/auth/CSS) decoupled from the dashboard's live-state complexity, which lands in pass 2.

### Layout

```
web/                       # new — Svelte+Vite + TypeScript source
  src/
    routes/                # ported pages land here, one at a time
    lib/                   # shared components, api client, i18n
    app.css                # migrated from internal/web/static/css/app.css
  index.html
  vite.config.ts
  tsconfig.json
  package.json
internal/web/spa/          # go:embed target for built dist/
internal/web/templates/    # UNCHANGED — HTMX pages stay until ported
internal/web/static/       # UNCHANGED until cutover
```

### Build + embed

- `vite build` → `web/dist/` → output into `internal/web/spa/`, embedded via `go:embed`.
- A `scripts`/Makefile target `build-spa` runs `vite build` before `go build`. Documented in `AGENTS.md`.
- Single Go binary unchanged. No Node at runtime.

### Path-routed coexistence (the strangler switch)

- `internal/api/server.go` gains a `spaRoutes` set, seeded with the proving-ground path only.
- Routing dispatch:
  - path in `spaRoutes` → serve SPA `index.html` (client router takes over);
  - `/assets/*` → embedded SPA dist static files;
  - `/api/*` → JSON handlers (cookie-auth middleware);
  - everything else → existing template handlers (unchanged).
- **Porting a page later = add its path to `spaRoutes` + delete its template.** That is the entire per-page cutover.

### Dev loop (local Mac)

- `vite dev` on `:5173` with proxy: `/api`, `/assets`, and session cookie forwarded to Go on `:8080`.
- SPA hot reload; Go serves real data + real auth. No staging, no deploy.

### Auth

- `/api/*` reuses the existing session-cookie middleware.
- SPA `fetch` sends the cookie (same-origin in prod; dev proxy forwards it).
- Zero client token handling. OIDC/SSO untouched.

### CSS

- Move `app.css` (3,523 lines) to `web/src/app.css` as the global baseline so ported pages render identically.
- Component-scoped styles get added per page in later sub-projects. **Same style preserved** — a hard requirement.

### Proving-ground page — dashboard, static pass (pass 1)

The dashboard (`dashboard.html` + `dashboard_mining_columns.html`, ~300 LOC) is the user's top-pain page, so it ports first. To avoid coupling harness-proving with the hardest interactivity, **pass 1 ports it as a static read-only page**: same layout, same data, rendered once from a JSON snapshot. Live SSE updates and the account/campaign modals are explicitly deferred to sub-project #2 (Dashboard live state).

Pass 1 steps: build the static Svelte dashboard view → add a `GET /api/dashboard` endpoint returning the current snapshot (the same data the template handler computes) → add `/` (or the dashboard path) to `spaRoutes` → `build-spa` + `go build` → run locally → confirm identical render with real data.

Deferred to pass 2 (sub-project #2), **not** in foundation scope:
- SSE / live mining updates,
- account modal, campaign modal,
- any mutation (toggles, claims) from the dashboard.

This validates, in one slice: embed, path routing, dev proxy, cookie auth, one JSON snapshot endpoint, and CSS parity — on the real target page, without the live-state risk.

## Testing

- **Go:** `spaRoutes` dispatch unit test (ported path → SPA `index.html`; other path → template handler). `GET /api/dashboard` handler test (snapshot shape + cookie-auth required).
- **SPA:** Vitest component test for the static dashboard view rendering API snapshot data.
- **Manual (local):** run the binary locally, open the dashboard, confirm static-render parity with the current HTMX page.

## Out of scope (foundation)

- Any page other than the dashboard.
- The dashboard's live behaviour: SSE / live updates, account modal, campaign modal, and any mutation (arrives in sub-project #2, Dashboard live state).
- Deleting the dashboard template / cutting the live route over to the SPA **in prod**. Foundation is local-only; the dashboard template stays the source of truth for prod until **pass 2 (live state)** reaches parity. Locally, pass 1 routes the dashboard to the SPA for development; prod keeps the HTMX dashboard until pass 2 ships. This prevents shipping a regressed (static, no live updates) dashboard to prod.
- i18n in the SPA (stub/passthrough now; real port in sub-project #8).
- CI build-pipeline changes for `build-spa` (local only for now; CI integration tracked when a page ships).

## Risks

- **Two paradigms coexist** (HTMX + SPA) for the duration. Accepted — inherent to strangler; ends at sub-project #8 cutover.
- **CSS drift** as pages port. Mitigated by shipping the whole `app.css` as global baseline up front.
- **CodeQL** still runs on push to master — keep JSON handlers free of injection; no outside-controlled data into any eval. (No chromedp in the web layer, so low exposure.)
- **`build-spa` not yet in CI** — a green local build is the foundation gate; CI wiring deferred until the first page ships to master.
