package store

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

func newAccount(t *testing.T, q *gen.Queries, id string, created int64) {
	t.Helper()
	_, err := q.CreateAccount(context.Background(), gen.CreateAccountParams{
		ID: id, Platform: "kick", DisplayName: id,
		Status: "idle", FingerprintJson: "{}", Enabled: 1, CreatedAt: created, UpdatedAt: created,
	})
	require.NoError(t, err)
}

func accountIDs(as []gen.Account) []string {
	out := make([]string, 0, len(as))
	for _, a := range as {
		out = append(out, a.ID)
	}
	return out
}

func TestAccountSortOrder(t *testing.T) {
	q := gen.New(openTest(t))
	ctx := context.Background()
	// Same created_at: new accounts must still land last.
	newAccount(t, q, "a1", 100)
	newAccount(t, q, "a2", 100)
	newAccount(t, q, "a3", 100)
	all, err := q.ListAllAccounts(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"a1", "a2", "a3"}, accountIDs(all))

	for i, id := range []string{"a3", "a1", "a2"} {
		require.NoError(t, q.SetAccountSortOrder(ctx, gen.SetAccountSortOrderParams{SortOrder: int64(i + 1), ID: id}))
	}
	all, err = q.ListAllAccounts(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"a3", "a1", "a2"}, accountIDs(all))
	en, err := q.ListEnabledAccounts(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"a3", "a1", "a2"}, accountIDs(en))

	newAccount(t, q, "a4", 50)
	all, err = q.ListAllAccounts(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"a3", "a1", "a2", "a4"}, accountIDs(all))
}

func TestAccountSortOrderBackfill(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "b.db")
	db, err := sql.Open("sqlite", fmt.Sprintf("file:%s?_pragma=foreign_keys(ON)", path))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	goose.SetBaseFS(migrationsFS)
	require.NoError(t, goose.SetDialect("sqlite3"))
	require.NoError(t, goose.UpToContext(ctx, db, "migrations", 20))
	now := time.Now().Unix()
	// Inserted out of creation order.
	for _, r := range []struct {
		id string
		at int64
	}{{"late", now + 20}, {"early", now}, {"mid", now + 10}} {
		_, err := db.ExecContext(ctx, `INSERT INTO accounts (id,platform,display_name,status,fingerprint_json,enabled,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?)`,
			r.id, "kick", r.id, "idle", "{}", 1, r.at, r.at)
		require.NoError(t, err)
	}
	require.NoError(t, goose.UpContext(ctx, db, "migrations"))

	all, err := gen.New(db).ListAllAccounts(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"early", "mid", "late"}, accountIDs(all))
	assert.Equal(t, []int64{1, 2, 3}, []int64{all[0].SortOrder, all[1].SortOrder, all[2].SortOrder})
}
