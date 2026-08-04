package api

import (
	"context"
	"database/sql"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/aalejandrofer/grubdrops/internal/store"
)

// A snapshot must be a real, openable SQLite database containing the live
// data — not a truncated or torn copy. VACUUM INTO is the reason this button
// exists instead of telling the operator to cp a live WAL database.
func TestWriteSnapshot_ProducesOpenableCopy(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "src.db"))
	if err != nil {
		t.Fatalf("open source db: %v", err)
	}
	defer db.Close()

	if _, err := db.ExecContext(ctx,
		`INSERT INTO kv (key, value) VALUES ('snapshot_probe', 'present')`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	dest := filepath.Join(t.TempDir(), "snap.db")
	if err := writeSnapshot(ctx, db, dest); err != nil {
		t.Fatalf("writeSnapshot: %v", err)
	}

	fi, err := os.Stat(dest)
	if err != nil {
		t.Fatalf("stat snapshot: %v", err)
	}
	if fi.Size() == 0 {
		t.Fatal("snapshot file is empty")
	}

	copied, err := sql.Open("sqlite", dest)
	if err != nil {
		t.Fatalf("open snapshot: %v", err)
	}
	defer copied.Close()

	var got string
	if err := copied.QueryRowContext(ctx,
		`SELECT value FROM kv WHERE key = 'snapshot_probe'`).Scan(&got); err != nil {
		t.Fatalf("read from snapshot: %v", err)
	}
	if got != "present" {
		t.Fatalf("snapshot value = %q, want present", got)
	}
}

// VACUUM INTO refuses to overwrite, so a stale temp file must not wedge the
// button permanently.
func TestWriteSnapshot_FailsOnExistingDest(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "src.db"))
	if err != nil {
		t.Fatalf("open source db: %v", err)
	}
	defer db.Close()

	dest := filepath.Join(t.TempDir(), "snap.db")
	if err := os.WriteFile(dest, []byte("stale"), 0o600); err != nil {
		t.Fatalf("write stale file: %v", err)
	}
	if err := writeSnapshot(ctx, db, dest); err == nil {
		t.Fatal("expected an error when the destination already exists")
	}
}

// A nil db (no live database handle wired) must fail closed with 503, not
// panic. This is the "snapshot unavailable" branch at the top of
// postSnapshot.
func TestPostSnapshot_NilDBReturns503(t *testing.T) {
	d := &settingsDeps{}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/settings/snapshot", nil)
	d.postSnapshot(rec, req)

	if rec.Code != 503 {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

var snapshotFilenamePattern = regexp.MustCompile(`^attachment; filename="grubdrops-\d{8}-\d{4}\.db"$`)

// The handler must stream a real, openable SQLite snapshot with the right
// headers, and must not leak its working temp directory once the request
// completes. This is the design spec's explicit promise ("the temp file is
// gone afterwards", "the response is a non-empty SQLite file") that had no
// coverage before this test.
func TestPostSnapshot_ServesSnapshotAndCleansUpTempDir(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "src.db"))
	if err != nil {
		t.Fatalf("open source db: %v", err)
	}
	defer db.Close()

	if _, err := db.ExecContext(ctx,
		`INSERT INTO kv (key, value) VALUES ('snapshot_handler_probe', 'present')`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	before, err := filepath.Glob(filepath.Join(os.TempDir(), "grubdrops-snapshot-*"))
	if err != nil {
		t.Fatalf("glob before: %v", err)
	}

	d := &settingsDeps{db: db}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/settings/snapshot", nil)
	d.postSnapshot(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/x-sqlite3" {
		t.Fatalf("Content-Type = %q, want application/x-sqlite3", ct)
	}
	if cd := rec.Header().Get("Content-Disposition"); !snapshotFilenamePattern.MatchString(cd) {
		t.Fatalf("Content-Disposition = %q, does not match grubdrops-YYYYMMDD-HHMM.db shape", cd)
	}
	if rec.Body.Len() == 0 {
		t.Fatal("response body is empty")
	}

	// The response body must be a valid, openable SQLite database containing
	// the row we seeded — not a truncated or torn copy.
	copyPath := filepath.Join(t.TempDir(), "downloaded.db")
	if err := os.WriteFile(copyPath, rec.Body.Bytes(), 0o600); err != nil {
		t.Fatalf("write downloaded body: %v", err)
	}
	copied, err := sql.Open("sqlite", copyPath)
	if err != nil {
		t.Fatalf("open downloaded snapshot: %v", err)
	}
	defer copied.Close()
	var got string
	if err := copied.QueryRowContext(ctx,
		`SELECT value FROM kv WHERE key = 'snapshot_handler_probe'`).Scan(&got); err != nil {
		t.Fatalf("read seeded row from downloaded snapshot: %v", err)
	}
	if got != "present" {
		t.Fatalf("downloaded snapshot value = %q, want present", got)
	}

	after, err := filepath.Glob(filepath.Join(os.TempDir(), "grubdrops-snapshot-*"))
	if err != nil {
		t.Fatalf("glob after: %v", err)
	}
	if len(after) != len(before) {
		t.Fatalf("leftover grubdrops-snapshot-* temp dirs: before=%d after=%d", len(before), len(after))
	}
}
