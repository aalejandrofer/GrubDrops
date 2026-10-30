package api

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/store/gen"
	"github.com/aalejandrofer/grubdrops/internal/watcher"
)

func TestChannelPointsCacheIsPerAccountAndChannelAndRefreshesHourly(t *testing.T) {
	ctx := context.Background()
	cache := &channelPointsCache{}
	var calls atomic.Int32
	fetch := func(_ context.Context) (int, error) {
		return int(calls.Add(1) * 100), nil
	}
	await := func(accountID, channel string, balance int) channelPointsEntry {
		var got channelPointsEntry
		require.Eventually(t, func() bool {
			var ok bool
			got, ok = cache.get(ctx, accountID, channel, fetch)
			return ok && got.Balance == balance
		}, 2*time.Second, time.Millisecond)
		return got
	}

	first, ok := cache.get(ctx, "account-1", "Streamer", fetch)
	assert.False(t, ok, "a cache miss should refresh in the background")
	first = await("account-1", "Streamer", 100)
	assert.Equal(t, 100, first.Balance)
	cached, ok := cache.get(ctx, "account-1", "streamer", fetch)
	require.True(t, ok)
	assert.Equal(t, 100, cached.Balance, "channel logins are case-insensitive and cached for an hour")
	assert.Equal(t, int32(1), calls.Load())

	_, ok = cache.get(ctx, "account-1", "another", fetch)
	assert.False(t, ok)
	assert.Equal(t, 200, await("account-1", "another", 200).Balance)
	_, ok = cache.get(ctx, "account-2", "streamer", fetch)
	assert.False(t, ok)
	assert.Equal(t, 300, await("account-2", "streamer", 300).Balance)

	key := "account-1\x00" + strings.ToLower("Streamer")
	cache.mu.Lock()
	entry := cache.entries[key]
	entry.FetchedAt = time.Now().Add(-2 * time.Hour)
	cache.entries[key] = entry
	cache.attempted[key] = entry.FetchedAt
	cache.mu.Unlock()

	stale, ok := cache.get(ctx, "account-1", "streamer", fetch)
	require.True(t, ok)
	assert.Equal(t, 100, stale.Balance, "keep the last successful value during a refresh")
	assert.Equal(t, 400, await("account-1", "streamer", 400).Balance)
	assert.Equal(t, int32(4), calls.Load())
}

func TestChannelBalancesKeepKickBalancesUnavailablePerChannel(t *testing.T) {
	d := dashboardDeps{}
	got := d.channelBalances(gen.Account{ID: "kick-1", Platform: "kick"}, watcher.Snapshot{
		Channel:       "streamer-a",
		ExtraChannels: []string{"streamer-b"},
	}, "en")

	require.Len(t, got, 2)
	assert.Equal(t, []string{"streamer-a", "streamer-b"}, []string{got[0].Name, got[1].Name})
	assert.False(t, got[0].PointsAvailable)
	assert.False(t, got[1].PointsAvailable)
}
