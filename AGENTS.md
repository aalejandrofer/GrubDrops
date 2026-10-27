# AGENTS.md

Guidance for AI coding agents working in this repo. GrubDrops is a self-hosted
Twitch + Kick drops miner: a single Go daemon serving a JSON + SSE API and an
embedded SvelteKit SPA, a SQLite store, and on-demand Chrome sidecars for Kick
playback.

## Build, test, format

```bash
go build ./...        # build everything
go test ./...         # run all unit tests — keep green before pushing
gofmt -w .            # CI has a gofmt gate that fails fast; always format Go
docker build -f deploy/Dockerfile.miner .   # build the miner image
```

- The CI gofmt gate fails the build if any Go file is unformatted. Run `gofmt -w`
  before every push.
- Always `go build ./...` **and** `go test ./...` after edits, including changes
  authored by others before you build on them. Don't assume a branch compiles.
- **CodeQL code scanning** (GitHub default setup, no workflow file) runs on every
  push to `master`. Keep it at zero open alerts. When embedding outside-controlled
  data — game names, drop titles, channel names — into a chromedp `Evaluate`
  script, **base64-encode it and decode in-script** (`jsB64JSON` +
  `TextDecoder`); never concatenate raw/JSON values into the eval literal.

## Run locally

```bash
mkdir -p data
GRUB_MASTER_KEY=$(go run ./cmd/miner keygen) \
  GRUB_DB_PATH=./data/miner.db GRUB_SECURE_COOKIES=0 \
  go run ./cmd/miner
```

Build the SPA into `internal/web/dist` (Go serves a 503 "UI not built" page
without it):

```bash
npm --prefix web ci && npm --prefix web run build
```

UI dev loop (Vite proxies `/api` and `/auth` to the miner):

```bash
web/scripts/dev-miner.sh          # temp DB miner on :18080, prints GRUB_DEV_DIR
GRUB_DEV_DIR=<dir> web/scripts/dev-seed.sh   # admin plus sample games and accounts
GRUB_DEV_API=http://127.0.0.1:18080 npm --prefix web run dev
```

Create the admin with `POST /api/setup` (CSRF token from `GET /api/session`)
when not seeding. No env var is required (an unset `GRUB_MASTER_KEY` is
generated into `master.key` next to the DB); see the README
"Configuration" table for the rest (`GRUB_*` prefix).

## Layout

```
cmd/miner               main daemon
cmd/browser-sidecar     chromedp sidecar binary (Kick IVS playback) — core, not dead code
internal/platform/...   per-platform backends (twitch, kick)
internal/plan           pure planner: picks what each account mines next
internal/runner         per-account runner (watch, mine, claim) and the pool
internal/collect        runner hooks into the store (claims, minutes, scans)
internal/summary        daily watch summary at local midnight
internal/dockerctl      on-demand sidecar start/stop over the docker socket
internal/api            JSON API + SSE (`/api/events`) for the SPA
internal/web            embedded SPA build in dist/ (go:embed)
internal/store          SQLite (sqlc + goose), age-encrypted sessions
internal/i18n           locale JSON: Go-sent strings and the Svelte UI
web/                    SvelteKit SPA source (pages in src/routes, components in src/lib)
proto/                  buf config + gRPC defs for the sidecar — core, not dead code
```

## Architecture constraints (do not violate)

- **The browser sidecar stack is core, not dead code.** Do not delete
  `cmd/browser-sidecar`, `internal/auth/browser`, `internal/dockerctl`, `proto/`
  + buf config, or the Twitch `BrowserBackend`. Kick watch-time (IVS playback
  credit) depends on the on-demand chromedp sidecars. Cleanup passes have
  wrongly flagged these as removable — leave them.
- **UI is a SvelteKit static SPA embedded in the Go binary; the Go side serves
  JSON + SSE.**

## Platform gotchas

- **Twitch = Android device-code OAuth over direct HTTP.** Never send a
  `Client-Integrity` header — adding it is a self-inflicted integrity wall. Do
  not re-implement logged-out catalog scraping; `/drops/campaigns` while logged
  out is just a login wall.
- **Twitch credit beacon is gated by `runner.DefaultTick`** (1 minute credited
  per beacon), so anything above 60s under-credits. Keep it at 60s. Kick is
  immune.
- **Kick detection/claims** ride a Chrome-TLS-fingerprinted HTTP client (`utls`)
  with no browser; **watch-time** needs the Chrome sidecar (real IVS playback).

## sqlc

- **Never put `?` or parentheses in a `queries/*.sql` comment.** It corrupts
  placeholder rewriting for later queries, producing a cryptic SQL syntax error.

## Internationalization

- User-facing strings Go sends go through `internal/i18n` (`i18n.T(lang, key)`).
  Add the key to **every** locale file under
  `internal/i18n/locales/` — the locales must keep equal key sets
  (`TestLocaleParity`).
- The Svelte UI reads the same files through `t()`/`tn()`.
  `npm --prefix web run check` fails on unused or unequal keys.

## Releasing

A commit is **not** a release. Day-to-day work lands on `master` freely.

1. **Log every change to `docs/CHANGELOG.md`** under `## [Unreleased]`
   (Added / Changed / Fixed / Removed) as you commit — every bug fix, addition,
   removal, and change, no matter how small. This is the complete running record.
2. **Verify before cutting a version.** Live drops are rarely available to test
   against, so the gate is tiered:
   - **Default gate (no live drop available):** a green build + passing unit tests
     + a green Kick WS transport canary (Heartbeat Health Checker). This is enough
     to tag changes that do **not** touch accrual/claim logic (UI, settings,
     watcher lifecycle, discovery, plumbing). Cover the fix with a unit test —
     that's the durable proof, since most of the time a live drop can't be.
   - **Accrual/claim changes:** when a change touches how watch-time accrues or how
     a drop is claimed (Twitch beacon, Kick WS/IVS watch path, claim flow), confirm
     against a **live drop** before tagging (Twitch and/or Kick, whichever the
     change touches). If no live drop is available, hold the tag in `[Unreleased]`
     until one is — do not tag accrual changes on unit tests alone.
3. Once verified: move `[Unreleased]` to the new version in `docs/CHANGELOG.md`, push
   the `v*` tag (this triggers the ghcr image build), and write the patch notes
   in the GitHub **Releases** tab — **cherry-pick** the user-facing highlights
   from `[Unreleased]`. The release notes are a curated subset, not the whole
   changelog.

A broken miner must never ship under a version tag.

### Release notes format

Never a paragraph wall. Lead with a one-line **highlight**, then group entries
under these emoji section headers, in this order (omit empty sections):

- **🌱 Added** — new features
- **⚙️ Changed** — behaviour / UX changes
- **🐛 Fixed** — bug fixes
- **🔥 Removed** — removed features, flags, or paths

Bold the subject of each bullet; write what changed + why it matters in plain
user-facing language, not commit-speak. The notes are a curated subset of
`[Unreleased]`, not the raw changelog.

Release **title** = bare version only (e.g. `v1.2.5`). Release **notes** are
humanized (run the humanizer skill): plain voice, **no em/en dashes**, but keep
the emoji-section + bold-subject format above.
