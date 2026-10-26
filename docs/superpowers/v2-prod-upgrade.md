# v2 prod upgrade runbook

Upgrades prod (container `grubdrops`, drops.ryuzec.dev) from the 1.x image `ghcr.io/aalejandrofer/grubdrops:latest` (DB at goose 16) to v2. Jandro runs it. It has not been run yet.

## Preconditions

- The v2.0.0 tag is cut only after a live-drop check.
- Staging is green on the same image.

## Steps

1. Stop the container: `docker stop grubdrops`.
2. Copy `miner.db`, `miner.db-wal` and `miner.db-shm` to a dated backup folder outside the data directory.
3. Pull the v2 image and pin its tag (`v2.0.0`) in the compose file, not `latest`.
4. Start the container: `docker start grubdrops`, or `docker compose up -d`.
5. Check the log for:
   - `goose: successfully migrated database to version: 18`
   - `store 1.x import done`, with games, claims and minutes counts.

v2 writes `<db>.pre-v2` by itself before it migrates.

## Post-checks in the UI

- Home totals look right.
- Rewards all-time count matches the 1.x claim count.
- Games lists the chosen games.
- Accounts show signed in.
- Settings, Health: run the canary and see a result.

## Rollback

1. Stop the container.
2. Copy `miner.db.pre-v2` (or the manual copy) over `miner.db`.
3. Remove `miner.db-wal` and `miner.db-shm`.
4. Run the previous 1.x image.
