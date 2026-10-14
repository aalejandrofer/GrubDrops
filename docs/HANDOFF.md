# New-computer handoff

Updated 2026-10-14. Read this file, AGENTS.md and CLAUDE.md before continuing.
This is portable project memory, consolidated from local assistant notes. Historical
release/deployment statements below were recorded in September and must be checked
against Git and the running instance; they are not fresh verification.

## Resume on a new laptop

```bash
git clone https://github.com/aalejandrofer/GrubDrops.git
cd GrubDrops
git fetch origin --prune
git branch -a
git switch feat/pipeline-v2
go build ./...
go test ./...
```

The old laptop's main checkout was on `feat/startup-preflight`; active pipeline
work lived in `.claude/worktrees/pipeline-v2` on `feat/pipeline-v2`. Do not assume
master or the startup branch contains that work. Check remote branches and their
latest commits before selecting the starting point. Old worktree paths, ignored
SDD ledgers, assistant sessions and local memories do not transfer with a clone.
See `docs/PROJECT_MEMORY.md` for their useful consolidated context.

Install Git and Go matching `go.mod` (currently Go 1.26.2). Docker is needed for
container builds and Kick IVS sidecars. GitHub authentication and any deployment
SSH/Tailscale access must be configured separately on the new computer.

For a fresh local database:

```bash
mkdir -p data
export GRUB_MASTER_KEY="$(go run ./cmd/miner keygen)"
GRUB_DB_PATH=./data/miner.db GRUB_SECURE_COOKIES=0 go run ./cmd/miner
```

Open http://localhost:8080 and create the admin login. The key must be an age
X25519 identity; random base64 is invalid. Preserve the key if preserving the DB.
Runtime databases, cookies, tokens, `.env`, SSH keys and deployment credentials
must transfer separately and stay out of Git. Use Settings → Health's database
snapshot for a consistent backup; retain its matching original master key.

## First directions to the next agent

> Read AGENTS.md, CLAUDE.md, docs/HANDOFF.md and docs/PROJECT_MEMORY.md. Fetch
> origin and inspect master plus feat/pipeline-v2, including uncommitted changes.
> Report which v2 tasks are implemented and which live verification remains.
> Continue non-UI work toward the planned 1.4.5 release; keep 2.0.0 UI work parked.
> Stay Go/HTMX, preserve the browser sidecars, and do not tag until the live gate
> passes. Keep follow-ups in tracked notes rather than opening public issues.

## Working preferences

- Act autonomously within the requested task and keep progress visible. Use a
  visible task list for multi-step work, exactly one item in progress at a time.
- Do not publish GitHub comments, reviews or new issues without explicit approval.
  This handoff request authorizes its Git commit and push.
- Log committed changes under CHANGELOG `[Unreleased]`; run gofmt, build and tests
  before pushing. A commit is not a release.
- Keep these tracked notes current at the end of future sessions so the project
  no longer depends on one laptop's assistant memory.
