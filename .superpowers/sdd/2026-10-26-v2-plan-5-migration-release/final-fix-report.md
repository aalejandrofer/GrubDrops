# Final fix report

- Important 4: `Snapshot.NextScanAt` is per state. Watching, claiming, picking, starting: lastScanTry + rescanEvery. Sleeping and awaiting-connect: the armed recheck timer deadline (`r.nextRecheck`, stamped in `Run`), falling back to lastScanTry + recheckInterval. needs_login and no_games: zero. Tests added for sleeping and needs_login.
- Minor 1: Home now checks signed_out before the all-paused banner, matching Accounts. Vitest assertion added.
- Important 1-3, Minor 2-3: runbook rewritten (rollback image tag, compose stop/pull/up, db+wal+shm as a set, stop on missing import line, pre-v2 rename after rollback, master key unchanged).
- CHANGELOG [Unreleased] Fixed updated.
- Gate: gofmt clean, go build and go test green, web check 0 errors, vitest 183/183.
- Minor 4 (canary skip shown as failing in 1.x) not changed; cosmetic.
