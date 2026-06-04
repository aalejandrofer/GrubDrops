# Plan 5 — Production deploy runbook

Operator's checklist for shipping a build to https://rdrops.ryuzec.dev.

## One-time setup

1. **GHCR login.** Generate a GitHub PAT with `write:packages` scope, then:

   ```bash
   echo "$GHCR_TOKEN" | docker login ghcr.io -u aalejandrofer --password-stdin
   ```

2. **Confirm Traefik + Blocky are configured.** `*.ryuzec.dev` already resolves to 10.10.2.40 via Blocky, and Traefik holds a Let's Encrypt DNS-01 cert via the `letsencrypt-dns` resolver. No per-stack work needed.

3. **Create the data dir on the host:**

   ```bash
   ssh jandro@10.10.2.40 'sudo mkdir -p /home/jandro/localConfig/rust-drops-miner/data && sudo chown -R jandro:jandro /home/jandro/localConfig/rust-drops-miner'
   ```

4. **Generate the master key (once):**

   ```bash
   go run filippo.io/age/cmd/age-keygen 2>&1 | awk '/AGE-SECRET-KEY/ {print $NF}'
   ```

   Copy the key — paste into the homelab `.env` in step 6.

## Each deploy

5. **Tag + push images:**

   ```bash
   cd /Users/jandro/Library/CloudStorage/OneDrive-Personal/00ALX/02Projects/RustDropsMiner
   git tag v0.1.0
   ./scripts/release.sh v0.1.0
   ```

   Expected: both images pushed to ghcr.io with `v0.1.0` and `latest` tags.

6. **First deploy only — create .env on the host:**

   ```bash
   ssh jandro@10.10.2.40
   cd ~/deployments/humblewhale/rust-drops-miner   # path depends on homelab clone location
   cp .env.example .env
   # Edit .env — paste MINER_MASTER_KEY from step 4
   nano .env
   exit
   ```

   `DOMAIN` is inherited from `humblewhale/.env` (already set to `ryuzec.dev`); no per-stack override needed.

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
   # Select humblewhale → rust-drops-miner → deploy
   ```

   Option B — direct SSH:

   ```bash
   ssh jandro@10.10.2.40 '
     cd ~/deployments/humblewhale &&
     git pull &&
     cd rust-drops-miner &&
     docker compose pull &&
     docker compose up -d &&
     docker compose logs --tail=40 rust-drops-miner
   '
   ```

   Expected logs:
   - `OK   0001_init.sql`
   - `OK   0002_dev_seed.sql`
   - `http listening addr=0.0.0.0:8080`

9. **Smoke test:**

   ```bash
   curl -fsS https://rdrops.ryuzec.dev/healthz
   ```

   Expected: `ok`.

10. **Walk the GUI:** open https://rdrops.ryuzec.dev in a browser. Should land on `/setup` for first-time admin password. Complete and verify dashboard renders. Add a Twitch account and walk the device-code flow.

## Rollback

If a release is bad:

```bash
ssh jandro@10.10.2.40 '
  cd ~/deployments/humblewhale/rust-drops-miner &&
  # Edit compose.yml to pin the previous tag, then:
  docker compose pull &&
  docker compose up -d
'
```

For faster rollback, pin `compose.yml` images to specific tags (`:v0.1.0`) rather than `:latest` so each redeploy is explicit.

## Troubleshooting

- **`*.ryuzec.dev` not resolving from LAN:** Blocky is the DNS authority. Confirm via `dig +short rdrops.ryuzec.dev @10.10.2.40` returns `10.10.2.40`.
- **Traefik 404:** Verify the container joined `traeky_proxynet` (`docker network inspect traeky_proxynet | grep rust-drops-miner`). If absent, the label namespace or network name is wrong.
- **Cert pending forever:** Cloudflare DNS-01 needs API tokens already set at the Traefik level (under the `letsencrypt-dns` resolver). They're already configured for `*.ryuzec.dev`.
- **`MINER_MASTER_KEY is required`:** `.env` missing or empty on the host. Re-do step 6.
- **`docker compose pull` 401s on ghcr:** the host machine needs `docker login ghcr.io` too if the images are private. Run the same login command from step 1 on the host.

## Pass criteria

- `https://rdrops.ryuzec.dev/healthz` returns 200 `ok`
- `/setup` form renders, admin creation succeeds
- Adding a Twitch account triggers device-code flow
- `docker compose logs` show no panics within 5 minutes
