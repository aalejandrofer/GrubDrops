package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
)

// seedV15 builds a DB at schema version 15 with one account, game and claim.
func seedV15(t *testing.T, path string) map[string]int {
	t.Helper()
	ctx := context.Background()
	db, err := sql.Open("sqlite", fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)", path))
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	goose.SetBaseFS(migrationsFS)
	require.NoError(t, goose.SetDialect("sqlite3"))
	require.NoError(t, goose.UpToContext(ctx, db, "migrations", 15))
	now := time.Now().Unix()
	for _, q := range []string{
		fmt.Sprintf(`INSERT INTO accounts (id,platform,display_name,status,fingerprint_json,enabled,created_at,updated_at) VALUES ('a1','twitch','Acc','idle','{}',1,%d,%d)`, now, now),
		`INSERT INTO games (id,name,slug,priority) VALUES ('g1','Fixture Game','fixture-game',100)`,
		`INSERT INTO global_games (game_id,rank) VALUES ('g1',1)`,
		`INSERT INTO account_games (account_id,game_id,rank) VALUES ('a1','g1',1)`,
		fmt.Sprintf(`INSERT INTO campaigns (id,platform,game,name,starts_at,ends_at,status,raw_json,discovered_at) VALUES ('c1','twitch','PUBG','PGS',%d,%d,'active','{}',%d)`, now-100, now+100, now),
		`INSERT INTO benefits (id,campaign_id,name,required_minutes,image_url) VALUES ('b1','c1','Watch Out',60,'')`,
		fmt.Sprintf(`INSERT INTO claims (id,account_id,benefit_id,claimed_at,value_meta_json) VALUES ('clm','a1','b1',%d,'{}')`, now),
		fmt.Sprintf(`INSERT INTO progress (account_id,benefit_id,minutes_watched,updated_at) VALUES ('a1','b1',5,%d)`, now),
	} {
		_, err := db.ExecContext(ctx, q)
		require.NoError(t, err, q)
	}
	counts := map[string]int{}
	for _, tbl := range []string{"claims", "progress", "games", "global_games", "account_games"} {
		counts[tbl] = count(t, db, tbl)
	}
	require.NoError(t, db.Close())
	return counts
}

func count(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var n int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM `+table).Scan(&n))
	return n
}

func openReadOnly(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestOpen_BacksUpBeforeV2(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "m.db")
	seedV15(t, path)

	db, err := Open(ctx, path)
	require.NoError(t, err)
	defer db.Close()

	backup := path + ".pre-v2"
	_, err = os.Stat(backup)
	require.NoError(t, err, "backup must exist")

	bk := openReadOnly(t, backup)
	var v int
	require.NoError(t, bk.QueryRow(`SELECT max(version_id) FROM goose_db_version`).Scan(&v))
	require.Equal(t, 15, v)
	require.Equal(t, 1, count(t, bk, "accounts"))

	require.NoError(t, db.QueryRow(`SELECT max(version_id) FROM goose_db_version`).Scan(&v))
	require.Equal(t, 23, v)
	require.Equal(t, 1, count(t, db, "claim_log"), "seed claim imported from 1.x")
}

func TestOpen_BackupNotOverwritten(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "m.db")
	seedV15(t, path)

	db, err := Open(ctx, path)
	require.NoError(t, err)
	backup := path + ".pre-v2"
	before, err := os.Stat(backup)
	require.NoError(t, err)
	beforeBytes, err := os.ReadFile(backup)
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, `INSERT INTO claim_log (account_id,benefit_id,platform,source,claimed_at) VALUES ('a1','b9','twitch','test',1)`)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	db2, err := Open(ctx, path)
	require.NoError(t, err)
	defer db2.Close()
	require.Equal(t, 2, count(t, db2, "claim_log"), "imported claim plus the inserted one")

	after, err := os.Stat(backup)
	require.NoError(t, err)
	require.Equal(t, before.ModTime(), after.ModTime())
	afterBytes, err := os.ReadFile(backup)
	require.NoError(t, err)
	require.Equal(t, beforeBytes, afterBytes)
	bk := openReadOnly(t, backup)
	var n int
	err = bk.QueryRow(`SELECT count(*) FROM claim_log`).Scan(&n)
	require.Error(t, err, "backup predates claim_log")
}

func TestOpen_FreshDBNoBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "m.db")
	db, err := Open(context.Background(), path)
	require.NoError(t, err)
	defer db.Close()
	_, err = os.Stat(path + ".pre-v2")
	require.True(t, os.IsNotExist(err))
}

func TestMigration0017_OldTablesUntouched(t *testing.T) {
	path := filepath.Join(t.TempDir(), "m.db")
	want := seedV15(t, path)
	db, err := Open(context.Background(), path)
	require.NoError(t, err)
	defer db.Close()
	for tbl, n := range want {
		require.Equal(t, n, count(t, db, tbl), tbl)
	}
}

func TestOpen_StaleBackupTmpIgnored(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "m.db")
	seedV15(t, path)
	require.NoError(t, os.WriteFile(path+".pre-v2.tmp", []byte("partial vacuum garbage"), 0o600))

	db, err := Open(ctx, path)
	require.NoError(t, err)
	defer db.Close()

	_, err = os.Stat(path + ".pre-v2.tmp")
	require.True(t, os.IsNotExist(err), "tmp must be gone")
	bk := openReadOnly(t, path+".pre-v2")
	var v int
	require.NoError(t, bk.QueryRow(`SELECT max(version_id) FROM goose_db_version`).Scan(&v))
	require.Equal(t, 15, v)
	require.Equal(t, 1, count(t, bk, "accounts"))
}
