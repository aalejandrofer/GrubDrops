package runner

import (
	"context"
	"sync"
	"testing"

	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/platform/platformtest"
)

type codeClaims struct {
	fakeClaims
	mu    sync.Mutex
	codes []string
}

func (c *codeClaims) RecordClaimWithCode(_ context.Context, acc string, b platform.DropBenefit, code string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.codes = append(c.codes, acc+":"+b.ID+":"+code)
	return nil
}

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

func TestPubSub_RegisteredBeforeFirstCall(t *testing.T) {
	p := psFake()
	New(Config{AccountID: "acc", Platform: "twitch", Backend: p, Games: rustGames})
	h := hooksOf(t, p)
	if h.OnDropProgress == nil || h.OnDropClaim == nil || h.OnStreamDown == nil ||
		h.OnStreamUp == nil || h.OnRewardCode == nil {
		t.Fatalf("hooks incomplete: %+v", h)
	}
	if len(p.Calls) != 0 {
		t.Fatalf("calls=%v", p.Calls)
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

func TestPubSub_RewardCodeRecorded(t *testing.T) {
	cl := &codeClaims{}
	_, p := psWatching(t, func(c *Config) { c.Claims = cl })
	hooksOf(t, p).OnRewardCode("n", "CODE", "b")
	cl.mu.Lock()
	defer cl.mu.Unlock()
	if len(cl.codes) != 1 || cl.codes[0] != "acc:d1:CODE" {
		t.Fatalf("codes=%v", cl.codes)
	}
}

func TestPubSub_RewardCodeWithoutCurrentIgnored(t *testing.T) {
	cl := &codeClaims{}
	p := psFake()
	New(Config{AccountID: "acc", Platform: "twitch", Backend: p, Games: rustGames, Claims: cl})
	hooksOf(t, p).OnRewardCode("n", "CODE", "b")
	if len(cl.codes) != 0 {
		t.Fatalf("codes=%v", cl.codes)
	}
}

func TestPubSub_PushThenPollNoFalseFreeze(t *testing.T) {
	r, p := psWatching(t, nil)
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
