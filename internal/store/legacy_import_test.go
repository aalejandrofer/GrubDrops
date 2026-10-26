package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/aalejandrofer/grubdrops/internal/store/gen"
	"github.com/stretchr/testify/require"
)

func rawDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)", path))
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	return db
}

func execAll(t *testing.T, db *sql.DB, stmts ...string) {
	t.Helper()
	for _, q := range stmts {
		_, err := db.Exec(q)
		require.NoError(t, err, q)
	}
}

// legacyFixture replaces the seedV15 game and progress rows with the data the
// import tests share.
func legacyFixture(t *testing.T, path string) {
	t.Helper()
	db := rawDB(t, path)
	defer db.Close()
	execAll(t, db,
		`INSERT OR IGNORE INTO games (id,name,slug,priority) VALUES ('g_rust','Rust','rust',0)`,
		`INSERT OR IGNORE INTO games (id,name,slug,priority) VALUES ('g_tarkov','Tarkov','tarkov-fixture',100)`,
		`INSERT OR IGNORE INTO games (id,name,slug,priority) VALUES ('g_apex','Apex Legends','apex-legends',100)`,
		`INSERT OR IGNORE INTO games (id,name,slug,priority) VALUES ('g_valorant','Valorant','valorant',100)`,
		`DELETE FROM global_games`,
		`DELETE FROM account_games`,
		`INSERT INTO accounts (id,platform,display_name,status,fingerprint_json,enabled,created_at,updated_at) VALUES ('a2','kick','Two','idle','{}',1,0,0)`,
		`INSERT INTO global_games (game_id,rank) VALUES ('g_rust',0),('g_tarkov',1)`,
		`INSERT INTO account_games (account_id,game_id,rank) VALUES ('a1','g_tarkov',0),('a1','g_valorant',2),('a2','g_apex',1)`,
		`UPDATE claims SET value_meta_json='{"code":"ABC"}' WHERE id='clm'`,
		`UPDATE progress SET minutes_watched=30 WHERE account_id='a1' AND benefit_id='b1'`,
		`INSERT INTO benefits (id,campaign_id,name,required_minutes,image_url) VALUES ('b2','c1','Second',60,'')`,
		`INSERT INTO progress (account_id,benefit_id,minutes_watched,updated_at) VALUES ('a1','b2',45,0)`,
	)
}

func chosenIDs(t *testing.T, db *sql.DB) []string {
	t.Helper()
	list, err := NewChosenGames(db, gen.New(db)).List(context.Background())
	require.NoError(t, err)
	ids := []string{}
	for _, g := range list {
		ids = append(ids, g.ID)
	}
	return ids
}

func totalMinutes(t *testing.T, db *sql.DB) int {
	t.Helper()
	n, err := NewWatchMinutes(gen.New(db)).Total(context.Background())
	require.NoError(t, err)
	return n
}

func assertLegacyResult(t *testing.T, db *sql.DB) {
	t.Helper()
	require.Equal(t, []string{"g_rust", "g_tarkov", "g_apex", "g_valorant"}, chosenIDs(t, db))
	var code, source, game, name, platform string
	require.NoError(t, db.QueryRow(`SELECT code, source, game, campaign_name, platform FROM claim_log WHERE benefit_id='b1'`).
		Scan(&code, &source, &game, &name, &platform))
	require.Equal(t, "ABC", code)
	require.Equal(t, ClaimSourceLegacy, source)
	require.Equal(t, "PUBG", game)
	require.Equal(t, "PGS", name)
	require.Equal(t, "twitch", platform)
	require.Equal(t, 75, totalMinutes(t, db))
}

func TestImportLegacy_V15(t *testing.T) {
	path := filepath.Join(t.TempDir(), "m.db")
	seedV15(t, path)
	legacyFixture(t, path)
	db, err := Open(context.Background(), path)
	require.NoError(t, err)
	defer db.Close()
	assertLegacyResult(t, db)
}

func TestImportLegacy_V16(t *testing.T) {
	path := filepath.Join(t.TempDir(), "m.db")
	raw, err := os.ReadFile("testdata/0016_pipeline_v2_1x.sql")
	require.NoError(t, err)
	seedV16(t, path, gooseUp(t, raw))
	legacyFixture(t, path)
	db, err := Open(context.Background(), path)
	require.NoError(t, err)
	defer db.Close()
	assertLegacyResult(t, db)
}

func TestImportLegacy_ClaimFallbacks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "m.db")
	seedV15(t, path)
	raw := rawDB(t, path)
	execAll(t, raw,
		`PRAGMA foreign_keys=OFF`,
		`INSERT INTO claims (id,account_id,benefit_id,claimed_at,value_meta_json) VALUES ('orphan','a1','gone',5,'{}')`,
		`UPDATE claims SET value_meta_json='not json' WHERE id='clm'`,
	)
	require.NoError(t, raw.Close())

	db, err := Open(context.Background(), path)
	require.NoError(t, err)
	defer db.Close()

	var platform, game, code string
	require.NoError(t, db.QueryRow(`SELECT platform, game FROM claim_log WHERE benefit_id='gone'`).Scan(&platform, &game))
	require.Equal(t, "twitch", platform)
	require.Equal(t, "", game)
	require.NoError(t, db.QueryRow(`SELECT code FROM claim_log WHERE benefit_id='b1'`).Scan(&code))
	require.Equal(t, "", code)
}

func TestImportLegacy_KeepsExistingChosen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "m.db")
	seedV15(t, path)
	legacyFixture(t, path)
	db, err := Open(ctx, path)
	require.NoError(t, err)
	execAll(t, db,
		`DELETE FROM kv WHERE key='`+legacyImportKey+`'`,
		`DELETE FROM claim_log`,
		`DELETE FROM chosen_games`,
		`INSERT INTO chosen_games (game_id,rank) VALUES ('g_apex',0)`,
	)
	require.NoError(t, db.Close())

	db, err = Open(ctx, path)
	require.NoError(t, err)
	defer db.Close()
	require.Equal(t, []string{"g_apex"}, chosenIDs(t, db))
	require.Equal(t, 1, count(t, db, "claim_log"))
}

func TestImportLegacy_RunsOnce(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "m.db")
	seedV15(t, path)
	legacyFixture(t, path)
	db, err := Open(ctx, path)
	require.NoError(t, err)
	require.Equal(t, 75, totalMinutes(t, db))
	execAll(t, db, `DELETE FROM chosen_games WHERE game_id='g_rust'`)
	require.NoError(t, db.Close())

	db, err = Open(ctx, path)
	require.NoError(t, err)
	defer db.Close()
	require.Equal(t, []string{"g_tarkov", "g_apex", "g_valorant"}, chosenIDs(t, db))
	require.Equal(t, 75, totalMinutes(t, db))
}

func TestImportLegacy_FreshDB(t *testing.T) {
	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "m.db"))
	require.NoError(t, err)
	defer db.Close()
	require.Equal(t, 0, count(t, db, "chosen_games"))
	require.Equal(t, 0, count(t, db, "claim_log"))
	var v string
	require.NoError(t, db.QueryRow(`SELECT value FROM kv WHERE key=?`, legacyImportKey).Scan(&v))
	require.Equal(t, "1", v)
}
