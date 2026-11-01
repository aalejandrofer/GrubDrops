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

// Migration 0026 adds points_mode, off for every account, including accounts
// already on a Force watch channel, and keeps their assignments.
func TestMigration0026PointsModeStartsOff(t *testing.T) {
	ctx := context.Background()
	db := migratedTo(t, 25)
	for _, id := range []string{"a_force", "a_plain", "a_both"} {
		_, err := db.ExecContext(ctx, `INSERT INTO accounts (id, platform, display_name, status, fingerprint_json, enabled, created_at, updated_at)
			VALUES (?, 'twitch', ?, 'idle', '{}', 1, 1, 1)`, id, id)
		require.NoError(t, err)
	}
	_, err := db.ExecContext(ctx, `INSERT INTO force_channels (id, platform, channel, rank, created_at) VALUES (1, 'twitch', 'one', 0, 1), (2, 'twitch', 'two', 1, 1)`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO force_channel_accounts (channel_id, account_id) VALUES (1, 'a_force'), (1, 'a_both'), (2, 'a_both')`)
	require.NoError(t, err)

	require.NoError(t, goose.UpToContext(ctx, db, "migrations", 26))
	want := map[string]string{"a_force": "off", "a_both": "off", "a_plain": "off"}
	for id, mode := range want {
		var got string
		require.NoError(t, db.QueryRowContext(ctx, `SELECT points_mode FROM accounts WHERE id = ?`, id).Scan(&got))
		require.Equal(t, mode, got, id)
	}
	var n int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM force_channel_accounts`).Scan(&n))
	require.Equal(t, 3, n, "assignments stay")

	require.NoError(t, goose.DownToContext(ctx, db, "migrations", 25))
	_, err = db.ExecContext(ctx, `SELECT points_mode FROM accounts`)
	require.Error(t, err, "down drops the column")
}
