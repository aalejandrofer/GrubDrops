package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpen_RunsMigrations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := Open(context.Background(), path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	row := db.QueryRowContext(context.Background(),
		`SELECT name FROM sqlite_master WHERE type='table' AND name='accounts'`)
	var name string
	require.NoError(t, row.Scan(&name))
	assert.Equal(t, "accounts", name)
}

func TestOpen_SeedsRustGame(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := Open(context.Background(), path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	row := db.QueryRowContext(context.Background(),
		`SELECT priority FROM games WHERE slug='rust'`)
	var p int
	require.NoError(t, row.Scan(&p))
	assert.Equal(t, 0, p)
}
