package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

func TestSettings_RoundTrip(t *testing.T) {
	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "t.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	s := NewSettings(gen.New(db))

	url, err := s.GlobalDiscordWebhook(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "", url)

	require.NoError(t, s.SetGlobalDiscordWebhook(context.Background(), "https://discord.example/wh/abc"))
	url, err = s.GlobalDiscordWebhook(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "https://discord.example/wh/abc", url)
}

func TestSettings_LogRetentionDays(t *testing.T) {
	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "t.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	s := NewSettings(gen.New(db))

	days, err := s.LogRetentionDays(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 7, days) // default

	require.NoError(t, s.SetLogRetentionDays(context.Background(), 30))
	days, err = s.LogRetentionDays(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 30, days)
}

func TestSettings_KickWatchMode(t *testing.T) {
	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "t.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	s := NewSettings(gen.New(db))
	ctx := context.Background()

	// Default is the verified browser path.
	mode, err := s.KickWatchMode(ctx)
	require.NoError(t, err)
	assert.Equal(t, KickWatchModeBrowser, mode)

	// Round-trips the experimental WS path.
	require.NoError(t, s.SetKickWatchMode(ctx, KickWatchModeWS))
	mode, err = s.KickWatchMode(ctx)
	require.NoError(t, err)
	assert.Equal(t, KickWatchModeWS, mode)

	// Unknown values are coerced back to the safe default on both write…
	require.NoError(t, s.SetKickWatchMode(ctx, "garbage"))
	mode, err = s.KickWatchMode(ctx)
	require.NoError(t, err)
	assert.Equal(t, KickWatchModeBrowser, mode)
}
