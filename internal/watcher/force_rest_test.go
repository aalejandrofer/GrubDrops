package watcher

import (
	"context"
	"testing"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/platform/platformtest"
	"github.com/stretchr/testify/require"
)

// At WatchPercent p an account watches a session, then rests
// session*(100-p)/p, and force-watch stays off until the rest is over.
func TestForceWatchRestsAfterSession(t *testing.T) {
	old := forceSessionLength
	forceSessionLength = func() time.Duration { return 90 * time.Minute }
	t.Cleanup(func() { forceSessionLength = old })

	ctx := context.Background()
	backend := &idleBackend{platformtest.New()}
	w := New(Config{
		AccountID: "acct", Backend: backend, Session: platform.Session{AccessToken: "tok"},
		ForceWatcher: fakeForce{channel: "streamer"},
	})
	start := func(accrued time.Duration) {
		w.mu.Lock()
		w.forceTask = &ForceTask{Channel: "streamer", WatchPercent: 75}
		w.currentStream = &platform.Stream{Channel: "streamer", ChannelID: "1"}
		w.handle = &platform.WatchHandle{Channel: "streamer"}
		w.watchStartedAt = time.Now().Add(-2 * time.Minute)
		w.forceWatched = accrued
		w.mu.Unlock()
		w.setState(ctx, StateForceWatch)
	}

	// 82 of 90 min used: keep watching.
	start(80 * time.Minute)
	require.NoError(t, w.forceWatch(ctx))
	require.Equal(t, StateForceWatch, w.State())

	// 91 of 90 min used: rest 90*25/75 = 30 min, so 75% of the time is spent watching.
	start(89 * time.Minute)
	require.NoError(t, w.forceWatch(ctx))
	require.Equal(t, StatePickCampaign, w.State())
	until := w.Snapshot().ForceRestUntil
	require.WithinDuration(t, time.Now().Add(30*time.Minute), until, time.Minute)
	require.False(t, w.maybeStartForceTask(ctx), "no force-watch while resting")

	// Rest over: the next session starts fresh.
	w.mu.Lock()
	w.forceRestUntil = time.Now().Add(-time.Second)
	w.mu.Unlock()
	require.True(t, w.maybeStartForceTask(ctx))
	w.mu.Lock()
	require.Zero(t, w.forceWatched)
	w.mu.Unlock()
}

func TestForceWatchNeverRestsAt100Percent(t *testing.T) {
	ctx := context.Background()
	w := New(Config{AccountID: "acct", Backend: &idleBackend{platformtest.New()}, Session: platform.Session{AccessToken: "tok"}})
	w.mu.Lock()
	w.forceTask = &ForceTask{Channel: "streamer", WatchPercent: 100}
	w.currentStream = &platform.Stream{Channel: "streamer", ChannelID: "1"}
	w.handle = &platform.WatchHandle{Channel: "streamer"}
	w.watchStartedAt = time.Now().Add(-10 * time.Hour)
	w.mu.Unlock()
	w.setState(ctx, StateForceWatch)

	require.NoError(t, w.forceWatch(ctx))
	require.True(t, w.Snapshot().ForceRestUntil.IsZero())
}
