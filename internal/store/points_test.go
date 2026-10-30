package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

func TestPointsSettingsDefaultsAndRoundTrip(t *testing.T) {
	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "points.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	q := gen.New(db)
	ctx := context.Background()

	account, err := LoadPointsAccountSettings(ctx, q, "a1")
	require.NoError(t, err)
	require.Equal(t, PointsAccountSettings{Enabled: true, Streams: 2}, account)
	account = PointsAccountSettings{Enabled: false, Only: true, Streams: 7}
	require.NoError(t, SavePointsAccountSettings(ctx, q, "a1", account))
	gotAccount, err := LoadPointsAccountSettings(ctx, q, "a1")
	require.NoError(t, err)
	require.Equal(t, account, gotAccount)

	safety, err := LoadPointsSafety(ctx, q)
	require.NoError(t, err)
	require.Equal(t, PointsSafety{SleepEnabled: true, SleepStart: 120, SleepEnd: 480, FollowRaids: true}, safety)
	safety = PointsSafety{SleepEnabled: false, SleepStart: 90, SleepEnd: 510, FollowRaids: false}
	require.NoError(t, SavePointsSafety(ctx, q, safety))
	gotSafety, err := LoadPointsSafety(ctx, q)
	require.NoError(t, err)
	require.Equal(t, safety, gotSafety)
}
