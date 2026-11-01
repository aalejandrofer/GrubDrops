package runner

import (
	"context"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/platform/platformtest"
)

// raidFake is a PubSubFake that can follow raids. It records raid topic
// subscriptions and joins.
type raidFake struct {
	*platformtest.PubSubFake
	mu     sync.Mutex
	subs   map[string]bool // channel ID -> raid topic subscribed
	joins  []string
	events []string // "sub:<id>", "unsub:<id>", "join:<raid>"
}

func (f *raidFake) SubscribeRaids(_ string, channelID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.subs[channelID] = true
	f.events = append(f.events, "sub:"+channelID)
}

func (f *raidFake) UnsubscribeRaids(_ string, channelID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.subs, channelID)
	f.events = append(f.events, "unsub:"+channelID)
}

func (f *raidFake) JoinRaid(_ context.Context, _ platform.Session, raidID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.joins = append(f.joins, raidID)
	f.events = append(f.events, "join:"+raidID)
	return nil
}

func (f *raidFake) joined() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.joins...)
}

func (f *raidFake) subscribed(channelID string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.subs[channelID]
}

var _ platform.RaidJoiner = (*raidFake)(nil)

func newRaidFake() *raidFake {
	return &raidFake{PubSubFake: psFake(), subs: map[string]bool{}}
}

// shrinkRaidDelay sets the join delay for one test.
func shrinkRaidDelay(t *testing.T, d time.Duration) {
	t.Helper()
	old := raidDelay
	raidDelay = func() time.Duration { return d }
	t.Cleanup(func() { raidDelay = old })
}

// raidWatching is a runner on f drop-watching channel "a" (ID "42"), with
// follow raids read from on (nil = setting unset).
func raidWatching(t *testing.T, f *raidFake, on *atomic.Bool) *Runner {
	t.Helper()
	f.Campaigns = []platform.Campaign{camp("c1", "Rust", "d1")}
	f.Campaigns[0].Benefits[0].RequiredMinutes = 100000
	f.Progress["d1"] = &platform.Progress{BenefitID: "d1"}
	f.Streams["c1"] = []platform.Stream{{Channel: "a", ChannelID: "42", ViewerCount: 10}}
	cfg := Config{AccountID: "acc", Platform: "twitch", Backend: f, Games: rustGames, TickInterval: time.Nanosecond}
	if on != nil {
		cfg.FollowRaids = on.Load
	}
	r := New(cfg)
	t.Cleanup(func() { r.stopWatch(context.Background()) })
	steps(t, r, 2) // pick, start
	if r.State() != StateWatching {
		t.Fatalf("state=%s", r.State())
	}
	return r
}

func onAtomic(v bool) *atomic.Bool {
	b := &atomic.Bool{}
	b.Store(v)
	return b
}

// With the setting unset, a watch subscribes to no raids and a pushed raid
// is not joined.
func TestRaid_OffByDefault(t *testing.T) {
	shrinkRaidDelay(t, time.Millisecond)
	f := newRaidFake()
	r := raidWatching(t, f, nil)
	if f.subscribed("42") {
		t.Fatal("raid topic subscribed with follow raids unset")
	}
	hooksOf(t, f.PubSubFake).OnRaid("42", "raid-1")
	r.onRaid("42", "raid-2")
	time.Sleep(30 * time.Millisecond)
	if j := f.joined(); len(j) != 0 {
		t.Fatalf("joined %v with follow raids unset", j)
	}
}

// A raid pushed by the watched channel is joined once, however often the
// push repeats; a raid from another channel is not joined.
func TestRaid_JoinsOncePerRaidID(t *testing.T) {
	shrinkRaidDelay(t, 5*time.Millisecond)
	f := newRaidFake()
	r := raidWatching(t, f, onAtomic(true))
	if !f.subscribed("42") {
		t.Fatal("drop watch did not subscribe its channel's raids")
	}
	h := hooksOf(t, f.PubSubFake)
	for range 3 {
		h.OnRaid("42", "raid-1")
	}
	h.OnRaid("99", "raid-other")
	if !waitFor(func() bool { return len(f.joined()) >= 1 }, time.Second) {
		t.Fatal("raid not joined")
	}
	h.OnRaid("42", "raid-1") // after the join too
	time.Sleep(30 * time.Millisecond)
	if j := f.joined(); !slices.Equal(j, []string{"raid-1"}) {
		t.Fatalf("joins=%v, want [raid-1]", j)
	}
	r.stopWatch(context.Background())
	if f.subscribed("42") {
		t.Fatal("raid topic kept after the watch stopped")
	}
}

// A raid whose watch stops during the delay is not joined.
func TestRaid_NoJoinAfterWatchStops(t *testing.T) {
	shrinkRaidDelay(t, 40*time.Millisecond)
	f := newRaidFake()
	r := raidWatching(t, f, onAtomic(true))
	r.onRaid("42", "raid-1")
	r.stopWatch(context.Background())
	time.Sleep(80 * time.Millisecond)
	if j := f.joined(); len(j) != 0 {
		t.Fatalf("joined %v after the watch stopped", j)
	}
}

// A raid whose runner stops (run context cancelled) during the delay is not
// joined, even before stopWatch runs.
func TestRaid_NoJoinAfterRunnerStops(t *testing.T) {
	shrinkRaidDelay(t, 40*time.Millisecond)
	f := newRaidFake()
	f.Campaigns = []platform.Campaign{camp("c1", "Rust", "d1")}
	f.Campaigns[0].Benefits[0].RequiredMinutes = 100000
	f.Progress["d1"] = &platform.Progress{BenefitID: "d1"}
	f.Streams["c1"] = []platform.Stream{{Channel: "a", ChannelID: "42", ViewerCount: 10}}
	r := New(Config{AccountID: "acc", Platform: "twitch", Backend: f, Games: rustGames,
		TickInterval: time.Nanosecond, FollowRaids: func() bool { return true }})
	t.Cleanup(func() { r.stopWatch(context.Background()) })
	ctx, cancel := context.WithCancel(context.Background())
	for i := range 2 {
		if err := r.step(ctx); err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
	}
	if r.State() != StateWatching {
		t.Fatalf("state=%s", r.State())
	}
	r.onRaid("42", "raid-1")
	cancel()
	time.Sleep(80 * time.Millisecond)
	if j := f.joined(); len(j) != 0 {
		t.Fatalf("joined %v after the runner stopped", j)
	}
}

// After the delay, a join goes out only while the raid task is still the
// live watch's on the raiding channel.
func TestRaid_JoinRechecksWatch(t *testing.T) {
	f := newRaidFake()
	r := raidWatching(t, f, onAtomic(true))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stale := &raidWatch{channelID: "42", ctx: ctx, cancel: cancel}
	stale.wg.Add(1)
	r.joinRaid(stale, f, "raid-stale", 0)
	if j := f.joined(); len(j) != 0 {
		t.Fatalf("joined %v from a raid task the watch no longer runs", j)
	}

	r.mu.Lock()
	live := r.raids
	r.mu.Unlock()
	live.wg.Add(1)
	r.joinRaid(live, f, "raid-live", 0)
	if j := f.joined(); !slices.Equal(j, []string{"raid-live"}) {
		t.Fatalf("joins=%v, want [raid-live]", j)
	}
}

// Turning the setting on or off applies to the running watch without
// restarting it.
func TestRaid_SettingAppliesLive(t *testing.T) {
	shrinkRaidDelay(t, time.Millisecond)
	f := newRaidFake()
	on := onAtomic(false)
	r := raidWatching(t, f, on)
	if f.subscribed("42") {
		t.Fatal("subscribed while off")
	}
	on.Store(true)
	r.applyLive(context.Background())
	if !f.subscribed("42") || r.State() != StateWatching {
		t.Fatalf("after on: subscribed=%v state=%s", f.subscribed("42"), r.State())
	}
	on.Store(false)
	r.applyLive(context.Background())
	if f.subscribed("42") || r.State() != StateWatching {
		t.Fatalf("after off: subscribed=%v state=%s", f.subscribed("42"), r.State())
	}
	r.onRaid("42", "raid-1")
	time.Sleep(20 * time.Millisecond)
	if j := f.joined(); len(j) != 0 {
		t.Fatalf("joined %v after turning follow raids off", j)
	}
}

// A force watch follows raids from its channel too.
func TestRaid_ForceWatchSubscribes(t *testing.T) {
	shrinkRaidDelay(t, time.Millisecond)
	f := newRaidFake()
	setLive(f.Fake, "b", true)
	cfg := Config{AccountID: "acc", Platform: "twitch", Backend: f, Games: rustGames,
		FollowRaids: func() bool { return true }}
	forceCfg(f.Fake, "b")(&cfg)
	r := New(cfg)
	t.Cleanup(func() { r.stopWatch(context.Background()) })
	steps(t, r, 1)
	forceOn(t, r, "b")
	if !f.subscribed("id-b") {
		t.Fatal("force watch did not subscribe its channel's raids")
	}
	r.onRaid("id-b", "raid-1")
	if !waitFor(func() bool { return len(f.joined()) == 1 }, time.Second) {
		t.Fatal("force watch raid not joined")
	}
	forceOn(t, r, "b") // still on the original channel
}
