package store

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

// TestMigration0011DedupesClaims proves the claims fix: 0011 collapses
// pre-existing duplicate (account_id, benefit_id) rows to one, and the
// unique index + upserting InsertClaim stop new duplicates from forming
// (the bug behind the 80x COLLECTED chips + repeated Past rows).
func TestMigration0011DedupesClaims(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "m.db")
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)", path)
	db, err := sql.Open("sqlite", dsn)
	require.NoError(t, err)
	defer db.Close()
	db.SetMaxOpenConns(1)

	goose.SetBaseFS(migrationsFS)
	require.NoError(t, goose.SetDialect("sqlite3"))

	// Migrate to v10 (before the dedupe), then seed duplicate claims.
	require.NoError(t, goose.UpToContext(ctx, db, "migrations", 10))

	now := time.Now().Unix()
	exec := func(q string, args ...any) {
		_, e := db.ExecContext(ctx, q, args...)
		require.NoError(t, e)
	}
	exec(`INSERT INTO accounts (id,platform,display_name,status,fingerprint_json,enabled,created_at,updated_at) VALUES ('a1','twitch','Acc','idle','{}',1,?,?)`, now, now)
	exec(`INSERT INTO campaigns (id,platform,game,name,starts_at,ends_at,status,raw_json,discovered_at) VALUES ('c1','twitch','PUBG','PGS',?,?,'active','{}',?)`, now-100, now+100, now)
	exec(`INSERT INTO benefits (id,campaign_id,name,required_minutes,image_url) VALUES ('b1','c1','Watch Out',60,'')`)
	// 5 duplicate claim rows for the same (account, benefit).
	for i := 0; i < 5; i++ {
		exec(`INSERT INTO claims (id,account_id,benefit_id,claimed_at,value_meta_json) VALUES (?, 'a1','b1',?, '{}')`, fmt.Sprintf("clm_%d", i), now)
	}

	var n int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM claims`).Scan(&n))
	require.Equal(t, 5, n, "5 dup claims seeded pre-migration")

	// Apply 0011 (dedupe + unique index).
	require.NoError(t, goose.UpContext(ctx, db, "migrations"))
	require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM claims`).Scan(&n))
	require.Equal(t, 1, n, "0011 should collapse to a single claim row")

	// New InsertClaim for the same pair must upsert, not duplicate.
	q := gen.New(db)
	for i := 0; i < 3; i++ {
		require.NoError(t, q.InsertClaim(ctx, gen.InsertClaimParams{
			ID: fmt.Sprintf("new_%d", i), AccountID: "a1", BenefitID: "b1", ClaimedAt: now + int64(i), ValueMetaJson: "{}",
		}))
	}
	require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM claims`).Scan(&n))
	require.Equal(t, 1, n, "upserting InsertClaim must not create duplicates")
}
