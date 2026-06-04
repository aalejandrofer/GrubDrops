package fake

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chano-fernandez/rust-drops-miner/internal/platform"
)

func TestFake_LifecycleClaims(t *testing.T) {
	ctx := context.Background()
	b := New(WithFastTime())

	challenge, err := b.StartDeviceLogin(ctx)
	require.NoError(t, err)

	sess, err := b.PollDeviceLogin(ctx, challenge)
	require.NoError(t, err)
	assert.NotEmpty(t, sess.AccessToken)

	campaigns, err := b.ListActiveCampaigns(ctx, sess)
	require.NoError(t, err)
	require.NotEmpty(t, campaigns)
	require.NotEmpty(t, campaigns[0].Benefits)

	streams, err := b.ListEligibleChannels(ctx, sess, campaigns[0])
	require.NoError(t, err)
	require.NotEmpty(t, streams)

	h, err := b.StartWatch(ctx, sess, streams[0])
	require.NoError(t, err)

	for i := 0; i < campaigns[0].Benefits[0].RequiredMinutes; i++ {
		require.NoError(t, b.Heartbeat(ctx, h))
	}

	progress, err := b.InventoryProgress(ctx, sess)
	require.NoError(t, err)
	require.NotEmpty(t, progress)
	assert.Equal(t, campaigns[0].Benefits[0].RequiredMinutes, progress[0].MinutesWatched)

	require.NoError(t, b.Claim(ctx, sess, campaigns[0].Benefits[0]))
	require.NoError(t, b.StopWatch(ctx, h))

	_ = platform.Session{}
}
