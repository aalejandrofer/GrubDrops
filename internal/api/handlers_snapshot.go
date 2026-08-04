package api

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// writeSnapshot writes a consistent copy of the live database to dest using
// VACUUM INTO. This is the point of the feature: copying miner.db with cp
// while the miner runs can capture a torn WAL state, whereas VACUUM INTO is
// consistent against a live database. dest must not already exist — VACUUM
// INTO refuses to overwrite.
func writeSnapshot(ctx context.Context, db *sql.DB, dest string) error {
	// dest is built by the caller from os.MkdirTemp plus a fixed name, never
	// from request input, so there is no injection surface here.
	if _, err := db.ExecContext(ctx, fmt.Sprintf("VACUUM INTO %s", quoteSQLiteString(dest))); err != nil {
		return fmt.Errorf("vacuum into: %w", err)
	}
	return nil
}

// quoteSQLiteString renders s as a SQLite string literal. VACUUM INTO takes a
// literal path, not a bound parameter, so the path must be quoted rather than
// passed as an argument.
func quoteSQLiteString(s string) string {
	out := make([]rune, 0, len(s)+2)
	out = append(out, '\'')
	for _, r := range s {
		if r == '\'' {
			out = append(out, '\'') // SQLite escapes a quote by doubling it
		}
		out = append(out, r)
	}
	out = append(out, '\'')
	return string(out)
}

// postSnapshot streams a consistent copy of the database as a download.
// POST (not GET) so it goes through the same CSRF middleware as every other
// admin action; it is an operator data-egress action and belongs on that
// footing even though it only reads.
//
// Known tradeoff, accepted: store.Open sets db.SetMaxOpenConns(1) to
// serialize writers, so VACUUM INTO holds the app's only connection for its
// duration and briefly stalls other DB work (dashboard polls, watcher
// progress writes). Acceptable for a manual, operator-initiated action on a
// database that is a few megabytes in normal use. Do not add a "snapshot on
// a timer" on top of this without revisiting that decision.
func (d *settingsDeps) postSnapshot(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if d.db == nil {
		http.Error(w, "snapshot unavailable", http.StatusServiceUnavailable)
		return
	}

	dir, err := os.MkdirTemp("", "grubdrops-snapshot-")
	if err != nil {
		slog.Warn("snapshot: temp dir failed", "err", err)
		http.Error(w, "snapshot failed", http.StatusInternalServerError)
		return
	}
	// Remove the whole directory, which covers the snapshot plus any -wal or
	// -shm sidecar file, on every exit path including a client disconnect.
	defer func() { _ = os.RemoveAll(dir) }()

	name := "grubdrops-" + time.Now().Format("20060102-1504") + ".db"
	dest := filepath.Join(dir, name)
	if err := writeSnapshot(ctx, d.db, dest); err != nil {
		slog.Warn("snapshot: vacuum failed", "err", err)
		http.Error(w, "snapshot failed", http.StatusInternalServerError)
		return
	}

	f, err := os.Open(dest)
	if err != nil {
		slog.Warn("snapshot: open failed", "err", err)
		http.Error(w, "snapshot failed", http.StatusInternalServerError)
		return
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		slog.Warn("snapshot: stat failed", "err", err)
		http.Error(w, "snapshot failed", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/x-sqlite3")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", fi.Size()))
	if _, err := io.Copy(w, f); err != nil {
		// Headers are already sent; nothing to report to the client.
		slog.Debug("snapshot: stream interrupted", "err", err)
	}
}
