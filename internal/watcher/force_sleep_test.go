package watcher

import (
	"context"
	"testing"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/platform/platformtest"
	"github.com/stretchr/testify/require"
)

// Inside the sleep window a running force-watch stops, force-watch will not
// start, and it starts again once the window is over.
func TestForceWatchHonoursSleepWindow(t *testing.T) {
	ctx := context.Background()
	asleep := false
	end := time.Now().Add(3 * time.Hour)
	w := New(Config{
		AccountID: "acct", Backend: &idleBackend{platformtest.New()},
		Session:      platform.Session{AccessToken: "tok"},
		ForceWatcher: fakeForce{channel: "streamer"},
		SleepUntil: func(time.Time) (time.Time, bool) {
			if asleep {
				return end, true
			}
			return time.Time{}, false
		},
	})
	w.mu.Lock()
	w.forceTask = &ForceTask{Channel: "streamer"}
	w.currentStream = &platform.Stream{Channel: "streamer", ChannelID: "1"}
	w.handle = &platform.WatchHandle{Channel: "streamer"}
	w.watchStartedAt = time.Now().Add(-time.Minute)
	w.mu.Unlock()
	w.setState(ctx, StateForceWatch)

	require.NoError(t, w.forceWatch(ctx))
	require.Equal(t, StateForceWatch, w.State(), "awake: keeps watching")

	asleep = true
	require.NoError(t, w.forceWatch(ctx))
	require.Equal(t, StatePickCampaign, w.State(), "bedtime: stops")
	require.WithinDuration(t, end, w.Snapshot().ForceRestUntil, time.Second, "UI shows when it wakes")
	require.False(t, w.maybeStartForceTask(ctx), "no force-watch while asleep")

	asleep = false
	require.True(t, w.maybeStartForceTask(ctx), "starts again after the window")
}
