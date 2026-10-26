# v2 prod upgrade runbook

Upgrades prod from the 1.x image to v2. Prod is the compose project at `/home/jandro/projects/homelab/humblewhale/grubdrops` (`compose.yml`), container `grubdrops`, image `ghcr.io/aalejandrofer/grubdrops:latest`, DB at goose 16. Jandro runs it. It has not been run yet.

## Preconditions

- Staging is green on the same image and a live-drop check passed.
- `GRUB_MASTER_KEY` stays unchanged in the compose env. A different key cannot decrypt sessions and every account shows signed out.
- No `miner.db.pre-v2` is in the data directory. v2 skips its backup when one exists, so rename any old one first.
- Do all of "Before the tag" first. Pushing `v2.0.0` moves `latest` to v2, so after that the 1.x image can only be recovered from the local copy.

## Before the tag

The running image is `sha256:386a3059b9d4f3d6f056cf20a4cfb3ffe2c83c351d3b6064c0d2139ba8c6d682`. No `v1.x` tag has migration 0016, so this local image is the only rollback target.

1. Confirm the container still runs it: `docker inspect grubdrops --format '{{.Image}}'`.
2. Tag it: `docker tag sha256:386a3059b9d4f3d6f056cf20a4cfb3ffe2c83c351d3b6064c0d2139ba8c6d682 grubdrops:rollback-1x`.
3. Verify: `docker image inspect grubdrops:rollback-1x --format '{{.Id}}'` prints the same ID.

## Steps

1. From the compose dir, stop the stack: `docker compose stop`.
2. Copy `miner.db`, `miner.db-wal` and `miner.db-shm` as one set to a dated backup folder outside the data directory. Restore them only as a set.
3. Upgrade: `docker compose pull && docker compose up -d`. Never `docker start`: it reuses the old image. To pin instead, set the image to `ghcr.io/aalejandrofer/grubdrops:v2.0.0` first.
4. Check the log for both lines:
   - `goose: successfully migrated database to version: 18`
   - `store 1.x import done`, with games, claims and minutes counts.
5. If `store 1.x import done` is missing or a `1.x import:` error appears, run `docker compose stop` and roll back. The import is one transaction and rolls back, but the migrations to 18 have already run, so restore the DB before starting 1.x.

v2 writes `<db>.pre-v2` itself before it migrates.

## Post-checks in the UI

- Home totals look right.
- Rewards all-time count matches the 1.x claim count.
- Games lists the chosen games.
- Accounts show signed in.
- Settings, Health: run the canary and see a result.

## Rollback

Claims and minutes recorded under v2 are lost.

1. `docker compose stop`.
2. Restore the DB, either:
   - the manual backup set: `miner.db`, `miner.db-wal` and `miner.db-shm` together, or
   - `miner.db.pre-v2` alone as `miner.db`, with `miner.db-wal` and `miner.db-shm` removed. It is a standalone copy v2 made just before it migrated.
3. Rename `miner.db.pre-v2` to `miner.db.pre-v2.used` (or delete it). v2 skips the backup when the file exists, so a later re-upgrade would keep the stale one.
4. Set the compose image to `grubdrops:rollback-1x` and run `docker compose up -d`.
