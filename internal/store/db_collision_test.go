package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aalejandrofer/grubdrops/internal/store/gen"
	"github.com/stretchr/testify/require"
)

// gooseUp returns the Up section of a goose migration file.
func gooseUp(t *testing.T, raw []byte) string {
	t.Helper()
	up, _, ok := strings.Cut(string(raw), "-- +goose Down")
	require.True(t, ok, "migration has no Down section")
	return up
}

// seedV16 builds a DB at goose version 16 from seedV15 plus the given
// migration body, recorded as version 16 the way goose would.
func seedV16(t *testing.T, path, body string) {
	t.Helper()
	seedV15(t, path)
	db, err := sql.Open("sqlite", fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)", path))
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	_, err = db.Exec(body)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO goose_db_version (version_id, is_applied) VALUES (16, 1)`)
	require.NoError(t, err)
	require.NoError(t, db.Close())
}

func schemaVersion(t *testing.T, db *sql.DB) int {
	t.Helper()
	var v int
	require.NoError(t, db.QueryRow(`SELECT max(version_id) FROM goose_db_version`).Scan(&v))
	return v
}

// The 1.x branch feat/pipeline-v2 shipped its own 0016. A DB at that version
// must still get the v2 collection tables.
func TestOpen_1xPipelineV16GetsV2Tables(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "m.db")
	raw, err := os.ReadFile("testdata/0016_pipeline_v2_1x.sql")
	require.NoError(t, err)
	seedV16(t, path, gooseUp(t, raw))

	db, err := Open(ctx, path)
	require.NoError(t, err)
	defer db.Close()

	require.Equal(t, 23, schemaVersion(t, db))
	for _, tbl := range []string{"chosen_games", "claim_log", "drop_states", "watch_minutes", "account_scans", "drop_state"} {
		count(t, db, tbl) // fails if the table is missing
	}
	c := NewChosenGames(db, gen.New(db))
	require.NoError(t, c.Set(ctx, []string{"g1"}))
	got, err := c.List(ctx)
	require.NoError(t, err)
	require.Equal(t, []ChosenGame{{ID: "g1", Name: "Fixture Game", Slug: "fixture-game"}}, got)

	bk := openReadOnly(t, path+".pre-v2")
	require.Equal(t, 16, schemaVersion(t, bk))
	require.Equal(t, 1, count(t, bk, "accounts"))
}

// A DB that already ran the v2 collection migration as 0016 upgrades to 17
// and keeps its rows.
func TestOpen_V2At16UpgradesTo17(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "m.db")
	raw, err := migrationsFS.ReadFile("migrations/0017_v2_collection.sql")
	require.NoError(t, err)
	seedV16(t, path, gooseUp(t, raw)+"\nINSERT INTO chosen_games (game_id, rank) VALUES ('g1', 0);")

	db, err := Open(ctx, path)
	require.NoError(t, err)
	defer db.Close()

	require.Equal(t, 23, schemaVersion(t, db))
	got, err := NewChosenGames(db, gen.New(db)).List(ctx)
	require.NoError(t, err)
	require.Len(t, got, 1)
}
