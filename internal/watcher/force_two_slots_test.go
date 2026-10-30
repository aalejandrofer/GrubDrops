package watcher

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/platform/platformtest"
	"github.com/stretchr/testify/require"
)

// slotBackend records watches per channel and refuses the offline ones.
type slotBackend struct {
	*platformtest.MockBackend
	mu      sync.Mutex
	offline map[string]bool
	beats   map[string]int
	stopped []string
	joined  chan string
}

func newSlotBackend(offline ...string) *slotBackend {
	b := &slotBackend{MockBackend: platformtest.New(), offline: map[string]bool{}, beats: map[string]int{}, joined: make(chan string, 4)}
	for _, o := range offline {
		b.offline[o] = true
	}
	return b
}

func (b *slotBackend) StartWatch(_ context.Context, _ platform.Session, s platform.Stream) (platform.WatchHandle, error) {
	if b.offline[s.Channel] {
		return platform.WatchHandle{}, errors.New("offline")
	}
	return platform.WatchHandle{Channel: s.Channel, ChannelID: "id-" + s.Channel}, nil
}

func (b *slotBackend) Heartbeat(_ context.Context, h platform.WatchHandle) error {
	b.mu.Lock()
	b.beats[h.Channel]++
	b.mu.Unlock()
	return nil
}

func (b *slotBackend) StopWatch(_ context.Context, h platform.WatchHandle) error {
	b.mu.Lock()
	b.stopped = append(b.stopped, h.Channel)
	b.mu.Unlock()
	return nil
}

func (b *slotBackend) JoinRaid(_ context.Context, _ platform.Session, raidID string) error {
	b.joined <- raidID
	return nil
}

func twoSlotWatcher(b *slotBackend, task ForceTask) *Watcher {
	w := New(Config{AccountID: "acct", Backend: b, Session: platform.Session{AccessToken: "tok"}})
	w.forceTask = &task
	w.setState(context.Background(), StateForceWatch)
	return w
}

// A Twitch force-watch runs two channels at once: the first two watchable ones
// in priority order, both heartbeated, both stopped together.
func TestForceWatch_TwoChannelsAtOnce(t *testing.T) {
	ctx := context.Background()
	b := newSlotBackend("top") // top is offline
	w := twoSlotWatcher(b, ForceTask{Channel: "top", Alternates: []string{"b", "c", "d"}, Slots: 2})

	require.NoError(t, w.forceWatch(ctx)) // starts the primary
	require.NoError(t, w.forceWatch(ctx)) // heartbeats it, starts the second
	snap := w.Snapshot()
	require.Equal(t, "b", snap.Channel, "offline top is skipped")
	require.Equal(t, []string{"c"}, snap.ExtraChannels)

	require.NoError(t, w.forceWatch(ctx))
	require.Equal(t, 2, b.beats["b"])
	require.Equal(t, 1, b.beats["c"], "the second channel is heartbeated too")

	w.stopCurrentWatch(ctx)
	require.ElementsMatch(t, []string{"b", "c"}, b.stopped)
	require.Empty(t, w.Snapshot().ExtraChannels)
}

// Secondary account settings can request more than two slots. Losing the
// primary promotes an active slot and the watcher fills the freed slot.
func TestForceWatch_ArbitrarySlotsAndPrimaryPromotion(t *testing.T) {
	ctx := context.Background()
	b := newSlotBackend()
	w := twoSlotWatcher(b, ForceTask{Channel: "a", Alternates: []string{"b", "c", "d"}, Slots: 3})
	for range 3 {
		require.NoError(t, w.forceWatch(ctx))
	}
	require.Equal(t, "a", w.Snapshot().Channel)
	require.Equal(t, []string{"b", "c"}, w.Snapshot().ExtraChannels)

	b.offline["a"] = true
	w.handlePubSubStreamDown("id-a")
	require.Equal(t, "b", w.Snapshot().Channel, "an active secondary replaces the failed primary")
	require.Equal(t, []string{"c"}, w.Snapshot().ExtraChannels)
	require.NoError(t, w.forceWatch(ctx))
	require.Equal(t, []string{"c", "d"}, w.Snapshot().ExtraChannels, "the freed slot is replenished without duplicating channels")

	w.stopCurrentWatch(ctx)
	require.ElementsMatch(t, []string{"a", "b", "c", "d"}, b.stopped)
}

func TestForceWatch_SingleSlotWhenNotTwitchOrOneChannel(t *testing.T) {
	ctx := context.Background()
	for name, task := range map[string]ForceTask{
		"kick (1 slot)":      {Channel: "a", Alternates: []string{"b"}, Slots: 1},
		"one channel":        {Channel: "a", Slots: 2},
		"all others offline": {Channel: "a", Alternates: []string{"x"}, Slots: 2},
	} {
		b := newSlotBackend("x")
		w := twoSlotWatcher(b, task)
		for i := 0; i < 3; i++ {
			require.NoError(t, w.forceWatch(ctx), name)
		}
		require.Empty(t, w.Snapshot().ExtraChannels, name)
	}
}

// A raid on the second channel is joined, and that channel going offline
// drops only the second slot; the primary keeps watching and a replacement is
// not looked for until the retry window passes.
func TestForceWatch_SecondChannelRaidAndStreamDown(t *testing.T) {
	pinRaidDelay(t, 0)
	ctx := context.Background()
	b := newSlotBackend()
	w := twoSlotWatcher(b, ForceTask{Channel: "a", Alternates: []string{"b"}, Slots: 2})
	for i := 0; i < 2; i++ {
		require.NoError(t, w.forceWatch(ctx))
	}
	require.Equal(t, []string{"b"}, w.Snapshot().ExtraChannels)

	w.handlePubSubRaid("id-b", "raid-b")
	select {
	case id := <-b.joined:
		require.Equal(t, "raid-b", id)
	case <-time.After(time.Second):
		t.Fatal("raid on the second channel was not joined")
	}

	w.handlePubSubStreamDown("id-b")
	require.Empty(t, w.Snapshot().ExtraChannels)
	require.Equal(t, "a", w.Snapshot().Channel, "primary unaffected")
	require.Equal(t, StateForceWatch, w.State())
	require.Equal(t, []string{"b"}, b.stopped)

	require.NoError(t, w.forceWatch(ctx)) // next tick starts a replacement (b is live again)
	require.Equal(t, []string{"b"}, w.Snapshot().ExtraChannels)
}

// When the primary drops, the second slot is promoted and a replacement fills
// the remaining slot without duplicating a channel.
func TestForceWatch_PrimaryDownPromotesSecondAndRefills(t *testing.T) {
	ctx := context.Background()
	b := newSlotBackend()
	w := twoSlotWatcher(b, ForceTask{Channel: "a", Alternates: []string{"b", "c"}, Slots: 2})
	for i := 0; i < 2; i++ {
		require.NoError(t, w.forceWatch(ctx))
	}
	require.Equal(t, []string{"b"}, w.Snapshot().ExtraChannels)

	w.handlePubSubStreamDown("id-a")
	b.offline["a"] = true                 // a really went offline
	require.NoError(t, w.forceWatch(ctx)) // restarts the primary
	require.Equal(t, "b", w.Snapshot().Channel, "the existing second slot is promoted")
	require.Equal(t, []string{"c"}, w.Snapshot().ExtraChannels, "the replacement does not duplicate b")
}
