package store

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
)

func migratedTo(t *testing.T, version int64) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "m.db")
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)", path)
	db, err := sql.Open("sqlite", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	db.SetMaxOpenConns(1)
	goose.SetBaseFS(migrationsFS)
	require.NoError(t, goose.SetDialect("sqlite3"))
	require.NoError(t, goose.UpToContext(context.Background(), db, "migrations", version))
	return db
}

// Migration 0024 renames the seeded Overwatch 2 row to the name Twitch and
// Kick both use, keeping its id so lists that hold it stay put.
func TestMigration0024RenamesOverwatch(t *testing.T) {
	ctx := context.Background()
	db := migratedTo(t, 23)
	require.NoError(t, goose.UpToContext(ctx, db, "migrations", 24))
	var name, slug, twitchName string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT name, slug, twitch_name FROM games WHERE id = 'g_overwatch'`).Scan(&name, &slug, &twitchName))
	require.Equal(t, "Overwatch", name)
	require.Equal(t, "overwatch", slug)
	require.Equal(t, "", twitchName)
}

// A user who already added Overwatch under its own row keeps both rows as
// they were, so the slug stays unique.
func TestMigration0024SkipsTakenSlug(t *testing.T) {
	ctx := context.Background()
	db := migratedTo(t, 23)
	_, err := db.ExecContext(ctx, `INSERT INTO games (id, name, slug) VALUES ('g_ow_user', 'Overwatch', 'overwatch')`)
	require.NoError(t, err)
	require.NoError(t, goose.UpToContext(ctx, db, "migrations", 24))
	var name string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT name FROM games WHERE id = 'g_overwatch'`).Scan(&name))
	require.Equal(t, "Overwatch 2", name)
}
