package runner

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/plan"
	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/platform/platformtest"
)

type notifyRec struct {
	event  string
	fields map[string]any
}

type spyNotifier struct {
	mu  sync.Mutex
	got []notifyRec
}

func (n *spyNotifier) Notify(_ context.Context, ev string, f map[string]any) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.got = append(n.got, notifyRec{ev, f})
	return nil
}

func (n *spyNotifier) of(ev string) []notifyRec {
	n.mu.Lock()
	defer n.mu.Unlock()
	var out []notifyRec
	for _, g := range n.got {
		if g.event == ev {
			out = append(out, g)
		}
	}
	return out
}

func tiered(id, game string, mins ...int) platform.Campaign {
	c := platform.Campaign{ID: id, Platform: "kick", Game: game, Status: "active", AccountLinked: true}
	for i, m := range mins {
		c.Benefits = append(c.Benefits, platform.DropBenefit{
			ID: "d" + string(rune('1'+i)), Name: "Drop" + string(rune('1'+i)), CampaignID: id, RequiredMinutes: m,
		})
	}
	return c
}

func claimSetup(f *platformtest.Fake, mins ...int) (*Runner, *spyNotifier, *fakeClaims, *fakeSkips) {
	f.Campaigns = []platform.Campaign{tiered("c1", "Rust", mins...)}
	f.Streams["c1"] = []platform.Stream{{Channel: "a", ChannelID: "ida", ViewerCount: 10}}
	for _, b := range f.Campaigns[0].Benefits {
		f.Progress[b.ID] = &platform.Progress{BenefitID: b.ID}
	}
	n := &spyNotifier{}
	cl := &fakeClaims{}
	sk := &fakeSkips{}
	r := newRunner(f, func(c *Config) { c.Notifier = n; c.Claims = cl; c.Skips = sk })
	return r, n, cl, sk
}

func TestRunner_MinesUntilClaim(t *testing.T) {
	f := platformtest.NewFake()
	r, n, cl, _ := claimSetup(f, 2)
	steps(t, r, 5) // pick, start, tick, tick->claiming, claim
	if r.State() != StatePicking {
		t.Fatalf("state=%s", r.State())
	}
	if c := f.CallCount("Claim:d1"); c != 1 {
		t.Fatalf("claims=%d", c)
	}
	if len(cl.recorded()) != 1 || cl.recorded()[0] != "acc:d1:" {
		t.Fatalf("rec=%v", cl.recorded())
	}
	cn := n.of("claim")
	if len(cn) != 1 || cn[0].fields["drop"] != "Drop1" || cn[0].fields["channel"] != "a" ||
		cn[0].fields["cur_min"] != 2 || cn[0].fields["req_min"] != 2 {
		t.Fatalf("notify=%+v", cn)
	}
	if f.CallCount("Stop:") != 1 {
		t.Fatalf("calls=%v", f.Calls)
	}
	if r.Snapshot().Current != nil {
		t.Fatal("current not cleared")
	}
}

// toClaiming drives the runner to StateClaiming on d1.
func toClaiming(t *testing.T, r *Runner) {
	t.Helper()
	steps(t, r, 4)
	if r.State() != StateClaiming {
		t.Fatalf("state=%s", r.State())
	}
}

func TestClaim_RepeatedFailureSkips(t *testing.T) {
	f := platformtest.NewFake()
	r, _, _, sk := claimSetup(f, 2, 2)
	f.ClaimErr = errors.New("boom")
	toClaiming(t, r)
	for i := 0; i < claimFailSkipThreshold; i++ {
		if err := r.claim(context.Background()); err == nil {
			t.Fatalf("attempt %d: want error", i)
		}
	}
	if len(sk.recorded) != 1 || sk.recorded[0] != "d1" {
		t.Fatalf("recorded=%v", sk.recorded)
	}
	f.ClaimErr = nil
	r.setState(context.Background(), StatePicking) // Run does this after a step error
	steps(t, r, 1)
	if cur := r.Snapshot().Current; cur == nil || cur.Benefit.ID != "d2" {
		t.Fatalf("current=%+v", cur)
	}
}

func TestClaim_TransientFailureNotSkipped(t *testing.T) {
	f := platformtest.NewFake()
	r, _, cl, sk := claimSetup(f, 2)
	toClaiming(t, r)
	f.ClaimErr = errors.New("flaky")
	for i := 0; i < 2; i++ {
		if err := r.claim(context.Background()); err == nil {
			t.Fatal("want error")
		}
	}
	f.ClaimErr = nil
	if err := r.claim(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(sk.recorded) != 0 || len(cl.recorded()) != 1 || len(r.claimFailures) != 0 {
		t.Fatalf("skips=%v rec=%v failures=%v", sk.recorded, cl.recorded(), r.claimFailures)
	}
}

func TestClaim_FailSkipKeptWhileStillComplete(t *testing.T) {
	f := platformtest.NewFake()
	r, _, _, sk := claimSetup(f, 2)
	toClaiming(t, r)
	f.ClaimErr = errors.New("boom")
	for i := 0; i < claimFailSkipThreshold; i++ {
		_ = r.claim(context.Background())
	}
	// Inventory keeps the row at >= required and unclaimed.
	f.Mu.Lock()
	f.Progress["d1"] = &platform.Progress{BenefitID: "d1", MinutesWatched: 2}
	f.Mu.Unlock()
	r.setState(context.Background(), StatePicking)
	steps(t, r, 1)
	if len(sk.cleared) != 0 {
		t.Fatalf("cleared=%v", sk.cleared)
	}
	if cur := r.Snapshot().Current; cur != nil && cur.Benefit.ID == "d1" {
		t.Fatalf("d1 re-picked: %+v", cur)
	}
}

func TestClaim_MultiTierContinuesAfterFirstClaim(t *testing.T) {
	f := platformtest.NewFake()
	r, _, _, _ := claimSetup(f, 1, 3)
	steps(t, r, 4) // pick, start, tick -> claiming (1 min), claim
	if f.CallCount("Claim:d1") != 1 {
		t.Fatalf("calls=%v", f.Calls)
	}
	steps(t, r, 1) // pick
	if cur := r.Snapshot().Current; cur == nil || cur.Benefit.ID != "d2" {
		t.Fatalf("current=%+v", cur)
	}
}

func TestSweep_NotifiesSiblingsOnceExcludingCurrent(t *testing.T) {
	f := platformtest.NewFake()
	f.Campaigns = []platform.Campaign{tiered("c1", "Rust", 60)}
	f.Streams["c1"] = []platform.Stream{{Channel: "a", ChannelID: "ida", ViewerCount: 10}}
	f.Progress["d1"] = &platform.Progress{BenefitID: "d1"}
	f.Campaigns[0].Name = "Camp"
	f.Campaigns[0].Benefits[0].ImageURL = "http://img"
	sf := &platformtest.SweepFake{Fake: f}
	sf.Sweep = func() []platform.ClaimedReward {
		return []platform.ClaimedReward{{Title: "Sib", Game: "Rust"}, {Title: "Drop1"}, {Title: ""}}
	}
	n := &spyNotifier{}
	r := New(Config{AccountID: "acc", Platform: "kick", Backend: sf, Games: rustGames, Notifier: n})
	steps(t, r, 4) // pick, start, tick, tick
	cn := n.of("claim")
	if len(cn) != 1 || cn[0].fields["drop"] != "Sib" {
		t.Fatalf("notify=%+v", cn)
	}
	if _, ok := cn[0].fields["game"]; ok { // not in the plan: backend game not trusted
		t.Fatalf("game leaked: %+v", cn[0].fields)
	}
	if _, ok := cn[0].fields["campaign"]; ok {
		t.Fatalf("campaign leaked: %+v", cn[0].fields)
	}
	if _, ok := cn[0].fields["image"]; ok {
		t.Fatalf("notify=%+v", cn)
	}
}

func TestReaper_CallsOnceWithRewardGames(t *testing.T) {
	f := platformtest.NewFake()
	rf := &platformtest.RewardFake{Fake: f, Rewards: []platform.ClaimedReward{{Game: "Rust", Title: "R"}}}
	a := platform.Campaign{ID: "r1", Platform: "kick", Game: "Rust", Kind: "reward", Status: "active"}
	b := platform.Campaign{ID: "r2", Platform: "kick", Game: "Rust", Kind: "reward", Status: "active"}
	other := platform.Campaign{ID: "r3", Platform: "kick", Game: "Other", Kind: "reward", Status: "active"}
	f.Campaigns = []platform.Campaign{a, b, other}
	r := New(Config{AccountID: "acc", Platform: "kick", Backend: rf, Games: rustGames})
	steps(t, r, 1)
	rf.Mu.Lock()
	defer rf.Mu.Unlock()
	if len(rf.RewardCalls) != 1 || len(rf.RewardCalls[0]) != 1 || rf.RewardCalls[0][0] != "Rust" {
		t.Fatalf("calls=%v", rf.RewardCalls)
	}
}

func TestReaper_SkippedWithoutRewardCampaigns(t *testing.T) {
	f := platformtest.NewFake()
	rf := &platformtest.RewardFake{Fake: f}
	f.Campaigns = []platform.Campaign{tiered("c1", "Rust", 60)}
	r := New(Config{AccountID: "acc", Platform: "kick", Backend: rf, Games: rustGames})
	steps(t, r, 1)
	rf.Mu.Lock()
	defer rf.Mu.Unlock()
	if len(rf.RewardCalls) != 0 {
		t.Fatalf("calls=%v", rf.RewardCalls)
	}
}

func TestClaim_InventoryClaimedRowTreatedAsClaimed(t *testing.T) {
	f := platformtest.NewFake()
	r, n, cl, _ := claimSetup(f, 100)
	steps(t, r, 2) // pick, start
	f.Mu.Lock()
	f.Progress["d1"].Claimed = true // claimed outside GrubDrops, under the requirement
	f.Mu.Unlock()
	steps(t, r, 1)
	if r.State() != StatePicking {
		t.Fatalf("state=%s", r.State())
	}
	if f.CallCount("Claim:") != 0 {
		t.Fatalf("backend Claim called: %v", f.Calls)
	}
	if !hasCall(f, "Stop:a") {
		t.Fatalf("watch not stopped: %v", f.Calls)
	}
	if len(cl.recorded()) != 1 || cl.recorded()[0] != "acc:d1:" {
		t.Fatalf("rec=%v", cl.recorded())
	}
	if cn := n.of("claim"); len(cn) != 1 || cn[0].fields["drop"] != "Drop1" || cn[0].fields["channel"] != "a" {
		t.Fatalf("notify=%+v", cn)
	}
	if r.Snapshot().Current != nil {
		t.Fatal("current not cleared")
	}
}

func TestSnapshot_ClearedWhenNotWatching(t *testing.T) {
	f := platformtest.NewFake()
	r, _, _, _ := claimSetup(f, 100)
	f.Progress["d1"].MinutesWatched = 5
	steps(t, r, 3) // pick, start, tick
	if s := r.Snapshot(); s.Channel != "a" || s.ViewerCount != 10 || s.MinutesWatched != 6 {
		t.Fatalf("watching snapshot %+v", s)
	}
	r.stopWatch(context.Background())
	if s := r.Snapshot(); s.Channel != "" || s.ViewerCount != 0 || s.MinutesWatched != 0 {
		t.Fatalf("after stopWatch %+v", s)
	}

	// current=nil path: claim clears it.
	f2 := platformtest.NewFake()
	r2, _, _, _ := claimSetup(f2, 2)
	steps(t, r2, 5) // pick, start, tick, tick->claiming, claim
	if s := r2.Snapshot(); s.Current != nil || s.Channel != "" || s.ViewerCount != 0 || s.MinutesWatched != 0 {
		t.Fatalf("after claim %+v", s)
	}
}

func TestRunner_NoStreamReusesQueue(t *testing.T) {
	f := platformtest.NewFake()
	f.Campaigns = []platform.Campaign{camp("c1", "Rust", "b1"), camp("c2", "Rust", "b2"), camp("c3", "Rust", "b3")}
	f.Streams["c3"] = []platform.Stream{{Channel: "a", ChannelID: "ida", ViewerCount: 5}}
	r := newRunner(f, nil)
	for i := 0; i < 5 && r.State() != StateWatching; i++ {
		steps(t, r, 1)
	}
	if r.State() != StateWatching {
		t.Fatalf("state=%s calls=%v", r.State(), f.Calls)
	}
	if n := f.CallCount("List"); n != 1 {
		t.Fatalf("campaign lists=%d", n)
	}
	i1, i2, i3 := callIndex(f, "Channels:c1", 1), callIndex(f, "Channels:c2", 1), callIndex(f, "Channels:c3", 1)
	if i1 < 0 || i2 <= i1 || i3 <= i2 {
		t.Fatalf("channel order wrong: %v", f.Calls)
	}
	if cur := r.Snapshot().Current; cur == nil || cur.Benefit.ID != "b3" {
		t.Fatalf("current=%+v", cur)
	}
}

func TestRunner_NoStreamAnywhereSleeps(t *testing.T) {
	f := platformtest.NewFake()
	f.Campaigns = []platform.Campaign{camp("c1", "Rust", "b1"), camp("c2", "Rust", "b2")}
	r := newRunner(f, nil)
	for i := 0; i < 5 && r.State() != StateSleeping; i++ {
		steps(t, r, 1)
	}
	if r.State() != StateSleeping {
		t.Fatalf("state=%s", r.State())
	}
	if n := f.CallCount("List"); n != 1 {
		t.Fatalf("campaign lists=%d", n)
	}
	r.mu.Lock()
	ns := len(r.noStream)
	r.mu.Unlock()
	if r.Snapshot().Current != nil || ns != 0 {
		t.Fatalf("current=%+v noStream=%d", r.Snapshot().Current, ns)
	}
}

func TestRunner_SweptRewardRecordedWithGame(t *testing.T) {
	f := platformtest.NewFake()
	sf := &platformtest.SweepFake{Fake: f}
	f.Campaigns = []platform.Campaign{camp("c1", "Rust", "b1"), tiered("c2", "Rust Console", 60)}
	f.Campaigns[1].Benefits[0].ID = "h1"
	f.Campaigns[1].Benefits[0].Name = "Hoodie"
	f.Streams["c1"] = []platform.Stream{{Channel: "a", ChannelID: "ida", ViewerCount: 5}}
	var swept []platform.ClaimedReward
	sf.Sweep = func() []platform.ClaimedReward { return swept }
	n := &spyNotifier{}
	cl := &fakeClaims{}
	games := []plan.Game{{Name: "Rust"}, {Name: "Rust Console"}}
	r := New(Config{AccountID: "acc", Platform: "kick", Backend: sf, Games: games, Notifier: n, Claims: cl})
	steps(t, r, 2) // pick, start
	if r.State() != StateWatching {
		t.Fatalf("state=%s", r.State())
	}
	swept = []platform.ClaimedReward{{Game: "Rust", Title: "Hoodie"}}
	steps(t, r, 1)
	cn := n.of("claim")
	if len(cn) != 1 || cn[0].fields["game"] != "Rust Console" || cn[0].fields["drop"] != "Hoodie" {
		t.Fatalf("notify=%+v", cn)
	}
	if got := cl.recorded(); len(got) != 1 || got[0] != "acc:h1:" {
		t.Fatalf("recorded=%v", got)
	}
	if cl.recs[0].Target.Campaign.Game != "Rust Console" {
		t.Fatalf("%+v", cl.recs[0])
	}

	swept = []platform.ClaimedReward{{Game: "Rust", Title: "Mystery"}}
	steps(t, r, 1)
	cn = n.of("claim")
	if len(cn) != 2 || cn[1].fields["drop"] != "Mystery" {
		t.Fatalf("notify=%+v", cn)
	}
	if _, ok := cn[1].fields["game"]; ok {
		t.Fatalf("unmatched title kept a game: %+v", cn[1].fields)
	}
	if len(cl.recs) != 1 {
		t.Fatalf("recs=%v", cl.recorded())
	}
}

func TestRunner_ClaimFailureRepicksAndRetries(t *testing.T) {
	oldB := stepErrBackoff
	stepErrBackoff = time.Millisecond
	t.Cleanup(func() { stepErrBackoff = oldB })
	f := platformtest.NewFake()
	r, _, cl, _ := claimSetup(f, 1)
	r.cfg.TickInterval = 5 * time.Millisecond
	f.ClaimErr = errors.New("boom")
	stop := runUntil(t, r)
	defer stop()
	if !waitFor(func() bool { return f.CallCount("Claim:") >= 1 }, 2*time.Second) {
		t.Fatalf("no claim attempt: %v", f.Calls)
	}
	f.Mu.Lock()
	f.ClaimErr = nil
	f.Mu.Unlock()
	if !waitFor(func() bool { return len(cl.recorded()) == 1 }, 2*time.Second) {
		t.Fatalf("claim not recorded: %v", f.Calls)
	}
	if n := f.CallCount("Claim:"); n != 2 {
		t.Fatalf("claim calls=%d", n)
	}
	if n := f.CallCount("Start:"); n < 2 {
		t.Fatalf("expected a restart between claims, starts=%d", n)
	}
	if s := r.State(); s == StateClaiming || s == StateNeedsLogin {
		t.Fatalf("state=%s", s)
	}
}

func TestRunner_ReaperShellRowCampaign(t *testing.T) {
	f := platformtest.NewFake()
	rf := &platformtest.RewardFake{Fake: f}
	shell := platform.Campaign{ID: "s1", Platform: "kick", Game: "Rust", Kind: "drop", Status: "active"}
	other := platform.Campaign{ID: "s2", Platform: "kick", Game: "Other", Kind: "drop", Status: "active"}
	f.Campaigns = []platform.Campaign{shell, other}
	r := New(Config{AccountID: "acc", Platform: "kick", Backend: rf, Games: rustGames})
	steps(t, r, 1)
	rf.Mu.Lock()
	defer rf.Mu.Unlock()
	if len(rf.RewardCalls) != 1 || len(rf.RewardCalls[0]) != 1 || rf.RewardCalls[0][0] != "Rust" {
		t.Fatalf("calls=%v", rf.RewardCalls)
	}
}
