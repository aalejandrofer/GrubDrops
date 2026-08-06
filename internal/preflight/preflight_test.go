package preflight

import (
	"os"
	"path/filepath"
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

// skipIfRoot skips permission tests when running as root, which bypasses mode
// bits and would make the probe succeed regardless.
func skipIfRoot(t *testing.T) {
	t.Helper()
	if os.Getuid() == 0 {
		t.Skip("running as root; mode bits are not enforced")
	}
}

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
	skipIfRoot(t)
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
	skipIfRoot(t)
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
