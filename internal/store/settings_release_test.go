package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

func TestSettings_ReleaseAccessors(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	s := NewSettings(gen.New(db))

	// Defaults before anything is set.
	v, err := s.LatestRelease(ctx)
	require.NoError(t, err)
	require.Equal(t, "", v)
	ts, err := s.LastReleaseCheck(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(0), ts)

	require.NoError(t, s.SetLatestRelease(ctx, "v1.3.5"))
	require.NoError(t, s.SetLastReleaseCheck(ctx, 1751200000))

	v, err = s.LatestRelease(ctx)
	require.NoError(t, err)
	require.Equal(t, "v1.3.5", v)
	ts, err = s.LastReleaseCheck(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(1751200000), ts)
}
