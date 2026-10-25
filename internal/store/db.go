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
// it gets a one-time copy before the v2 tables are added.
const firstV2Migration = 16

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
	return db, nil
}

// backupBeforeV2 writes path+".pre-v2" once, when an existing database is
// below firstV2Migration. VACUUM INTO is WAL-safe. A failure aborts Open:
// never migrate without the copy.
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
	if _, err := db.ExecContext(ctx, "VACUUM INTO '"+strings.ReplaceAll(backup, "'", "''")+"'"); err != nil {
		return fmt.Errorf("pre-v2 backup: %w", err)
	}
	slog.Info("store pre-v2 backup written", "kind", "state", "path", backup)
	return nil
}
