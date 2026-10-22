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

func pinRaidDelay(t *testing.T, d time.Duration) {
	t.Helper()
	old := raidJoinDelay
	raidJoinDelay = func() time.Duration { return d }
	t.Cleanup(func() { raidJoinDelay = old })
}

func newRaidWatcher(follow func() bool) (*Watcher, *raidJoinTestBackend) {
	backend := &raidJoinTestBackend{MockBackend: platformtest.New(), joined: make(chan string, 1)}
	w := New(Config{AccountID: "acct", Backend: backend, Session: platform.Session{AccessToken: "tok"}, FollowRaids: follow})
	w.mu.Lock()
	w.currentStream = &platform.Stream{ChannelID: "source-1"}
	w.mu.Unlock()
	return w, backend
}

func TestWatcherJoinsRaidFromCurrentWatchedChannel(t *testing.T) {
	pinRaidDelay(t, 0)
	w, backend := newRaidWatcher(nil)

	w.handlePubSubRaid("other-channel", "ignored")
	w.handlePubSubRaid("source-1", "raid-1")

	select {
	case raidID := <-backend.joined:
		require.Equal(t, "raid-1", raidID)
	case <-time.After(time.Second):
		t.Fatal("watcher did not join the raid from its current stream")
	}
}

func TestWatcherIgnoresRaidsWhenFollowRaidsOff(t *testing.T) {
	pinRaidDelay(t, 0)
	on := false
	w, backend := newRaidWatcher(func() bool { return on })

	w.handlePubSubRaid("source-1", "raid-off")
	select {
	case id := <-backend.joined:
		t.Fatalf("joined raid %q with follow-raids off", id)
	case <-time.After(150 * time.Millisecond):
	}

	on = true // read per raid, so the switch needs no reload
	w.handlePubSubRaid("source-1", "raid-on")
	select {
	case raidID := <-backend.joined:
		require.Equal(t, "raid-on", raidID)
	case <-time.After(time.Second):
		t.Fatal("watcher did not join the raid once follow-raids was on")
	}
}

func TestWatcherWaitsBeforeJoiningRaid(t *testing.T) {
	pinRaidDelay(t, 300*time.Millisecond)
	w, backend := newRaidWatcher(nil)

	start := time.Now()
	w.handlePubSubRaid("source-1", "raid-1")
	select {
	case <-backend.joined:
		require.GreaterOrEqual(t, time.Since(start), 300*time.Millisecond)
	case <-time.After(2 * time.Second):
		t.Fatal("raid was never joined")
	}
}
