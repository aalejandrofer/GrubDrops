# AGENTS.md

Guidance for AI coding agents working in this repo. GrubDrops is a self-hosted
Twitch + Kick drops miner: a single Go daemon with an HTMX + `html/template` web
UI, a SQLite store, and on-demand Chrome sidecars for Kick playback.

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
GRUB_MASTER_KEY=$(head -c32 /dev/urandom | base64) \
  GRUB_DB_PATH=./data/miner.db GRUB_SECURE_COOKIES=0 \
  go run ./cmd/miner
```

Open http://localhost:8080 and create the admin login. Only `GRUB_MASTER_KEY`
is required; see the README "Configuration" table for the rest (`GRUB_*` prefix).

## Layout

```
cmd/miner               main daemon
cmd/browser-sidecar     chromedp sidecar binary (Kick IVS playback) — core, not dead code
internal/platform/...   per-platform backends (twitch, kick)
internal/watcher        per-account state machine (watch, mine, claim)
internal/dockerctl      on-demand sidecar start/stop over the docker socket
internal/discovery      catalog scraper
internal/api            HTMX UI handlers (html/template)
internal/web            embedded templates/ + static/ assets (go:embed)
internal/store          SQLite (sqlc + goose), age-encrypted sessions
internal/i18n           locale JSON + the `{{t "key"}}` template helper
proto/                  buf config + gRPC defs for the sidecar — core, not dead code
```

## Architecture constraints (do not violate)

- **The browser sidecar stack is core, not dead code.** Do not delete
  `cmd/browser-sidecar`, `internal/auth/browser`, `internal/dockerctl`, `proto/`
  + buf config, or the Twitch `BrowserBackend`. Kick watch-time (IVS playback
  credit) depends on the on-demand chromedp sidecars. Cleanup passes have
  wrongly flagged these as removable — leave them.
- **Stay Go + `html/template` + HTMX.** A React/Vite port was tried and scrapped.
  Do not re-propose a JS port; build features in templates.

## Platform gotchas

- **Twitch = Android device-code OAuth over direct HTTP.** Never send a
  `Client-Integrity` header — adding it is a self-inflicted integrity wall. Do
  not re-implement logged-out catalog scraping; `/drops/campaigns` while logged
  out is just a login wall.
- **Twitch credit beacon is gated by `HeartbeatInterval`** (1 minute credited
  per beacon), so anything above 60s under-credits. Keep it at 60s. Kick is
  immune.
- **Kick detection/claims** ride a Chrome-TLS-fingerprinted HTTP client (`utls`)
  with no browser; **watch-time** needs the Chrome sidecar (real IVS playback).

## sqlc

- **Never put `?` or parentheses in a `queries/*.sql` comment.** It corrupts
  placeholder rewriting for later queries, producing a cryptic SQL syntax error.

## Internationalization

- User-facing strings go through `internal/i18n` (`{{t "key"}}` in templates,
  `i18n.T(lang, key)` in Go). Add the key to **every** locale file under
  `internal/i18n/locales/` — the locales must keep equal key sets.

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
