package api

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
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
