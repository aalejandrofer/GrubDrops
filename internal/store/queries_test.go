package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/rust-drops-miner/internal/store/gen"
)

func openTest(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "t.db")
	db, err := Open(context.Background(), path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestQueries_AccountRoundtrip(t *testing.T) {
	db := openTest(t)
	q := gen.New(db)
	now := time.Now().Unix()

	acc, err := q.CreateAccount(context.Background(), gen.CreateAccountParams{
		ID:              "acc1",
		Platform:        "fake",
		Login:           "user1",
		DisplayName:     "User One",
		Status:          "idle",
		FingerprintJson: "{}",
		Enabled:         1,
		CreatedAt:       now,
		UpdatedAt:       now,
	})
	require.NoError(t, err)
	assert.Equal(t, "acc1", acc.ID)

	list, err := q.ListEnabledAccounts(context.Background())
	require.NoError(t, err)
	found := false
	for _, a := range list {
		if a.ID == "acc1" {
			found = true
			break
		}
	}
	assert.True(t, found, "acc1 not present in ListEnabledAccounts result")
}
