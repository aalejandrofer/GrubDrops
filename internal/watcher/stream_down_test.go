package watcher

import (
	"context"
	"testing"

	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/platform/platformtest"
	"github.com/stretchr/testify/require"
)

func TestForceWatchRestartsAfterStreamDown(t *testing.T) {
	ctx := context.Background()
	backend := &idleBackend{platformtest.New()}
	w := New(Config{
		AccountID:    "acct",
		Backend:      backend,
		Session:      platform.Session{AccessToken: "token"},
		ForceWatcher: fakeForce{channel: "streamer"},
	})
	w.mu.Lock()
	w.forceTask = &ForceTask{Channel: "streamer"}
	w.currentStream = &platform.Stream{Channel: "streamer", ChannelID: "123"}
	w.handle = &platform.WatchHandle{Channel: "streamer"}
	w.mu.Unlock()
	w.setState(ctx, StateForceWatch)

	w.handlePubSubStreamDown("123")
	require.Equal(t, StateForceWatch, w.State())
	require.NoError(t, w.step(ctx))

	w.mu.Lock()
	defer w.mu.Unlock()
	require.NotNil(t, w.handle)
	require.Equal(t, "streamer", w.currentStream.Channel)
}
