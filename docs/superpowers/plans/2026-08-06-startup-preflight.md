# Startup Preflight Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the two failures new self-hosters actually hit explain their own fix, instead of reporting a SQLite error code after creating a database with a key the miner then rejects.

**Architecture:** A new `internal/preflight` package with two pure-ish checks, called from `run()` in `cmd/miner/main.go` after the logger is configured and **before** `store.Open`. Master key is validated in memory (no disk), then the data directory is validated with a real write probe. Failures return a multi-line error that the existing `fmt.Fprintln(os.Stderr, "fatal:", err)` in `main()` prints as-is.

**Tech Stack:** Go 1.26.2, `filippo.io/age` v1.3.1 (already a direct dependency), stdlib only otherwise. Tests use `t.TempDir()` and `os.Chmod`; no Docker, no network.

## Global Constraints

- **Go 1.26.2** (`go.mod`). Run `gofmt -w` on every file you touch before committing — CI has a gofmt gate that fails fast.
- **Messages are plain multi-line text on stderr, not JSON via slog.** They are read by a human in a Portainer log pane. The `fatal:` path is already plain text.
- **English only.** Startup logs are not translated today and this does not change that. No locale keys, no `internal/i18n` involvement, no parity burden.
- **`internal/preflight` must not import `internal/store`, `internal/config`, or anything that opens a database.** Stdlib plus `filippo.io/age` only. It must not be able to fail for the reasons it exists to diagnose.
- **Do not change `config.Load`.** It already rejects an empty `GRUB_MASTER_KEY` (`internal/config/config.go:85`) and that behaviour stays.
- **Do not change or remove the `store.NewCryptor` call** at `cmd/miner/main.go:104`. It remains the real construction; preflight only fails earlier and more legibly.
- **Do not touch accrual or claim logic.** Do not delete or refactor `cmd/browser-sidecar`, `internal/auth/browser`, `internal/dockerctl`, `proto/`, or the Twitch `BrowserBackend`.
- **Log every change in `docs/CHANGELOG.md` under `## [Unreleased]`.**
- **Tests must be revert-proof.** For every test you add, delete the check it covers, watch the test fail, quote the failure, then restore. Three plan-specified tests in the previous sub-project turned out to be incapable of failing. This is a hard requirement, not a nicety.
- Test style in this repo is hand-rolled fakes and `t.Fatalf`, **not testify**. Match it.
- Commit messages: plain prose, no em dashes or en dashes. End each commit body with:
  `Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>`

## File Structure

| File | Responsibility | Task |
|---|---|---|
| `internal/preflight/preflight.go` | `Run` aggregate plus the two checks and their messages | 1, 2 |
| `internal/preflight/preflight_test.go` | Table tests for both checks and the ordering guarantee | 1, 2, 3 |
| `cmd/miner/main.go` | One `preflight.Run` call before `store.Open` | 3 |
| `docs/CHANGELOG.md` | `[Unreleased]` entry | 3 |

One package file rather than one file per check: together they are well under 150 lines, they share the message-formatting style, and splitting two short functions across two files would be ceremony. Tests live in one file for the same reason.

---

### Task 1: Master key check

Validates the age identity in memory. This runs before anything touches disk, which is the ordering fix: today a bad key exits after 18 log lines having created and fully migrated a database.

**Files:**
- Create: `internal/preflight/preflight.go`
- Create: `internal/preflight/preflight_test.go`

**Interfaces:**
- Consumes: `age.ParseX25519Identity` from `filippo.io/age` (already in `go.mod` as a direct dependency; the same function `internal/store/crypto.go:17` uses).
- Produces: `func CheckMasterKey(secret string) error`. Task 3 calls it via `Run`.

- [ ] **Step 1: Write the failing test**

Create `internal/preflight/preflight_test.go`:

```go
package preflight

import (
	"strings"
	"testing"

	"filippo.io/age"
)

// validKey returns a real age X25519 identity string.
func validKey(t *testing.T) string {
	t.Helper()
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("generate identity: %v", err)
	}
	return id.String()
}

func TestCheckMasterKey_ValidIdentityPasses(t *testing.T) {
	if err := CheckMasterKey(validKey(t)); err != nil {
		t.Fatalf("valid age identity rejected: %v", err)
	}
}

// The exact shape of issue #33: the old docs told users to make a key with
// `head -c32 /dev/urandom | base64`, which is not an age identity.
func TestCheckMasterKey_RandomStringIsRejected(t *testing.T) {
	err := CheckMasterKey("Zm9vYmFyYmF6cXV4MTIzNDU2Nzg5MGFiY2RlZg==")
	if err == nil {
		t.Fatal("a random base64 string must be rejected")
	}
	msg := err.Error()
	// The message has to carry the fix, not just the diagnosis. A user who
	// only reads this one line must know what to type next.
	if !strings.Contains(msg, "AGE-SECRET-KEY-1") {
		t.Fatalf("message must show the expected key shape, got: %s", msg)
	}
	if !strings.Contains(msg, "keygen") {
		t.Fatalf("message must name the keygen command, got: %s", msg)
	}
}

func TestCheckMasterKey_EmptyIsRejected(t *testing.T) {
	if err := CheckMasterKey(""); err == nil {
		t.Fatal("an empty key must be rejected")
	}
}

// Surrounding whitespace is easy to introduce in a .env file or a Portainer
// environment-variables box, and is not the user's mistake in spirit.
func TestCheckMasterKey_TrimsSurroundingWhitespace(t *testing.T) {
	if err := CheckMasterKey("  " + validKey(t) + "\n"); err != nil {
		t.Fatalf("a key with surrounding whitespace should be accepted: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/preflight/ -run TestCheckMasterKey -v`
Expected: FAIL to build, with `undefined: CheckMasterKey` (the package does not exist yet).

- [ ] **Step 3: Write minimal implementation**

Create `internal/preflight/preflight.go`:

```go
// Package preflight validates the environment before the miner opens its
// database, so that a misconfigured container reports the fix rather than a
// SQLite error code.
//
// Every user-reported problem with this project has been a deployment failure
// rather than a mining bug, so these messages are part of the product: they
// are read by a human in a log pane, often in Portainer, and they are the only
// thing standing between a new self-hoster and giving up.
//
// This package deliberately imports only the standard library and
// filippo.io/age. It must not be able to fail for the reasons it exists to
// diagnose.
package preflight

import (
	"fmt"
	"strings"

	"filippo.io/age"
)

// CheckMasterKey verifies GRUB_MASTER_KEY is an age X25519 identity. It
// touches no disk, so it is cheap enough to run before anything else.
//
// Ordering matters: the miner used to validate this only after opening and
// migrating the database, so a bad key cost eighteen log lines and left a
// half-created database behind. With the quickstart compose restarting on
// failure, that repeated forever and buried the one useful line.
func CheckMasterKey(secret string) error {
	if _, err := age.ParseX25519Identity(strings.TrimSpace(secret)); err != nil {
		return fmt.Errorf(`GRUB_MASTER_KEY is not a valid age identity

  It must look like: AGE-SECRET-KEY-1...
  Generate one with:  docker run --rm ghcr.io/aalejandrofer/grubdrops:latest keygen

  Reuse the SAME key on every restart, or stored logins cannot be decrypted.

  (underlying error: %v)`, err)
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/preflight/ -run TestCheckMasterKey -v`
Expected: PASS, four tests.

- [ ] **Step 5: Prove the tests bite**

Temporarily replace the body of `CheckMasterKey` with `return nil`.

Run: `go test ./internal/preflight/ -run TestCheckMasterKey -v`
Expected: `TestCheckMasterKey_RandomStringIsRejected` and `TestCheckMasterKey_EmptyIsRejected` both FAIL. Quote the failure output in your report, then restore the real body and re-run to confirm green.

A test that cannot fail is worse than no test, because it advertises safety it does not provide.

- [ ] **Step 6: Commit**

```bash
gofmt -w ./internal/preflight
git add internal/preflight/
git commit -m "feat(preflight): validate the master key before touching disk

A bad GRUB_MASTER_KEY used to be caught only after the database was opened
and fully migrated, so the failure cost eighteen log lines and left a
half-created database behind. Validating the age identity in memory makes
that failure immediate and tells the user how to generate a real key.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: Data directory check

**Files:**
- Modify: `internal/preflight/preflight.go`
- Modify: `internal/preflight/preflight_test.go`

**Interfaces:**
- Consumes: nothing from Task 1 beyond sharing the package.
- Produces: `func CheckDataDir(dbPath string) error`. Task 3 calls it via `Run`.

**Background you need:** the image runs as distroless `nonroot`, UID 65532. A freshly created bind mount is owned by the host user, so the container cannot write to it. Two different SQLite failures result, both verified against `store.Open` on 2026-08-06:

- directory missing or unwritable, no database yet: `ping sqlite: unable to open database file (14)`
- directory read-only but the database file already exists: `ping sqlite: attempt to write a readonly database (1544)`

Neither names permissions, the UID, or the mount. One write probe replaces both with one actionable message.

- [ ] **Step 1: Write the failing test**

Append to `internal/preflight/preflight_test.go` (and add `"os"`, `"path/filepath"` to its imports):

```go
func TestCheckDataDir_WritableDirPasses(t *testing.T) {
	dir := t.TempDir()
	if err := CheckDataDir(filepath.Join(dir, "miner.db")); err != nil {
		t.Fatalf("writable dir rejected: %v", err)
	}
}

// The probe must not litter the operator's data directory. A stray file on
// every boot would be its own small bug.
func TestCheckDataDir_LeavesNoProbeFile(t *testing.T) {
	dir := t.TempDir()
	if err := CheckDataDir(filepath.Join(dir, "miner.db")); err != nil {
		t.Fatalf("writable dir rejected: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("probe left files behind: %v", entries)
	}
}

// A missing directory means the mount is absent or GRUB_DB_PATH points
// somewhere unintended. That is a different fix from a permissions problem,
// so it gets a different message.
func TestCheckDataDir_MissingDirIsReported(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "not-created", "miner.db")
	err := CheckDataDir(missing)
	if err == nil {
		t.Fatal("a missing data directory must be reported")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("message should say the directory does not exist, got: %s", err)
	}
}

// The chown trap: the directory exists but the container's UID cannot write.
func TestCheckDataDir_UnwritableDirIsReported(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	err := CheckDataDir(filepath.Join(dir, "miner.db"))
	if err == nil {
		t.Fatal("an unwritable data directory must be reported")
	}
	msg := err.Error()
	if !strings.Contains(msg, dir) {
		t.Fatalf("message must name the path, got: %s", msg)
	}
	if !strings.Contains(msg, "chown") {
		t.Fatalf("message must name the chown fix, got: %s", msg)
	}
}

// The SQLite-1544 variant: the database already exists but the directory is
// read-only, so SQLite cannot create its -wal and -shm sidecars. The operator
// gets the same message as the other permissions case, because the fix is the
// same.
func TestCheckDataDir_ReadOnlyDirWithExistingDBIsReported(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "miner.db")
	if err := os.WriteFile(dbPath, []byte("not really a db"), 0o644); err != nil {
		t.Fatalf("seed db file: %v", err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	err := CheckDataDir(dbPath)
	if err == nil {
		t.Fatal("a read-only directory must be reported even when the db file exists")
	}
	if !strings.Contains(err.Error(), "chown") {
		t.Fatalf("message must name the chown fix, got: %s", err)
	}
}
```

**Note on running as root:** these two permission tests are meaningless as root, because root bypasses the mode bits and the probe would succeed. Guard both by skipping when `os.Getuid() == 0`. Add this helper to the test file and call it as the first line of `TestCheckDataDir_UnwritableDirIsReported` and `TestCheckDataDir_ReadOnlyDirWithExistingDBIsReported`:

```go
// skipIfRoot skips permission tests when running as root, which bypasses mode
// bits and would make the probe succeed regardless.
func skipIfRoot(t *testing.T) {
	t.Helper()
	if os.Getuid() == 0 {
		t.Skip("running as root; mode bits are not enforced")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/preflight/ -run TestCheckDataDir -v`
Expected: FAIL to build, with `undefined: CheckDataDir`.

- [ ] **Step 3: Write minimal implementation**

Add to `internal/preflight/preflight.go` (and add `"os"`, `"path/filepath"` to its imports):

```go
// CheckDataDir verifies the miner can actually write into the directory that
// will hold the database.
//
// This is a real write probe rather than an inspection of the mode bits.
// Mode bits do not account for a read-only filesystem, for the effective UID
// and its supplementary groups, or for ACLs, and reimplementing that logic in
// Go duplicates what the kernel answers in a single syscall. The probe also
// collapses two different SQLite failures into one message: "unable to open
// database file (14)" when the directory is unwritable and no database exists
// yet, and "attempt to write a readonly database (1544)" when the directory is
// read-only but the database file is already there. Neither names permissions,
// the UID, or the mount, and the fix for both is the same chown.
func CheckDataDir(dbPath string) error {
	dir := filepath.Dir(dbPath)

	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf(`the data directory does not exist

  path: %s

  Nothing is mounted there. Check the volume mapping in your compose file,
  and that GRUB_DB_PATH points inside it.`, dir)
		}
		return fmt.Errorf(`the data directory cannot be read

  path: %s

  (underlying error: %v)`, dir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf(`the data directory is not a directory

  path: %s

  GRUB_DB_PATH should be a file inside a mounted directory, for example
  /data/miner.db.`, dir)
	}

	f, err := os.CreateTemp(dir, ".grubdrops-preflight-*")
	if err != nil {
		return fmt.Errorf(`the data directory is not writable

  path: %s
  uid:  %d

  The mounted directory must be writable by UID %d. On the host:
      sudo chown -R %d:%d ./data
  Or use a named Docker volume instead of a bind mount.

  (underlying error: %v)`, dir, os.Getuid(), os.Getuid(), os.Getuid(), os.Getuid(), err)
	}
	name := f.Name()
	// Close before removing so the handle is never left open on an error path.
	_ = f.Close()
	_ = os.Remove(name)
	return nil
}
```

The UID comes from `os.Getuid()` at runtime rather than a hardcoded 65532, so the message stays correct if the image's user ever changes.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/preflight/ -run TestCheckDataDir -v`
Expected: PASS, five tests (the two permission tests skip if you happen to be root).

- [ ] **Step 5: Prove the tests bite**

Temporarily replace the body of `CheckDataDir` with `return nil`.

Run: `go test ./internal/preflight/ -run TestCheckDataDir -v`
Expected: `TestCheckDataDir_MissingDirIsReported`, `TestCheckDataDir_UnwritableDirIsReported`, and `TestCheckDataDir_ReadOnlyDirWithExistingDBIsReported` all FAIL. Quote the output in your report, then restore and re-run to confirm green.

Separately, confirm the no-litter test bites: comment out the `os.Remove(name)` line, run `TestCheckDataDir_LeavesNoProbeFile`, and confirm it FAILS. Quote it, then restore.

- [ ] **Step 6: Commit**

```bash
gofmt -w ./internal/preflight
git add internal/preflight/
git commit -m "feat(preflight): check the data directory with a real write probe

An unwritable bind mount produced one of two SQLite error codes, 14 or
1544, neither of which named permissions, the UID, or the mount. A write
probe answers the question the kernel already knows the answer to, and
covers a read-only filesystem and ACLs that mode bits alone would miss.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: Wire preflight into startup

**Files:**
- Modify: `internal/preflight/preflight.go`
- Modify: `internal/preflight/preflight_test.go`
- Modify: `cmd/miner/main.go` (in `run()`, between the "miner starting" log line and `store.Open` at `:98`)
- Modify: `docs/CHANGELOG.md`

**Interfaces:**
- Consumes: `CheckMasterKey(secret string) error` from Task 1, `CheckDataDir(dbPath string) error` from Task 2.
- Produces: `func Run(dbPath, masterKey string) error`.

- [ ] **Step 1: Write the failing test**

Append to `internal/preflight/preflight_test.go`:

```go
func TestRun_AllGoodPasses(t *testing.T) {
	dir := t.TempDir()
	if err := Run(filepath.Join(dir, "miner.db"), validKey(t)); err != nil {
		t.Fatalf("a good environment was rejected: %v", err)
	}
}

// The key check must come first because it touches no disk. This is the whole
// point of the ordering: a bad key should never cost a database open.
func TestRun_ReportsKeyBeforeDataDir(t *testing.T) {
	skipIfRoot(t)
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	// BOTH are broken. The key error is the one that must surface.
	err := Run(filepath.Join(dir, "miner.db"), "definitely-not-an-age-key")
	if err == nil {
		t.Fatal("expected an error when both the key and the data dir are broken")
	}
	if !strings.Contains(err.Error(), "AGE-SECRET-KEY-1") {
		t.Fatalf("the key failure should be reported first, got: %s", err)
	}
}

func TestRun_ReportsDataDirWhenKeyIsFine(t *testing.T) {
	skipIfRoot(t)
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	err := Run(filepath.Join(dir, "miner.db"), validKey(t))
	if err == nil {
		t.Fatal("expected an error for an unwritable data dir")
	}
	if !strings.Contains(err.Error(), "chown") {
		t.Fatalf("expected the data dir message, got: %s", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/preflight/ -run TestRun -v`
Expected: FAIL to build, with `undefined: Run`.

- [ ] **Step 3: Write minimal implementation**

Add to `internal/preflight/preflight.go`:

```go
// Run performs every startup check in cost order and returns the first
// failure. The master key is checked first because it touches no disk: a bad
// key should never cost a database open, let alone a full migration.
func Run(dbPath, masterKey string) error {
	if err := CheckMasterKey(masterKey); err != nil {
		return err
	}
	return CheckDataDir(dbPath)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/preflight/ -v`
Expected: PASS, all tests across the three tasks.

- [ ] **Step 5: Wire it into `run()`**

In `cmd/miner/main.go`, find the "miner starting" log line in `run()` and the `store.Open` call that follows it (currently `:98`). Insert the preflight call between them, so the startup line still prints `db_path` as context but nothing touches disk before the checks:

```go
	// Fail with an actionable message before opening the database. Every
	// user-reported problem with this project has been a deployment failure,
	// and the two that remain, an invalid master key and an unwritable data
	// mount, used to surface as a SQLite error code after the database had
	// already been created and migrated.
	if err := preflight.Run(cfg.DBPath, cfg.MasterKey); err != nil {
		return err
	}
```

Add `"github.com/aalejandrofer/grubdrops/internal/preflight"` to the import block. Do not wrap the returned error: it is already formatted for a human, and `fmt.Errorf("preflight: %w", err)` would prepend a label that breaks the multi-line layout.

- [ ] **Step 6: Verify the wiring and the whole suite**

Run: `gofmt -l ./cmd ./internal`
Expected: no output.

Run: `go build ./... && go vet ./... && go test ./... 2>&1 | tail -30`
Expected: build and vet silent; every package `ok` or `[no test files]`.

- [ ] **Step 7: Confirm the operator-visible output by hand**

This is the deliverable, so read what a new user would read. From the repo root:

```bash
go build -o /tmp/miner-preflight ./cmd/miner

# Failure 2: a random string for a key. Expect the age message, and NO
# migration log lines at all.
GRUB_MASTER_KEY="$(head -c32 /dev/urandom | base64)" \
  GRUB_DB_PATH=/tmp/preflight-probe/miner.db /tmp/miner-preflight

# Failure 1: a good key, missing directory.
GRUB_MASTER_KEY="$(/tmp/miner-preflight keygen)" \
  GRUB_DB_PATH=/tmp/definitely-missing/miner.db /tmp/miner-preflight

# Failure 1 again: a good key, unwritable directory.
mkdir -p /tmp/preflight-ro && chmod 500 /tmp/preflight-ro
GRUB_MASTER_KEY="$(/tmp/miner-preflight keygen)" \
  GRUB_DB_PATH=/tmp/preflight-ro/miner.db /tmp/miner-preflight
chmod 700 /tmp/preflight-ro && rm -rf /tmp/preflight-ro /tmp/preflight-probe
```

Expected for the first: the key message, exit 1, and **no `goose:` or migration lines**, which is the regression this task exists to prevent. Quote all three outputs in your report.

- [ ] **Step 8: Changelog and commit**

Add under `## [Unreleased]` in `docs/CHANGELOG.md`, creating a `### Changed` subsection if one is not already there:

```markdown
### Changed

- **A misconfigured container now tells you what to fix.** Two first-run
  failures used to surface as a raw SQLite error code: an unwritable `./data`
  mount, which said `unable to open database file (14)` and named neither
  permissions nor the user the container runs as, and an invalid
  `GRUB_MASTER_KEY`, which was only caught after the database had been created
  and fully migrated, so the real error scrolled past under eighteen lines of
  migration output and repeated on every restart. Both are now checked before
  the database is opened, and each message names the exact command that fixes
  it.
```

```bash
gofmt -w ./cmd ./internal
git add internal/preflight/ cmd/miner/main.go docs/CHANGELOG.md
git commit -m "feat(miner): run startup preflight before opening the database

Checks the master key and the data directory before store.Open, so a
misconfigured container reports the fix instead of a SQLite error code, and
a bad key no longer creates and migrates a database before being rejected.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: Verify in the real container

The messages exist for people running Docker, often through Portainer, so the last check is against the built image rather than a local binary. Both failures were reproduced this way on 2026-08-06 before this work started, and the point is to compare against those baselines.

**Files:** none. This task changes no code.

- [ ] **Step 1: Build the image**

```bash
docker buildx build --platform linux/amd64 -f deploy/Dockerfile.miner -t grubdrops:preflight --load .
```

Confirm the image is fresh before trusting it: `docker image inspect grubdrops:preflight --format '{{.Created}} {{.Os}}/{{.Architecture}}'`. A stale image with the same tag from an earlier build has caused a false pass in this project before.

- [ ] **Step 2: Bad master key in the container**

```bash
docker run --rm -e GRUB_MASTER_KEY="not-an-age-key" -e GRUB_DB_PATH=/tmp/p.db grubdrops:preflight 2>&1 | tee /tmp/preflight-key.log
wc -l /tmp/preflight-key.log
```

Baseline before this change: **18 lines**, ending in `fatal: master key invalid: parse age identity: malformed secret key: mixed case`, after a full 15-migration run.

Expected now: a handful of lines, no `goose:` output, no migration lines, and the message naming `AGE-SECRET-KEY-1` and `keygen`. Record the line count in your report.

- [ ] **Step 3: Unwritable mount in the container**

```bash
mkdir -p /tmp/ro-mount && chmod 500 /tmp/ro-mount
docker run --rm -v /tmp/ro-mount:/data -e GRUB_MASTER_KEY="$(docker run --rm grubdrops:preflight keygen)" \
  -e GRUB_DB_PATH=/data/miner.db grubdrops:preflight 2>&1 | tail -12
chmod 700 /tmp/ro-mount && rm -rf /tmp/ro-mount
```

Baseline before this change: `fatal: open store: ping sqlite: unable to open database file (14)`.

Expected now: the data directory message naming the path, the UID the container runs as (65532 in this image), and the `chown` command. Confirm the UID printed is 65532 and not 0, which would mean `os.Getuid()` is reporting the wrong thing inside the image.

- [ ] **Step 4: Confirm a correct configuration still boots**

```bash
mkdir -p /tmp/ok-mount && chmod 777 /tmp/ok-mount
docker run --rm -d --name preflight-ok -v /tmp/ok-mount:/data \
  -e GRUB_MASTER_KEY="$(docker run --rm grubdrops:preflight keygen)" \
  -e GRUB_DB_PATH=/data/miner.db grubdrops:preflight
sleep 8
docker logs preflight-ok 2>&1 | tail -6
docker rm -f preflight-ok; rm -rf /tmp/ok-mount
```

Expected: normal startup, migrations run, `http listening`. The preflight must be invisible when everything is fine. Also confirm no `.grubdrops-preflight-*` file is left in `/tmp/ok-mount` before you delete it, which would mean the probe cleanup failed in the container even though it passed in tests.

- [ ] **Step 5: Report**

Record all four outputs. If the container UID is not 65532, or any `.grubdrops-preflight-*` file survives, stop and report rather than papering over it: both would mean the message or the cleanup is wrong in the environment that actually matters.

---

## Self-Review

**Spec coverage:**

| Spec requirement | Task |
|---|---|
| Check 1, master key validated in memory before disk | 1 |
| Message names `AGE-SECRET-KEY-1` and the `keygen` command | 1 (asserted in test) |
| Check 2, real write probe rather than mode-bit inspection | 2 |
| Distinguish missing directory from permissions failure | 2 (separate messages, separate tests) |
| Message names path, UID from `os.Getuid()`, and the `chown` | 2 (asserted in test) |
| Probe file cleaned up | 2 (`TestCheckDataDir_LeavesNoProbeFile`) |
| Covers both SQLite variants, 14 and 1544 | 2 (two separate tests) |
| `Run` orders checks by cost, key first | 3 (`TestRun_ReportsKeyBeforeDataDir`) |
| Called from `run()` before `store.Open` | 3 |
| Plain multi-line stderr, no slog, no i18n | 3 (no wrapping, no locale keys touched) |
| `config.Load` and `NewCryptor` unchanged | Global Constraints, nothing in any task modifies them |
| Package imports only stdlib plus age | 1 (import block), enforced by review |
| Every test proven revert-proof | 1 Step 5, 2 Step 5 |
| Manual confirmation of operator-visible output | 3 Step 7 (binary), 4 (container) |
| Changelog entry | 3 Step 8 |

No spec requirement is unimplemented.

**Placeholder scan:** No "TBD", "TODO", "handle edge cases", or "similar to Task N". Every code step carries the actual code. Task 4 contains no code because it is a verification task, and its steps are exact commands with stated expected output and recorded baselines.

**Type consistency:** `CheckMasterKey(string) error` is defined in Task 1 Step 3 and called in Task 3 Step 3. `CheckDataDir(string) error` is defined in Task 2 Step 3 and called in Task 3 Step 3. `Run(dbPath, masterKey string) error` is defined in Task 3 Step 3 with that exact parameter order and called as `preflight.Run(cfg.DBPath, cfg.MasterKey)` in Step 5, matching. The test helpers `validKey(t)` (Task 1) and `skipIfRoot(t)` (Task 2) are each defined once and reused in later tasks; both live in the single shared test file, so they resolve.

**One correction applied during review:** the spec originally claimed the read-only-directory variant would pass startup and fail later at login. That was verified false on 2026-08-06 (it fails at startup with SQLite 1544), the spec was corrected, and this plan reflects the verified behaviour rather than the guess.
