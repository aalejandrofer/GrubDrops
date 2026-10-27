package runner

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/platform/platformtest"
)

func psFake() *platformtest.PubSubFake {
	return &platformtest.PubSubFake{Fake: platformtest.NewFake()}
}

func hooksOf(t *testing.T, p *platformtest.PubSubFake) platform.PubSubHooks {
	t.Helper()
	p.Mu.Lock()
	defer p.Mu.Unlock()
	h, ok := p.Hooks["acc"]
	if !ok {
		t.Fatal("hooks not registered")
	}
	return h
}

func psWatching(t *testing.T, mod func(*Config)) (*Runner, *platformtest.PubSubFake) {
	t.Helper()
	p := psFake()
	p.Campaigns = []platform.Campaign{camp("c1", "Rust", "d1")}
	p.Streams["c1"] = []platform.Stream{{Channel: "a", ChannelID: "42", ViewerCount: 10}}
	cfg := Config{AccountID: "acc", Platform: "twitch", Backend: p, Games: rustGames}
	if mod != nil {
		mod(&cfg)
	}
	r := New(cfg)
	steps(t, r, 2)
	if r.State() != StateWatching {
		t.Fatalf("state=%s", r.State())
	}
	return r, p
}

// bootstrapPS makes the account's first backend call, which is when a real
// backend's PubSub client captures the hooks.
func bootstrapPS(t *testing.T, p *platformtest.PubSubFake) {
	t.Helper()
	if _, err := p.ListActiveCampaigns(context.Background(), platform.Session{AccountID: "acc"}); err != nil {
		t.Fatal(err)
	}
}

func TestPubSub_RegisteredBeforeFirstCall(t *testing.T) {
	p := psFake()
	New(Config{AccountID: "acc", Platform: "twitch", Backend: p, Games: rustGames})
	if len(p.Calls) != 0 {
		t.Fatalf("calls=%v", p.Calls)
	}
	bootstrapPS(t, p)
	h := hooksOf(t, p)
	if h.OnDropProgress == nil || h.OnDropClaim == nil || h.OnStreamDown == nil ||
		h.OnStreamUp == nil || h.OnRewardCode == nil {
		t.Fatalf("hooks incomplete: %+v", h)
	}
	h.OnStreamUp("42") // no-op, must not panic
}

func TestPubSub_ProgressUpdatesSnapshot(t *testing.T) {
	var changes int
	r, p := psWatching(t, func(c *Config) { c.OnChange = func(Snapshot) { changes++ } })
	h := hooksOf(t, p)
	r.mu.Lock()
	r.noProgress, r.noAdvance = 2, 3
	r.mu.Unlock()
	before := changes
	h.OnDropProgress("other", 30, 60)
	if got := r.Snapshot().MinutesWatched; got != 0 {
		t.Fatalf("other id applied: %d", got)
	}
	h.OnDropProgress("d1", 7, 60)
	if got := r.Snapshot().MinutesWatched; got != 7 {
		t.Fatalf("minutes=%d", got)
	}
	if c := r.Snapshot().Current; c == nil || c.Minutes != 7 {
		t.Fatalf("current minutes=%+v", c)
	}
	r.mu.Lock()
	np, na, lm := r.noProgress, r.noAdvance, r.lastMin
	r.mu.Unlock()
	if np != 0 || na != 0 || lm != 7 {
		t.Fatalf("noProgress=%d noAdvance=%d lastMin=%d", np, na, lm)
	}
	if changes <= before {
		t.Fatal("OnChange not called")
	}
	// flat minutes keep noAdvance
	r.mu.Lock()
	r.noAdvance = 2
	r.mu.Unlock()
	h.OnDropProgress("d1", 7, 60)
	r.mu.Lock()
	na = r.noAdvance
	r.mu.Unlock()
	if na != 2 {
		t.Fatalf("noAdvance=%d", na)
	}
}

func TestPubSub_ClaimEventMovesToClaiming(t *testing.T) {
	r, p := psWatching(t, nil)
	h := hooksOf(t, p)
	h.OnDropClaim("other", "x")
	if r.State() != StateWatching {
		t.Fatalf("state=%s", r.State())
	}
	h.OnDropClaim("d1", "inst")
	s := r.Snapshot()
	if s.State != StateClaiming || s.Current.Benefit.InstanceID != "inst" {
		t.Fatalf("%+v", s)
	}
}

func TestPubSub_StreamDownRestarts(t *testing.T) {
	r, p := psWatching(t, nil)
	h := hooksOf(t, p)
	h.OnStreamDown("99")
	if r.State() != StateWatching || hasCall(p.Fake, "Stop:a") {
		t.Fatalf("other channel acted: state=%s calls=%v", r.State(), p.Calls)
	}
	h.OnStreamDown("42")
	if !hasCall(p.Fake, "Stop:a") || r.State() != StateStarting {
		t.Fatalf("state=%s calls=%v", r.State(), p.Calls)
	}
}

func TestPubSub_RewardCodeWhileWatchingIgnored(t *testing.T) {
	cl := &fakeClaims{}
	_, p := psWatching(t, func(c *Config) { c.Claims = cl })
	hooksOf(t, p).OnRewardCode("n", "CODE", "b")
	if len(cl.recorded()) != 0 {
		t.Fatalf("code recorded against an unclaimed drop: %v", cl.recorded())
	}
}

func TestPubSub_RewardCodeWithoutCurrentIgnored(t *testing.T) {
	cl := &fakeClaims{}
	p := psFake()
	New(Config{AccountID: "acc", Platform: "twitch", Backend: p, Games: rustGames, Claims: cl})
	bootstrapPS(t, p)
	hooksOf(t, p).OnRewardCode("n", "CODE", "b")
	if len(cl.recorded()) != 0 {
		t.Fatalf("codes=%v", cl.recorded())
	}
}

func TestPubSub_PushThenPollNoFalseFreeze(t *testing.T) {
	r, p := psWatching(t, nil)
	p.Mu.Lock()
	p.OnHeartbeat = func(*platformtest.Fake) {} // minutes move only via the test
	p.Mu.Unlock()
	h := hooksOf(t, p)
	for i := 1; i <= freezeThreshold+3; i++ {
		p.Mu.Lock()
		p.Progress["d1"] = &platform.Progress{BenefitID: "d1", MinutesWatched: i}
		p.Mu.Unlock()
		h.OnDropProgress("d1", int64(i), 60)
		steps(t, r, 1)
		r.mu.Lock()
		na := r.noAdvance
		r.mu.Unlock()
		if na != 0 || r.State() != StateWatching {
			t.Fatalf("round %d: noAdvance=%d state=%s", i, na, r.State())
		}
	}
	if hasCall(p.Fake, "Stop:a") {
		t.Fatalf("rotated: %v", p.Calls)
	}
}

// watchingOn builds a runner on p watching d1, counting its OnChange calls.
func watchingOn(t *testing.T, p *platformtest.PubSubFake, changes *atomic.Int32) *Runner {
	t.Helper()
	r := New(Config{AccountID: "acc", Platform: "twitch", Backend: p, Games: rustGames,
		OnChange: func(Snapshot) { changes.Add(1) }})
	steps(t, r, 2)
	if r.State() != StateWatching {
		t.Fatalf("state=%s", r.State())
	}
	return r
}

func TestPubSub_ReplacedRunnerGetsPushesAfterBootstrap(t *testing.T) {
	p := psFake()
	p.Campaigns = []platform.Campaign{camp("c1", "Rust", "d1")}
	p.Streams["c1"] = []platform.Stream{{Channel: "a", ChannelID: "42", ViewerCount: 10}}
	var c1, c2 atomic.Int32
	r1 := watchingOn(t, p, &c1) // bootstraps the client with r1's registration
	r2 := watchingOn(t, p, &c2)
	before := c1.Load()
	hooksOf(t, p).OnDropProgress("d1", 7, 60)
	if got := r2.Snapshot().MinutesWatched; got != 7 {
		t.Fatalf("new runner minutes=%d, push did not reach it", got)
	}
	if got := r1.Snapshot().MinutesWatched; got == 7 {
		t.Fatal("push reached the replaced runner")
	}
	if c1.Load() != before {
		t.Fatal("replaced runner fired OnChange")
	}
	// Releasing the old runner must not unhook the new one.
	r1.Release()
	hooksOf(t, p).OnDropProgress("d1", 8, 60)
	if got := r2.Snapshot().MinutesWatched; got != 8 {
		t.Fatalf("after old Release minutes=%d", got)
	}
}

func TestPubSub_ReleasedRunnerGetsNoPushes(t *testing.T) {
	p := psFake()
	p.Campaigns = []platform.Campaign{camp("c1", "Rust", "d1")}
	p.Streams["c1"] = []platform.Stream{{Channel: "a", ChannelID: "42", ViewerCount: 10}}
	var c atomic.Int32
	r := watchingOn(t, p, &c)
	r.Release()
	before := c.Load()
	h := hooksOf(t, p)
	h.OnDropProgress("d1", 7, 60)
	h.OnDropClaim("d1", "inst")
	h.OnStreamDown("42")
	h.OnRewardCode("n", "CODE", "b")
	if c.Load() != before || r.State() != StateWatching || r.Snapshot().MinutesWatched == 7 {
		t.Fatalf("released runner reacted: changes %d->%d state=%s", before, c.Load(), r.State())
	}
}
