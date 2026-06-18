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
internal/api + web      HTMX UI and handlers (html/template)
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

A commit is not a release. Day-to-day work lands on `master` freely. Log every
change to `docs/CHANGELOG.md` under `## [Unreleased]` as you commit. Tag a `v*`
version only after the verification gate in `CLAUDE.md` passes (green build +
tests + transport canary for non-accrual changes; a live drop for accrual/claim
changes). The `v*` tag triggers the ghcr image build.
