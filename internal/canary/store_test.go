package canary

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/store"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

func TestResultStore_RoundTrip(t *testing.T) {
	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "t.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	q := gen.New(db)
	ctx := context.Background()

	_, ok, err := LoadResult(ctx, q, "twitch")
	require.NoError(t, err)
	assert.False(t, ok)

	require.NoError(t, SaveResult(ctx, q, "twitch", Result{OK: true, Detail: "2 beacons accepted"}))

	got, ok, err := LoadResult(ctx, q, "twitch")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.True(t, got.OK)
	assert.Equal(t, "2 beacons accepted", got.Detail)
	assert.False(t, got.CheckedAt.IsZero())
}
