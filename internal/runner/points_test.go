package runner

import (
	"context"
	"errors"
	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/platform/platformtest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestZeroGameListForceWatchIsSafe(t *testing.T) {
	f := platformtest.NewFake()
	setLive(f, "a", true)
	r := newRunner(f, func(c *Config) {
		c.Games = nil
		forceCfg(f, "a")(c)
	})
	if err := r.pick(context.Background()); err != nil {
		t.Fatal(err)
	}
	forceOn(t, r, "a")
	if hasCall(f, "List") {
		t.Fatalf("zero game runner scanned drops: %v", f.Calls)
	}
}

func TestPointsOnlyDoesNotMineDrops(t *testing.T) {
	f := platformtest.NewFake()
	setLive(f, "a", true)
	r := New(Config{AccountID: "acc", Platform: "twitch", Backend: f, Games: rustGames, PointsOnly: true,
		ForceChannels: func() []string { return []string{"a"} },
		ForceLookup: func(ctx context.Context, login string) (platform.Stream, bool, error) {
			return f.LookupChannel(ctx, platform.Session{}, login)
		}})
	if err := r.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if r.State() != StateForceWatching {
		t.Fatalf("zero-game points-only runner state=%s", r.State())
	}
	for _, call := range f.Calls {
		if call == "List" || call == "Inventory" || strings.HasPrefix(call, "Claim:") {
			t.Fatalf("points-only runner mined drops: %v", f.Calls)
		}
	}
	if err := r.forceTick(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, call := range f.Calls {
		if call == "List" || call == "Inventory" || strings.HasPrefix(call, "Claim:") {
			t.Fatalf("points-only heartbeat mined drops: %v", f.Calls)
		}
	}
}
func TestSafetyChangedOnlyWakesForceAndSleepStates(t *testing.T) {
	r := New(Config{AccountID: "acc", Backend: platformtest.NewFake()})
	r.mu.Lock()
	r.state = StateForceWatching
	r.mu.Unlock()
	r.SafetyChanged()
	if !r.forceScan.Load() {
		t.Fatal("force watch was not scheduled to re-evaluate safety")
	}
	r.forceScan.Store(false)
	r.mu.Lock()
	r.state = StateWatching
	r.mu.Unlock()
	r.SafetyChanged()
	if r.forceScan.Load() {
		t.Fatal("active drop watch was interrupted by safety change")
	}
	r.mu.Lock()
	r.state = StateSleeping
	r.mu.Unlock()
	r.SafetyChanged()
	select {
	case <-r.rescan:
	default:
		t.Fatal("sleeping runner was not woken")
	}
}

func TestPrimaryStreamDownStopsEveryExtraSlot(t *testing.T) {
	f := platformtest.NewFake()
	for _, name := range []string{"a", "b", "c"} {
		setLive(f, name, true)
	}
	r := New(Config{AccountID: "acc", Platform: "twitch", Backend: f,
		PointsStreams: func() int { return 3 },
		ForceChannels: func() []string { return []string{"a", "b", "c"} },
		ForceLookup: func(ctx context.Context, login string) (platform.Stream, bool, error) {
			return f.LookupChannel(ctx, platform.Session{}, login)
		}})
	if err := r.pick(context.Background()); err != nil {
		t.Fatal(err)
	}
	r.onStreamDown("id-a")
	for _, channel := range []string{"a", "b", "c"} {
		if !hasCall(f, "Stop:"+channel) {
			t.Fatalf("stream-down leaked %s: %v", channel, f.Calls)
		}
	}
}
func TestExtraSlotRetriesNoMoreOftenThanFiveMinutes(t *testing.T) {
	f := platformtest.NewFake()
	setLive(f, "a", true)
	setLive(f, "b", false)
	r := New(Config{AccountID: "acc", Platform: "twitch", Backend: f,
		PointsStreams: func() int { return 3 },
		ForceChannels: func() []string { return []string{"a", "b"} },
		ForceLookup: func(ctx context.Context, login string) (platform.Stream, bool, error) {
			return f.LookupChannel(ctx, platform.Session{}, login)
		}})
	if err := r.pick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !hasCall(f, "Start:a") || hasCall(f, "Start:b") {
		t.Fatalf("initial fill did not skip offline slot: %v", f.Calls)
	}
	r.mu.Lock()
	retryAt := r.extraRetryAt
	r.mu.Unlock()
	if left := time.Until(retryAt); left <= 0 || left > recheckInterval {
		t.Fatalf("extra-slot retry deadline in %v, want within %v", left, recheckInterval)
	}
	setLive(f, "b", true)
	r.startExtraForce(context.Background(), "a")
	if hasCall(f, "Start:b") {
		t.Fatalf("retried extra slot before five-minute deadline: %v", f.Calls)
	}
	r.mu.Lock()
	r.extraRetryAt = time.Now().Add(-time.Second)
	r.mu.Unlock()
	r.startExtraForce(context.Background(), "a")
	if !hasCall(f, "Start:b") {
		t.Fatalf("did not fill slot after retry deadline: %v", f.Calls)
	}
}

func TestPointsForceStreamSlotsAndTeardown(t *testing.T) {
	for _, tc := range []struct{ slots, want int }{{1, 1}, {3, 3}} {
		t.Run(string(rune('0'+tc.slots)), func(t *testing.T) {
			f := platformtest.NewFake()
			for _, name := range []string{"a", "b", "c", "d"} {
				setLive(f, name, true)
			}
			r := New(Config{AccountID: "acc", Platform: "twitch", Backend: f,
				PointsStreams: func() int { return tc.slots },
				ForceChannels: func() []string { return []string{"a", "b", "c", "d"} },
				ForceLookup: func(ctx context.Context, login string) (platform.Stream, bool, error) {
					return f.LookupChannel(ctx, platform.Session{}, login)
				}})
			if err := r.pick(context.Background()); err != nil {
				t.Fatal(err)
			}
			starts := 0
			for _, call := range f.Calls {
				if strings.HasPrefix(call, "Start:") {
					starts++
				}
			}
			if starts != tc.want {
				t.Fatalf("started %d watches, want %d: %v", starts, tc.want, f.Calls)
			}
			if tc.slots == 3 {
				r.onStreamDown("id-b")
				if !hasCall(f, "Stop:b") || len(r.Snapshot().ExtraChannels) != 1 {
					t.Fatalf("extra stream-down did not tear down only that slot: state=%+v calls=%v", r.Snapshot(), f.Calls)
				}
			}
			r.stopWatch(context.Background())
			for _, name := range []string{"a", "b", "c"} {
				if tc.slots == 1 && name != "a" {
					continue
				}
				if !hasCall(f, "Stop:"+name) {
					t.Fatalf("watch %s leaked on teardown: %v", name, f.Calls)
				}
			}
		})
	}
}

func TestRaidJoinHonorsLiveToggleAndActiveSource(t *testing.T) {
	oldDelay := raidJoinDelay
	raidJoinDelay = func() time.Duration { return 0 }
	defer func() { raidJoinDelay = oldDelay }()
	f := &raidFake{Fake: platformtest.NewFake(), joined: make(chan string, 2)}
	var follow atomic.Bool
	r := New(Config{AccountID: "acc", Platform: "twitch", Backend: f, FollowRaids: follow.Load})
	r.mu.Lock()
	r.runCtx = context.Background()
	r.state = StateForceWatching
	h := platform.WatchHandle{Channel: "primary"}
	r.handle = &h
	r.stream = &platform.Stream{ChannelID: "source"}
	r.mu.Unlock()
	r.onRaid("other", "inactive")
	follow.Store(true)
	r.onRaid("source", "enabled")
	select {
	case id := <-f.joined:
		if id != "enabled" {
			t.Fatalf("joined raid %q", id)
		}
	case <-time.After(time.Second):
		t.Fatal("enabled raid was not joined")
	}
}

func TestRaidJoinIsCanceledWhenSourceStopsDuringDelay(t *testing.T) {
	oldDelay := raidJoinDelay
	delayStarted := make(chan struct{})
	raidJoinDelay = func() time.Duration {
		close(delayStarted)
		return 40 * time.Millisecond
	}
	defer func() { raidJoinDelay = oldDelay }()
	f := &raidFake{Fake: platformtest.NewFake(), joined: make(chan string, 1)}
	r := New(Config{AccountID: "acc", Platform: "twitch", Backend: f, FollowRaids: func() bool { return true }})
	r.mu.Lock()
	r.runCtx = context.Background()
	r.state = StateForceWatching
	h := platform.WatchHandle{Channel: "primary"}
	r.handle = &h
	r.extraWatches = []extraWatch{{handle: platform.WatchHandle{Channel: "extra"}, stream: platform.Stream{ChannelID: "extra-id"}}}
	r.extraChannels = []string{"extra"}
	r.mu.Unlock()
	r.onRaid("extra-id", "stale")
	<-delayStarted
	r.mu.Lock()
	r.extraWatches = nil
	r.extraChannels = nil
	r.mu.Unlock()
	time.Sleep(80 * time.Millisecond)
	select {
	case id := <-f.joined:
		t.Fatalf("joined stale raid %q after source stopped", id)
	default:
	}
}

func TestForceExtraStartRacingTeardownIsStoppedAndUnsubscribed(t *testing.T) {
	f := &extraRunnerFake{Fake: platformtest.NewFake(), entered: make(chan struct{}), release: make(chan struct{})}
	setLive(f.Fake, "a", true)
	setLive(f.Fake, "b", true)
	slots := 1
	r := New(Config{AccountID: "acc", Platform: "twitch", Backend: f,
		PointsStreams: func() int { return slots },
		ForceChannels: func() []string { return []string{"a", "b"} },
		ForceLookup: func(ctx context.Context, login string) (platform.Stream, bool, error) {
			return f.LookupChannel(ctx, platform.Session{}, login)
		}})
	if err := r.pick(context.Background()); err != nil {
		t.Fatal(err)
	}
	slots = 2
	started := make(chan struct{})
	go func() {
		r.startExtraForce(context.Background(), "a")
		close(started)
	}()
	<-f.entered
	r.stopForceWatch(context.Background())
	slots = 1
	stream, live, err := r.cfg.ForceLookup(context.Background(), "a")
	if err != nil || !live {
		t.Fatalf("primary lookup: live=%v err=%v", live, err)
	}
	if err := r.beginForce(context.Background(), "a", stream); err != nil {
		t.Fatal(err)
	}
	slots = 2
	close(f.release)
	<-started
	r.stopWatch(context.Background())
	if !hasCall(f.Fake, "Stop:b") {
		t.Fatalf("extra watch returned during teardown was not stopped: %v", f.Calls)
	}
	f.Mu.Lock()
	unsubscribed := contains(f.unsubscribed, "id-b")
	f.Mu.Unlock()
	if !unsubscribed {
		t.Fatal("extra channel subscription was not removed")
	}
}
func TestForceListChangeRejectsExtraAlreadyStarting(t *testing.T) {
	f := &extraRunnerFake{Fake: platformtest.NewFake(), entered: make(chan struct{}), release: make(chan struct{})}
	for _, name := range []string{"a", "b", "c"} {
		setLive(f.Fake, name, true)
	}
	channels := []string{"a", "b"}
	slots := 1
	r := New(Config{AccountID: "acc", Platform: "twitch", Backend: f,
		PointsStreams: func() int { return slots },
		ForceChannels: func() []string { return channels },
		ForceLookup: func(ctx context.Context, login string) (platform.Stream, bool, error) {
			return f.LookupChannel(ctx, platform.Session{}, login)
		}})
	if err := r.pick(context.Background()); err != nil {
		t.Fatal(err)
	}
	slots = 2
	started := make(chan struct{})
	go func() {
		r.startExtraForce(context.Background(), "a")
		close(started)
	}()
	<-f.entered
	channels = []string{"a", "c"}
	r.mu.Lock()
	r.extraRetryAt = time.Now().Add(recheckInterval)
	r.mu.Unlock()
	r.ForceListChanged()
	r.mu.Lock()
	retryAt := r.extraRetryAt
	r.mu.Unlock()
	if !retryAt.IsZero() {
		t.Fatalf("force-list edit retained extra cooldown: %v", retryAt)
	}
	close(f.release)
	<-started
	if got := strings.Join(r.Snapshot().ExtraChannels, ","); got != "" {
		t.Fatalf("stale in-flight extra accepted after force-list change: %q", got)
	}
	if !hasCall(f.Fake, "Stop:b") {
		t.Fatalf("stale in-flight extra was not stopped: %v", f.Calls)
	}
	f.Mu.Lock()
	unsubscribed := contains(f.unsubscribed, "id-b")
	f.Mu.Unlock()
	if !unsubscribed {
		t.Fatal("stale in-flight extra was not unsubscribed")
	}
}

func TestForceRecheckReconcilesRankedExtrasAndSlots(t *testing.T) {
	f := platformtest.NewFake()
	for _, name := range []string{"a", "b", "c", "d"} {
		setLive(f, name, true)
	}
	channels := []string{"a", "b", "c"}
	slots := 3
	r := New(Config{AccountID: "acc", Platform: "twitch", Backend: f,
		PointsStreams: func() int { return slots },
		ForceChannels: func() []string { return channels },
		ForceLookup: func(ctx context.Context, login string) (platform.Stream, bool, error) {
			return f.LookupChannel(ctx, platform.Session{}, login)
		}})
	if err := r.pick(context.Background()); err != nil {
		t.Fatal(err)
	}
	channels = []string{"a", "c", "d"}
	if err := r.forceRecheck(context.Background(), "a"); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(r.Snapshot().ExtraChannels, ","); got != "c,d" {
		t.Fatalf("extras after ranked assignment change = %q, want c,d", got)
	}
	if !hasCall(f, "Stop:b") || !hasCall(f, "Start:d") {
		t.Fatalf("stale extra was not replaced: %v", f.Calls)
	}
	slots = 2
	if err := r.forceRecheck(context.Background(), "a"); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(r.Snapshot().ExtraChannels, ","); got != "c" {
		t.Fatalf("extras after slot reduction = %q, want c", got)
	}
	if !hasCall(f, "Stop:d") {
		t.Fatalf("excess extra was not stopped: %v", f.Calls)
	}
}
func TestExtraFillSkipsOfflineHigherRankedChannel(t *testing.T) {
	f := platformtest.NewFake()
	setLive(f, "a", true)
	setLive(f, "b", false)
	setLive(f, "c", true)
	r := New(Config{AccountID: "acc", Platform: "twitch", Backend: f,
		PointsStreams: func() int { return 2 },
		ForceChannels: func() []string { return []string{"a", "b", "c"} },
		ForceLookup: func(ctx context.Context, login string) (platform.Stream, bool, error) {
			return f.LookupChannel(ctx, platform.Session{}, login)
		}})
	if err := r.pick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(r.Snapshot().ExtraChannels, ","); got != "c" {
		t.Fatalf("extra after offline higher-ranked channel = %q, want c", got)
	}
	starts := f.CallCount("Start:c")
	r.startExtraForce(context.Background(), "a")
	if got := f.CallCount("Start:c"); got != starts || hasCall(f, "Stop:c") {
		t.Fatalf("ordinary heartbeat restarted healthy fallback c: starts %d -> %d calls %v", starts, got, f.Calls)
	}
}

func TestExtraFillSkipsFailedHigherRankedChannel(t *testing.T) {
	f := platformtest.NewFake()
	for _, name := range []string{"a", "b", "c"} {
		setLive(f, name, true)
	}
	f.StartErrs = map[string]error{"b": errors.New("scripted start failure")}
	r := New(Config{AccountID: "acc", Platform: "twitch", Backend: f,
		PointsStreams: func() int { return 2 },
		ForceChannels: func() []string { return []string{"a", "b", "c"} },
		ForceLookup: func(ctx context.Context, login string) (platform.Stream, bool, error) {
			return f.LookupChannel(ctx, platform.Session{}, login)
		}})
	if err := r.pick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(r.Snapshot().ExtraChannels, ","); got != "c" {
		t.Fatalf("extra after failed higher-ranked channel = %q, want c", got)
	}
	if !hasCall(f, "Start:b") || !hasCall(f, "Start:c") {
		t.Fatalf("did not fall through failed candidate: %v", f.Calls)
	}
}

func TestExtraCooldownStillReconcilesReducedSlotCount(t *testing.T) {
	f := platformtest.NewFake()
	for _, name := range []string{"a", "b", "c"} {
		setLive(f, name, true)
	}
	slots := 3
	r := New(Config{AccountID: "acc", Platform: "twitch", Backend: f,
		PointsStreams: func() int { return slots },
		ForceChannels: func() []string { return []string{"a", "b", "c"} },
		ForceLookup: func(ctx context.Context, login string) (platform.Stream, bool, error) {
			return f.LookupChannel(ctx, platform.Session{}, login)
		}})
	if err := r.pick(context.Background()); err != nil {
		t.Fatal(err)
	}
	slots = 2
	r.mu.Lock()
	r.extraRetryAt = time.Now().Add(recheckInterval)
	r.mu.Unlock()
	r.startExtraForce(context.Background(), "a")
	if got := strings.Join(r.Snapshot().ExtraChannels, ","); got != "b" {
		t.Fatalf("extras after slot reduction during cooldown = %q, want b", got)
	}
	if !hasCall(f, "Stop:c") {
		t.Fatalf("excess extra was not stopped during cooldown: %v", f.Calls)
	}
}

func TestVeryLargePointsStreamCountDoesNotPanic(t *testing.T) {
	f := platformtest.NewFake()
	setLive(f, "a", true)
	setLive(f, "b", true)
	slots := 1
	r := New(Config{AccountID: "acc", Platform: "twitch", Backend: f,
		PointsStreams: func() int { return slots },
		ForceChannels: func() []string { return []string{"a", "b"} },
		ForceLookup: func(ctx context.Context, login string) (platform.Stream, bool, error) {
			return f.LookupChannel(ctx, platform.Session{}, login)
		}})
	if err := r.pick(context.Background()); err != nil {
		t.Fatal(err)
	}
	slots = int(^uint(0) >> 1)
	r.reconcileExtraForce(context.Background(), "a", []string{"a", "b"})
	if got := r.Snapshot().ExtraChannels; len(got) != 1 || got[0] != "b" {
		t.Fatalf("extras with very large slot count = %v, want [b]", got)
	}
}

func TestFullExtraSlotsDoNotReadForceListOnHeartbeat(t *testing.T) {
	f := platformtest.NewFake()
	setLive(f, "a", true)
	setLive(f, "b", true)
	var reads atomic.Int32
	r := New(Config{AccountID: "acc", Platform: "twitch", Backend: f,
		PointsStreams: func() int { return 2 },
		ForceChannels: func() []string { reads.Add(1); return []string{"a", "b"} },
		ForceLookup: func(ctx context.Context, login string) (platform.Stream, bool, error) {
			return f.LookupChannel(ctx, platform.Session{}, login)
		}})
	if err := r.pick(context.Background()); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	r.forceCheckAt = time.Now()
	r.mu.Unlock()
	before := reads.Load()
	if err := r.forceTick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := reads.Load(); got != before {
		t.Fatalf("full-slot heartbeat read force list %d times, want no additional reads", got-before)
	}
}

func TestExtraHeartbeatFailureOnlyStopsThatExtra(t *testing.T) {
	f := &extraRunnerFake{Fake: platformtest.NewFake()}
	setLive(f.Fake, "a", true)
	setLive(f.Fake, "b", true)
	r := New(Config{AccountID: "acc", Platform: "twitch", Backend: f,
		PointsStreams: func() int { return 2 },
		ForceChannels: func() []string { return []string{"a", "b"} },
		ForceLookup: func(ctx context.Context, login string) (platform.Stream, bool, error) {
			return f.LookupChannel(ctx, platform.Session{}, login)
		}})
	if err := r.pick(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.failHeartbeat = "b"
	if err := r.forceTick(context.Background()); err != nil {
		t.Fatalf("extra heartbeat failure escaped force tick: %v", err)
	}
	if r.State() != StateForceWatching {
		t.Fatalf("extra heartbeat failure changed primary state to %s", r.State())
	}
	if !hasCall(f.Fake, "Stop:b") || hasCall(f.Fake, "Stop:a") {
		t.Fatalf("extra heartbeat failure stopped wrong watches: %v", f.Calls)
	}
	r.mu.Lock()
	retryAt := r.extraRetryAt
	r.mu.Unlock()
	if time.Until(retryAt) <= 0 || time.Until(retryAt) > recheckInterval {
		t.Fatalf("failed extra cooldown deadline %v, want five minutes", retryAt)
	}
}

type extraRunnerFake struct {
	*platformtest.Fake
	entered       chan struct{}
	release       chan struct{}
	failHeartbeat string
	muSubs        sync.Mutex
	unsubscribed  []string
}

func (f *extraRunnerFake) StartWatch(ctx context.Context, session platform.Session, stream platform.Stream) (platform.WatchHandle, error) {
	if stream.Channel == "b" && f.entered != nil {
		close(f.entered)
		<-f.release
	}
	return f.Fake.StartWatch(ctx, session, stream)
}

func (f *extraRunnerFake) Heartbeat(ctx context.Context, handle platform.WatchHandle) error {
	if handle.Channel == f.failHeartbeat {
		return errors.New("scripted heartbeat failure")
	}
	return f.Fake.Heartbeat(ctx, handle)
}

func (f *extraRunnerFake) SubscribeChannel(_, _ string) {}

func (f *extraRunnerFake) UnsubscribeChannel(_, channelID string) {
	f.muSubs.Lock()
	f.unsubscribed = append(f.unsubscribed, channelID)
	f.muSubs.Unlock()
}

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

type raidFake struct {
	*platformtest.Fake
	joined chan string
}

func (f *raidFake) JoinRaid(_ context.Context, _ platform.Session, raidID string) error {
	f.joined <- raidID
	return nil
}
