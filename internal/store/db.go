package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// firstV2Migration is the first migration of the v2 schema. A database below
// it gets a one-time copy before the v2 tables are added. It is 17, not 16:
// the 1.x branch feat/pipeline-v2 shipped its own 0016, so deployed databases
// can sit at 16 without any v2 table.
const firstV2Migration = 17

func Open(ctx context.Context, path string) (*sql.DB, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// SQLite WAL allows one writer; serialize all access through a single
	// connection to avoid SQLITE_BUSY storms under concurrent goroutines.
	db.SetMaxOpenConns(1)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}

	goose.SetBaseFS(migrationsFS)
	if err := goose.SetDialect("sqlite3"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("set goose dialect: %w", err)
	}
	if err := backupBeforeV2(ctx, db, path); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := goose.UpContext(ctx, db, "migrations"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	if err := ImportLegacy(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

// backupBeforeV2 writes path+".pre-v2" once, when an existing database is
// below firstV2Migration. VACUUM INTO is WAL-safe. It writes a .tmp file and
// renames it, so a failed or killed vacuum leaves no .pre-v2 and the next boot
// retries. A failure aborts Open: never migrate without the copy.
func backupBeforeV2(ctx context.Context, db *sql.DB, path string) error {
	ver, err := goose.GetDBVersionContext(ctx, db)
	if err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if ver <= 0 || ver >= firstV2Migration {
		return nil
	}
	backup := path + ".pre-v2"
	if _, err := os.Stat(backup); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat pre-v2 backup: %w", err)
	}
	tmp := backup + ".tmp"
	if err := os.Remove(tmp); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove stale pre-v2 tmp: %w", err)
	}
	if _, err := db.ExecContext(ctx, "VACUUM INTO '"+strings.ReplaceAll(tmp, "'", "''")+"'"); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("pre-v2 backup: %w", err)
	}
	if err := os.Rename(tmp, backup); err != nil {
		return fmt.Errorf("pre-v2 backup rename: %w", err)
	}
	slog.Info("store pre-v2 backup written", "kind", "state", "path", backup)
	return nil
}
