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

func TestSettings_Canary(t *testing.T) {
	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "t.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	s := NewSettings(gen.New(db))
	ctx := context.Background()

	tw, err := s.CanaryTwitchChannel(ctx)
	require.NoError(t, err)
	assert.Equal(t, "alveussanctuary", tw)

	kk, err := s.CanaryKickChannel(ctx)
	require.NoError(t, err)
	assert.Equal(t, "", kk)

	iv, err := s.CanaryIntervalSec(ctx)
	require.NoError(t, err)
	assert.Equal(t, 6*3600, iv)

	require.NoError(t, s.SetCanaryTwitchChannel(ctx, "somechannel"))
	tw, _ = s.CanaryTwitchChannel(ctx)
	assert.Equal(t, "somechannel", tw)

	require.NoError(t, s.SetCanaryKickChannel(ctx, "kickchan"))
	kk, _ = s.CanaryKickChannel(ctx)
	assert.Equal(t, "kickchan", kk)

	require.NoError(t, s.SetCanaryIntervalSec(ctx, 1800))
	iv, _ = s.CanaryIntervalSec(ctx)
	assert.Equal(t, 1800, iv)

	// Explicit 0 disables the canary (Runner.Run treats <=0 as disabled).
	require.NoError(t, s.SetCanaryIntervalSec(ctx, 0))
	iv, err = s.CanaryIntervalSec(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, iv, "explicit 0 must disable (not default to 6h)")
}

func TestSettings_KickWatchMode(t *testing.T) {
	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "t.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	s := NewSettings(gen.New(db))
	ctx := context.Background()

	// Default is auto (WS first, Chrome fallback): WS needs no Docker so a fresh
	// install mines Kick on any Pi out of the box, and auto falls back to the
	// Chrome sidecar if WS stops accruing.
	mode, err := s.KickWatchMode(ctx)
	require.NoError(t, err)
	assert.Equal(t, KickWatchModeAuto, mode)

	// Round-trips the explicit browser path.
	require.NoError(t, s.SetKickWatchMode(ctx, KickWatchModeBrowser))
	mode, err = s.KickWatchMode(ctx)
	require.NoError(t, err)
	assert.Equal(t, KickWatchModeBrowser, mode)

	// Round-trips the experimental WS-only path.
	require.NoError(t, s.SetKickWatchMode(ctx, KickWatchModeWS))
	mode, err = s.KickWatchMode(ctx)
	require.NoError(t, err)
	assert.Equal(t, KickWatchModeWS, mode)

	// Unknown values are coerced back to the auto default on write…
	require.NoError(t, s.SetKickWatchMode(ctx, "garbage"))
	mode, err = s.KickWatchMode(ctx)
	require.NoError(t, err)
	assert.Equal(t, KickWatchModeAuto, mode)
}
