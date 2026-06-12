# Helper removal, cookies.txt login, docker-first deploy

**Date:** 2026-06-12
**Status:** Approved
**Base:** master @ 9533fec (PR #12 merged — on-demand Kick browser sidecars are core, NOT touched here)

## Goal

Make GrubDrops leaner and deployment-first, modeled on kick-miner-pro's
packaging (small surface, README that gets you running in minutes, published
docker images). Kill the helper CLI distribution channel entirely; cookie
ingestion becomes "export cookies.txt with a browser extension, paste/upload
into the web UI".

## Out of scope

- chromedp sidecar / `internal/auth/browser` / Twitch `BrowserBackend` /
  proto/buf/gRPC — sidecar is now the credit-earning Kick watch path
  (PR #12); leave all of it alone.
- `internal/` package reshuffle.
- Kick accrual/progress work (separate epic).

## 1. Helper stack removal (full)

Delete:

- `cmd/grubdrops-helper/` (main.go, interactive.go)
- `cmd/grubdrops-helper-gui/`
- `internal/helper/` (helper.go, chromium_extra.go, tests)
- `scripts/build-helper-gui.sh`

API surface:

- Remove `helperIngest` handler and route `POST /helper/accounts/{id}/kick`
  (`internal/api/handlers_login_kick.go`). `persistKickSession` stays (used
  by the authed form).
- Remove `internal/api/handlers_download.go` and route
  `GET /download/helper`.
- Remove `GRUB_HELPER_DIR` from `internal/config`.
- Remove any "download helper" links/copy in `internal/web` templates.

Build/deploy:

- `deploy/Dockerfile.miner`: drop the 4-platform helper cross-compile stage
  and the `/helpers` COPY into the final image.
- `go mod tidy`: expect `browserutils/kooky` and GUI toolkit deps to drop.

Replacement flow for remote friends: log in via SSO (authentik,
`grubdrops-users` group — already live), open their account page, paste
cookies.txt. No unauthenticated ingest endpoint remains.

## 2. cookies.txt Kick login

`login_kick.html` form changes from four cookie fields
(`kick_session`, `xsrf_token`, `cf_clearance`, `session_token`) to:

- one textarea: paste cookies.txt content
- a file input feeding the same server-side field (progressive enhancement;
  textarea is the source of truth)
- the existing channels input (unchanged)

Server side (`internal/api`):

- New parser `parseNetscapeCookies(raw string)`: Netscape cookies.txt format —
  tab-separated 7-field lines, `#` comment lines, `#HttpOnly_` domain prefix
  tolerated, blank lines skipped. Filter to domain `kick.com` / `.kick.com`.
- Extract `kick_session`, `XSRF-TOKEN`, `cf_clearance`, `session_token` into
  the existing `kickCookieForm`; reject with a clear form error if
  `kick_session` or `session_token` is missing.
- `persistKickSession` and downstream (sidecar cookie isolation, utls verify,
  scheduler reload) unchanged.

Form help text names the extension ("Get cookies.txt LOCALLY") and the steps:
log into kick.com, export cookies.txt for the site, paste here.

Twitch unchanged: device-code OAuth stays the only Twitch path.

Tests: table-driven parser tests (happy path, HttpOnly prefix, comments,
wrong domain filtered, missing required cookie) + handler test for the new
form field.

## 3. Docker image publishing

New `.github/workflows/release.yml`: on tag `v*`, build and push linux/amd64
images to ghcr.io:

- `ghcr.io/aalejandrofer/grubdrops:{version,latest}` from
  `deploy/Dockerfile.miner`
- `ghcr.io/aalejandrofer/grubdrops-browser:{version,latest}` from
  `deploy/Dockerfile.browser` (on-demand sidecars require this image present
  on the host)

`scripts/release.sh` stays as the manual/offline fallback. Prod deploy can
later switch from `docker save | ssh load` to pulling from ghcr (runbook
change, not part of this work).

## 4. README rewrite (deployment-first)

Reordered, kick-miner-pro style:

1. badges + one-paragraph what-it-is
2. Quick start: `docker compose up` with ghcr images (incl. docker socket
   mount + browser image note for on-demand sidecars)
3. Account setup: Twitch device-code; Kick = cookies.txt extension export +
   paste (with steps)
4. Configuration: env var table
5. Architecture: miner + on-demand per-account Chrome sidecars over the
   docker socket
6. Features, project layout, credits, disclaimer

All helper/download-helper content removed.

## 5. Repo tidy

- Delete `cmd/kick-encrypt` (self-described one-shot ops tool, superseded by
  the cookies.txt form).
- Keep `cmd/kick-probe` (live ops tool, updated in PR #12).
- Keep `docs/DESIGN.md` + `docs/superpowers/`; prune stale screenshots only
  if clearly orphaned.
- After completion: retire helper-GUI TODO and repo-cleanup TODO memories.

## Error handling

- cookies.txt parse failure → form re-render with specific error (which
  required cookie is missing / nothing for kick.com found).
- Oversized paste: cap request body at a sane limit (e.g. 1 MiB) — cookies.txt
  is a few KiB.

## Testing / verification

- `go build ./...`, `go vet ./...`, `go test ./... -race` green.
- Grep proves no `helper`, `GRUB_HELPER_DIR`, `kooky` references remain
  (outside CHANGELOG/specs).
- Docker image builds locally without the helper stage.
- Manual: paste a real cookies.txt export for a Kick account, watcher
  verifies session.
