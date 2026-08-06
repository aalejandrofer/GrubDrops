# Startup preflight — design

**Date:** 2026-08-06
**Status:** approved, pre-implementation
**Scope:** make the two failures new self-hosters actually hit explain themselves,
so a misconfigured container tells the operator the fix instead of a SQLite
error code.

This is sub-project **P2** of three (operator chores → **adoption** → internals).
P1 shipped as v1.3.12. P3 (splitting `watcher.go`, the fat handlers, a metrics
endpoint) is out of scope here.

## Problem

Every user issue this project has received was a deployment failure, not a
mining bug: #32 (Portainer rejected the compose YAML), #33 (the documented way
to make a master key produced an invalid one), #15 (ARM), #10 (Kick watch
time). Meanwhile the repository saw 53 unique cloners in 14 days. The people
who fail at `docker compose up` mostly do not file an issue; they leave.

Both remaining first-run failures were reproduced in the real container image
on 2026-08-06, not just against a local binary.

### Failure 1: the data directory is not writable

The image runs as distroless `nonroot`, UID 65532. A freshly created bind mount
is owned by the host user, so the miner cannot create its database. Today:

```
{"level":"INFO","msg":"miner starting", ... "db_path":"/data/miner.db" ...}
fatal: open store: ping sqlite: unable to open database file (14)
```

Nothing names permissions, UID 65532, or the bind mount. There is no path from
that message to the fix. The README documents the `chown`, but a reader who
missed it has no way back.

There is a second variant with a *different* error. If the directory is
read-only but the database file already exists, SQLite cannot create its `-wal`
and `-shm` sidecars and fails with a different code:

```
fatal: open store: ping sqlite: attempt to write a readonly database (1544)
```

Verified 2026-08-06 against `store.Open`. An earlier draft of this spec guessed
that this variant would pass startup and only fail later at login; that guess
was wrong, and the probe above is what settled it. Both variants do fail at
boot. The problem is that they fail with two different SQLite codes, 14 and
1544, neither of which names permissions, the UID, or the mount. A single
write probe covers both with one message the operator can act on.

### Failure 2: the master key is not an age identity

`GRUB_MASTER_KEY` must be an age X25519 identity. A random string parses as
nothing. Today, in the real container:

```
{"level":"INFO","msg":"OK   0013_account_channels.sql (2.86ms)"}
{"level":"INFO","msg":"OK   0014_account_force_channels.sql (4.29ms)"}
{"level":"INFO","msg":"OK   0015_drop_account_proxy_url.sql (5.97ms)"}
{"level":"INFO","msg":"goose: successfully migrated database to version: 15"}
fatal: master key invalid: parse age identity: malformed secret key: mixed case
```

Exit code 1 after **18 lines**, having created and fully migrated a database
using a key it then rejects. The key is validated at `cmd/miner/main.go:104`,
*after* `store.Open` at `:98`, even though validating it touches no disk and
costs microseconds.

This compounds: the quickstart compose sets `restart: unless-stopped`, so a bad
key is an infinite crash loop emitting 18 lines per restart. In a Portainer log
pane the one actionable line scrolls away continuously.

## Non-goals

- **A docker socket check.** The miner already warns at runtime ("docker
  control unavailable; kick sidecars stay always-on") and correctly treats it
  as non-fatal, since Twitch and the Kick WebSocket path work without it.
- **Disk space, port availability, memory.** No user has reported these, and
  Go's listener error for a taken port is already clear. Speculative checks
  are how a preflight turns into a framework nobody trusts.
- **A diagnostics HTTP page.** Considered and rejected: a half-running app that
  answers `/healthz` while broken misleads watchtower, orchestrators, and the
  operator. A misconfigured miner should not look alive.
- **Translating these messages.** Startup logs are English today and stay that
  way. No locale keys, no parity burden.
- **Changing the in-app empty states.** `/drops` already shows a bootstrap CTA
  on an empty whitelist, the accounts list has an add-account CTA, and the
  dashboard raises `no_games`. Nobody has reported getting lost once inside.

## Design

A new `internal/preflight` package, called from `run()` in `cmd/miner/main.go`
after the logger is configured (so the existing "miner starting" line still
prints `db_path` as context) and **before** `store.Open`.

`main()` already prints failures as `fmt.Fprintln(os.Stderr, "fatal:", err)`
(`cmd/miner/main.go:68`), so a multi-line error string renders correctly with no
change to `main()`. Messages go to stderr as plain text, not JSON through slog:
they are read by a human in a log pane, and the `fatal:` path is already plain.

### Check 1: master key

Validates the age identity in memory, touching no disk. `config.Load` already
rejects an empty `GRUB_MASTER_KEY` (`internal/config/config.go:85`) and that
stays; this covers the non-empty but invalid case.

To avoid a second copy of the parsing rule, preflight calls the same
`age.ParseX25519Identity` that `store.NewCryptor` uses
(`internal/store/crypto.go:17`). The `NewCryptor` call at `cmd/miner/main.go:104`
stays exactly as it is and remains the real construction; preflight only fails
earlier and more legibly.

```
fatal: GRUB_MASTER_KEY is not a valid age identity

  It must look like: AGE-SECRET-KEY-1...
  Generate one with:  docker run --rm ghcr.io/aalejandrofer/grubdrops:latest keygen

  Reuse the SAME key on every restart, or stored logins cannot be decrypted.
```

Running before `store.Open` means this failure costs two lines instead of 18,
and leaves no half-created database behind.

### Check 2: data directory

Takes `cfg.DBPath`, resolves its parent directory, and performs a **real write
probe**: create a uniquely named temp file in that directory, then remove it.
Reading the mode bits is not enough: the bits do not account for a read-only
filesystem, for the effective UID and its groups, or for ACLs, and getting that
reasoning right in Go duplicates what the kernel already answers in one syscall.
The probe also collapses both SQLite failures (codes 14 and 1544) into a single
message, which is the point.

The probe file is created with `os.CreateTemp(dir, ".grubdrops-preflight-*")`
and removed in a `defer`, so a crash between create and remove leaves one
obvious, clearly-named zero-byte file rather than something mysterious.

Distinguish two cases, because the fixes differ:

- **Directory does not exist** — the mount is missing entirely, or `GRUB_DB_PATH`
  points somewhere unintended. Say so, and name the path.
- **Directory exists but the probe fails** — permissions. Report the path and
  the effective UID from `os.Getuid()` (read at runtime, so the message stays
  correct if the image's user ever changes):

```
fatal: the data directory is not writable

  path: /data
  uid:  65532

  The mounted directory must be writable by UID 65532. On the host:
      sudo chown -R 65532:65532 ./data
  Or use a named Docker volume instead of a bind mount.
```

The `chown` line names the container-side path (`/data`) while the fix applies
to the host path. That is unavoidable, since the container cannot know the host
path, and the compose file in the README maps `./data` to `/data`, so `./data`
is the right thing to suggest.

## Isolation

`internal/preflight` depends only on the standard library plus `filippo.io/age`.
It does not import `internal/store`, `internal/config`, or anything that opens a
database, so it stays trivially testable and cannot itself be the thing that
fails.

Its public surface is one function per check plus one aggregate:

- `CheckMasterKey(secret string) error`
- `CheckDataDir(dbPath string) error`
- `Run(dbPath, masterKey string) error` — runs the checks in cost order (key
  first, since it touches no disk) and returns the first failure.

`run()` in `cmd/miner/main.go` gains one call and one error return.

## Testing

All tests use `t.TempDir()` and `chmod`. No Docker, no network.

| Case | Expectation |
|---|---|
| Valid age identity | no error |
| Random base64 string (the #33 case) | error naming `AGE-SECRET-KEY-1` and `keygen` |
| Empty string | error (defence in depth; `config.Load` catches it first) |
| Writable directory | no error, and no probe file left behind |
| Directory missing | error naming the path, distinct from the permissions error |
| Directory read-only (`chmod 0500`) | error naming the path and the UID |
| DB file exists, directory read-only | error, the SQLite-1544 variant, same message as the 14 variant |
| Check order | an invalid key is reported even when the data dir is also broken |

The last row matters: it pins the cost ordering, which is the whole point of
moving the key check earlier.

A test asserting no probe file remains after a successful check guards the
`defer` cleanup, since a leaked file in the operator's data directory on every
boot would be its own small bug.

**Tests must be revert-proof.** For each check, confirm the test fails when the
check is removed. Three plan-specified tests during P1 turned out to be
incapable of failing, so this is a standing requirement, not a nicety.

## Release gating

Nothing here touches accrual or claim logic, so per `CLAUDE.md` this ships on
the default gate: green build, passing unit tests, green transport canary. No
live drop required.

Worth a manual confirmation before tagging, since the entire point is the
operator-visible output: run the built image with a bad key and with an
unwritable mount, and read what a new user would read.

Every change lands in `docs/CHANGELOG.md` under `[Unreleased]` as it is
committed.
