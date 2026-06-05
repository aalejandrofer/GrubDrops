# Plan 5: Production Deploy (rdrops.ryuzec.dev)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship the daemon (+ browser sidecar) to the homelab so `https://rdrops.ryuzec.dev` serves the GUI. Images go to ghcr.io; a new homelab stack at `humblewhale/dropsminer/compose.yml` joins `traeky_proxynet`; Traefik labels handle TLS via Cloudflare DNS-01. Deploy via the homelab's standard SSH + `docker compose pull && up -d` flow (or the `homelab-update` TUI).

**Architecture:** Two-repo flow. The miner repo (this one) owns the source + a `scripts/release.sh` helper that tags and pushes images to `ghcr.io/aalejandrofer/dropsminer` and `...-browser`. The homelab repo (sibling at `/Users/jandro/Library/CloudStorage/OneDrive-Personal/00ALX/02Projects/homelab`) gains a new stack directory with `compose.yml` and `.env.example`. Operator pushes images interactively (ghcr requires login), commits the homelab change, then runs `update/homelab-update` (or SSH directly) to pull and reload on 10.10.2.40.

**Tech Stack:** Existing Docker images, `ghcr.io` registry, Traefik (already running on the host), Cloudflare DNS challenge (already configured at the Traefik level), `homelab-update` TUI.

**Out of scope:**
- GitHub Actions / automated CI image push (operator runs `scripts/release.sh` for now)
- Multi-arch image builds (single linux/amd64 for the homelab host)
- Backup / restore tooling for the sqlite database
- TLS rotation handling (Traefik already manages it via Cloudflare DNS-01)
- Migration to Postgres or external storage

---

## File Map

New files in this repo (`DropsMiner`):

| File | Responsibility |
|---|---|
| `scripts/release.sh` | Tag + push miner + browser images to ghcr.io |
| `docs/superpowers/notes/2026-06-04-plan-05-deploy-runbook.md` | Operator deploy walkthrough |

New files in the homelab repo (`humblewhale/dropsminer/`):

| File | Responsibility |
|---|---|
| `compose.yml` | Stack definition with Traefik labels for `rdrops.ryuzec.dev`, joins `traeky_proxynet`, bind-mount to `/home/jandro/localConfig/dropsminer/data` |
| `.env.example` | Documented env vars; operator copies to `.env` on the homelab host with real secrets |
| `CLAUDE.md` | One-paragraph stack note matching the convention of sibling stacks |

---

## Task 1: Release helper script

**Files:**
- Create: `scripts/release.sh`

- [ ] **Step 1: Write the script**

```bash
#!/usr/bin/env bash
# scripts/release.sh — tag and push miner + browser images to ghcr.io.
#
# Usage:
#   scripts/release.sh v0.1.0             # tag images and push
#   scripts/release.sh v0.1.0 --build-only # build but don't push
#
# Prereqs:
#   docker login ghcr.io -u <github-user>  (PAT with packages:write)

set -euo pipefail

TAG="${1:-}"
MODE="${2:-push}"

if [[ -z "$TAG" ]]; then
  echo "usage: $0 <tag> [--build-only]" >&2
  exit 2
fi

REGISTRY="ghcr.io/aalejandrofer"
MINER_IMAGE="$REGISTRY/dropsminer"
BROWSER_IMAGE="$REGISTRY/dropsminer-browser"

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

echo "=== Building $MINER_IMAGE:$TAG ==="
docker build -f deploy/Dockerfile.miner -t "$MINER_IMAGE:$TAG" -t "$MINER_IMAGE:latest" .

echo "=== Building $BROWSER_IMAGE:$TAG ==="
docker build -f deploy/Dockerfile.browser -t "$BROWSER_IMAGE:$TAG" -t "$BROWSER_IMAGE:latest" .

if [[ "$MODE" == "--build-only" ]]; then
  echo "Build-only mode; not pushing."
  exit 0
fi

echo "=== Pushing miner ==="
docker push "$MINER_IMAGE:$TAG"
docker push "$MINER_IMAGE:latest"

echo "=== Pushing browser ==="
docker push "$BROWSER_IMAGE:$TAG"
docker push "$BROWSER_IMAGE:latest"

echo
echo "Released:"
echo "  $MINER_IMAGE:$TAG"
echo "  $BROWSER_IMAGE:$TAG"
echo
echo "Next: update humblewhale/dropsminer/compose.yml image tags, commit, deploy."
```

- [ ] **Step 2: Make executable + verify**

```bash
chmod +x scripts/release.sh
./scripts/release.sh 2>&1 | head -5
```

Expected: usage line.

- [ ] **Step 3: Build-only smoke (no push)**

```bash
./scripts/release.sh v0.0.0-smoke --build-only
docker images | grep -E 'ghcr.io/aalejandrofer/dropsminer'
```

Expected: both `:v0.0.0-smoke` and `:latest` tags appear for both images.

Clean up the smoke tags:

```bash
docker rmi ghcr.io/aalejandrofer/dropsminer:v0.0.0-smoke || true
docker rmi ghcr.io/aalejandrofer/dropsminer-browser:v0.0.0-smoke || true
```

- [ ] **Step 4: Commit**

```bash
git add scripts/release.sh
git commit -m "$(cat <<'EOF'
feat(scripts): release.sh — build + push miner+browser to ghcr.io

Operator runs `scripts/release.sh v0.1.0` after `docker login ghcr.io`
to publish a new version. CI-driven release deferred.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: Homelab stack compose.yml

**Files:**
- Create: `/Users/jandro/Library/CloudStorage/OneDrive-Personal/00ALX/02Projects/homelab/humblewhale/dropsminer/compose.yml`

- [ ] **Step 1: Inspect a reference stack for the exact label/network conventions**

```bash
ls /Users/jandro/Library/CloudStorage/OneDrive-Personal/00ALX/02Projects/homelab/humblewhale/
# pick one that's similar (web service behind Traefik) and read it:
cat /Users/jandro/Library/CloudStorage/OneDrive-Personal/00ALX/02Projects/homelab/humblewhale/<one-with-traefik>/compose.yml 2>/dev/null | head -40
```

Copy the exact Traefik label spelling + network name from there. The homelab uses:
- External network: `traeky_proxynet`
- Cert resolver: `cloudflare` (already configured at Traefik level)
- Entry point: `websecure` (port 443)
- Routing: `Host(\`rdrops.ryuzec.dev\`)`

If the reference stack uses different label keys, mirror those.

- [ ] **Step 2: Write the stack**

```yaml
# humblewhale/dropsminer/compose.yml
services:
  dropsminer:
    image: ghcr.io/aalejandrofer/dropsminer:latest
    container_name: dropsminer
    restart: unless-stopped
    env_file: ./.env
    volumes:
      - /home/jandro/localConfig/dropsminer/data:/data
    networks:
      - traeky_proxynet
    depends_on:
      - dropsminer-browser
    labels:
      - "traefik.enable=true"
      - "traefik.http.routers.dropsminer.rule=Host(`rdrops.ryuzec.dev`)"
      - "traefik.http.routers.dropsminer.entrypoints=websecure"
      - "traefik.http.routers.dropsminer.tls=true"
      - "traefik.http.routers.dropsminer.tls.certresolver=cloudflare"
      - "traefik.http.services.dropsminer.loadbalancer.server.port=8080"
      - "traefik.docker.network=traeky_proxynet"

  dropsminer-browser:
    image: ghcr.io/aalejandrofer/dropsminer-browser:latest
    container_name: dropsminer-browser
    restart: unless-stopped
    networks:
      - traeky_proxynet
    expose:
      - "9090"

networks:
  traeky_proxynet:
    external: true
```

- [ ] **Step 3: Lint the YAML (best-effort)**

```bash
docker compose -f /Users/jandro/Library/CloudStorage/OneDrive-Personal/00ALX/02Projects/homelab/humblewhale/dropsminer/compose.yml config 2>&1 | head -30
```

Will likely fail because `traeky_proxynet` doesn't exist locally — that's expected. The lint is for YAML syntax + label keys. If `docker compose config` complains about an unknown field or malformed YAML, fix it. Network-not-found errors are OK.

> If `docker compose config` insists on resolving the external network, set `external: false` temporarily, re-run, then restore `external: true`.

- [ ] **Step 4: Commit in the homelab repo**

```bash
cd /Users/jandro/Library/CloudStorage/OneDrive-Personal/00ALX/02Projects/homelab
git add humblewhale/dropsminer/compose.yml
git status
```

DO NOT commit yet — Task 3 adds `.env.example` and `CLAUDE.md` so the directory is meaningful.

> The implementer should explicitly NOT push to the homelab remote yet. That's the operator's call once they've reviewed all stack files together.

---

## Task 3: Homelab stack docs + env template

**Files:**
- Create: `/Users/jandro/Library/CloudStorage/OneDrive-Personal/00ALX/02Projects/homelab/humblewhale/dropsminer/.env.example`
- Create: `/Users/jandro/Library/CloudStorage/OneDrive-Personal/00ALX/02Projects/homelab/humblewhale/dropsminer/CLAUDE.md`

- [ ] **Step 1: Write `.env.example`**

```dotenv
# Required — age secret key. Generate ONCE per environment:
#   docker run --rm ghcr.io/anchore/syft:latest age-keygen
# OR locally:
#   go run filippo.io/age/cmd/age-keygen
# Keep the secret line; commit only this template, never the real .env.
MINER_MASTER_KEY=AGE-SECRET-KEY-REPLACE-WITH-REAL-KEY

# HTTP listener — leave 0.0.0.0:8080 unless you change the Traefik label port.
MINER_HTTP_ADDR=0.0.0.0:8080

# SQLite path inside the container. Maps to the bind-mount declared in compose.yml.
MINER_DB_PATH=/data/miner.db

# Discord webhook URL (optional — overridden by per-account / settings page later).
MINER_DISCORD_WEBHOOK=

# Behind Traefik (HTTPS) the session cookie should be Secure-only.
MINER_SECURE_COOKIES=true

# Kick backend — uncomment to enable.
MINER_BROWSER_URL=dropsminer-browser:9090
```

- [ ] **Step 2: Write `CLAUDE.md`**

```markdown
# dropsminer

Self-hosted Twitch + Kick drops miner.

- Web GUI: https://rdrops.ryuzec.dev
- Data: bind-mount at `/home/jandro/localConfig/dropsminer/data` (sqlite + age-encrypted sessions).
- Browser sidecar (`dropsminer-browser`) handles Kick. Twitch works without it.
- Images: `ghcr.io/aalejandrofer/dropsminer` and `...-browser`.
- Releases pushed from the source repo via `scripts/release.sh v0.X.Y`.
- Source: ../../../DropsMiner (separate repo).

To redeploy after a new image push:

```bash
cd ~/deployments/humblewhale/dropsminer   # or wherever this directory lives on the host
docker compose pull
docker compose up -d
docker compose logs -f dropsminer | head -40
```

First-time setup: copy `.env.example` to `.env`, fill in `MINER_MASTER_KEY` with the real value, then `docker compose up -d`. Visit `https://rdrops.ryuzec.dev/setup` to create the admin password.
```

- [ ] **Step 3: Stage the homelab repo files**

```bash
cd /Users/jandro/Library/CloudStorage/OneDrive-Personal/00ALX/02Projects/homelab
git add humblewhale/dropsminer/
git status
```

Expected: 3 files staged (compose.yml from Task 2, .env.example, CLAUDE.md).

- [ ] **Step 4: Commit in the homelab repo**

```bash
git commit -m "$(cat <<'EOF'
feat(dropsminer): add stack for rdrops.ryuzec.dev

New humblewhale stack:
- ghcr.io/aalejandrofer/dropsminer:latest behind Traefik
  with Cloudflare DNS-01 cert at rdrops.ryuzec.dev
- ghcr.io/aalejandrofer/dropsminer-browser:latest for Kick
  via the chromedp sidecar
- Bind-mount /home/jandro/localConfig/dropsminer/data for sqlite

Initial release. See ../../../DropsMiner for source.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

DO NOT push yet — operator pushes once the source repo is also tagged and images are live in ghcr.

---

## Task 4: Operator runbook

**Files:**
- Create: `docs/superpowers/notes/2026-06-04-plan-05-deploy-runbook.md`

- [ ] **Step 1: Write the runbook**

```markdown
# Plan 5 — Production deploy runbook

This runbook is the operator's checklist for shipping a build to https://rdrops.ryuzec.dev. The miner repo and the homelab repo are both committed locally; this walkthrough pushes images, pushes the homelab change, and validates the deploy.

## One-time setup

1. **GHCR login.** Generate a GitHub PAT with `write:packages` scope, then:

   ```bash
   echo "$GHCR_TOKEN" | docker login ghcr.io -u aalejandrofer --password-stdin
   ```

2. **Confirm Traefik + Blocky are configured.** `*.ryuzec.dev` already resolves to 10.10.2.40 via Blocky and Traefik holds a Cloudflare DNS-01 cert. No changes needed.

3. **Create the data dir on the host:**

   ```bash
   ssh jandro@10.10.2.40 'sudo mkdir -p /home/jandro/localConfig/dropsminer/data && sudo chown -R jandro:jandro /home/jandro/localConfig/dropsminer'
   ```

4. **Generate the master key (once):**

   ```bash
   go run filippo.io/age/cmd/age-keygen 2>&1 | awk '/AGE-SECRET-KEY/ {print $NF}'
   ```

   Copy the key — paste into the homelab `.env` in step 6.

## Each deploy

5. **Tag + push images:**

   ```bash
   cd /Users/jandro/Library/CloudStorage/OneDrive-Personal/00ALX/02Projects/DropsMiner
   git tag v0.1.0
   ./scripts/release.sh v0.1.0
   ```

   Expected: both images pushed to ghcr with `v0.1.0` and `latest` tags.

6. **Update homelab .env on the host (first deploy only):**

   ```bash
   ssh jandro@10.10.2.40
   cd ~/deployments/humblewhale/dropsminer   # path depends on homelab clone location
   cp .env.example .env
   # Edit .env — paste MINER_MASTER_KEY from step 4
   nano .env
   exit
   ```

7. **Push the homelab compose change:**

   ```bash
   cd /Users/jandro/Library/CloudStorage/OneDrive-Personal/00ALX/02Projects/homelab
   git push origin master
   ```

8. **Pull + redeploy on the host:**

   Option A — `homelab-update` TUI:

   ```bash
   cd /Users/jandro/Library/CloudStorage/OneDrive-Personal/00ALX/02Projects/homelab/update
   ./homelab-update
   # Select humblewhale → dropsminer → deploy
   ```

   Option B — direct SSH:

   ```bash
   ssh jandro@10.10.2.40 '
     cd ~/deployments/humblewhale &&
     git pull &&
     cd dropsminer &&
     docker compose pull &&
     docker compose up -d &&
     docker compose logs --tail=40 dropsminer
   '
   ```

   Expected logs:
   - `OK   0001_init.sql`
   - `OK   0002_dev_seed.sql`
   - `http listening addr=0.0.0.0:8080`

9. **Smoke test:**

   ```bash
   curl -fsS -I https://rdrops.ryuzec.dev/healthz
   ```

   Expected: `HTTP/2 200`, body `ok`. (-I omits body; drop it to see body too.)

10. **Walk the GUI:** open https://rdrops.ryuzec.dev in a browser. Should land on `/setup` for first-time admin password. Complete and verify dashboard renders. Add a Twitch account and walk the device-code flow.

## Rollback

If a release is bad:

```bash
ssh jandro@10.10.2.40 '
  cd ~/deployments/humblewhale/dropsminer &&
  # pin previous tag in compose.yml manually OR:
  docker compose pull ghcr.io/aalejandrofer/dropsminer:v0.0.X
  docker compose up -d
'
```

For a faster rollback, keep `compose.yml` images pinned to `:v0.1.0` (not `:latest`) so each redeploy is explicit.

## Troubleshooting

- **`*.ryuzec.dev` not resolving from LAN:** Blocky is the DNS authority. Confirm via `dig +short rdrops.ryuzec.dev @10.10.2.40` returns `10.10.2.40`.
- **Traefik 404:** Verify the container joined `traeky_proxynet` (`docker network inspect traeky_proxynet | grep dropsminer`). If absent, the label namespace or network name is wrong.
- **Cert pending forever:** Cloudflare DNS-01 needs Cloudflare API tokens already set in Traefik's env. They're already configured for `*.ryuzec.dev`; no per-stack work.
- **`MINER_MASTER_KEY is required`:** `.env` missing or empty on the host. Re-do step 6.

## Pass criteria

- `https://rdrops.ryuzec.dev/healthz` returns 200 `ok`
- `/setup` form renders, admin creation succeeds
- Adding a Twitch account triggers device-code flow
- `docker compose logs` show no panics within 5 minutes
```

- [ ] **Step 2: Commit**

```bash
cd /Users/jandro/Library/CloudStorage/OneDrive-Personal/00ALX/02Projects/DropsMiner
git add docs/superpowers/notes/2026-06-04-plan-05-deploy-runbook.md
git commit -m "$(cat <<'EOF'
docs(plan-05): operator runbook for first deploy + redeploys

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Done definition

After Task 4:

1. `scripts/release.sh` exists, is executable, builds both images locally.
2. The homelab repo has `humblewhale/dropsminer/{compose.yml, .env.example, CLAUDE.md}` committed locally (not yet pushed).
3. The operator runbook documents the exact `docker login ghcr.io` + `release.sh` + `git push` + SSH deploy sequence.
4. After the operator runs the runbook end-to-end, `https://rdrops.ryuzec.dev/healthz` returns `ok` from the LAN.

## Self-review notes

- All operator-credentialed steps (ghcr login, ssh, git push) live in the runbook, not in tasks. The implementer subagent has no PAT and no ssh key; it would BLOCK on them.
- Image tag policy: each release writes `:vX.Y.Z` + `:latest`. compose.yml uses `:latest` for convenience; switch to pinned tags before any user except you depends on the deploy.
- Cookie security: production deploy MUST set `MINER_SECURE_COOKIES=true` because everything is behind HTTPS via Traefik. The `.env.example` defaults to `true` accordingly.
- Browser sidecar always starts in this stack (no compose profile), because the production deploy presumes Kick is wanted. To disable Kick on the host, set `MINER_BROWSER_URL=` in `.env` and stop the `dropsminer-browser` service.

## Next steps after Plan 5

- Add a daily log-trimming ticker honoring `settings.log_retention_days` (gap noted in Plan 2.5 self-review).
- GitHub Actions CI to push images on tag.
- Per-game `RequiredMinutes` override for Kick once the sidecar parses Kick's drops threshold.
