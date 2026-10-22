package watcher

import (
	"context"
	"testing"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/platform/platformtest"
	"github.com/stretchr/testify/require"
)

type raidJoinTestBackend struct {
	*platformtest.MockBackend
	joined chan string
}

func (b *raidJoinTestBackend) JoinRaid(_ context.Context, _ platform.Session, raidID string) error {
	b.joined <- raidID
	return nil
}

func TestWatcherJoinsRaidFromCurrentWatchedChannel(t *testing.T) {
	backend := &raidJoinTestBackend{MockBackend: platformtest.New(), joined: make(chan string, 1)}
	w := New(Config{AccountID: "acct", Backend: backend, Session: platform.Session{AccessToken: "tok"}})
	w.mu.Lock()
	w.currentStream = &platform.Stream{ChannelID: "source-1"}
	w.mu.Unlock()

	w.handlePubSubRaid("other-channel", "ignored")
	w.handlePubSubRaid("source-1", "raid-1")

	select {
	case raidID := <-backend.joined:
		require.Equal(t, "raid-1", raidID)
	case <-time.After(time.Second):
		t.Fatal("watcher did not join the raid from its current stream")
	}
}
