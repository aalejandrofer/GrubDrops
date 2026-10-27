package runner

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/plan"
	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/platform/platformtest"
)

// setLive scripts login's LookupChannel result.
func setLive(f *platformtest.Fake, login string, live bool) {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	f.Lookups[login] = platformtest.Lookup{
		Stream: platform.Stream{Channel: login, ChannelID: "id-" + login, BroadcastID: "bc-" + login, ViewerCount: 7},
		Live:   live,
	}
}

// forceCfg wires ForceChannels and a ForceLookup backed by f's scripted lookups.
func forceCfg(f *platformtest.Fake, channels ...string) func(*Config) {
	return func(c *Config) {
		// Step-driven tests tick back to back: let every tick beacon.
		c.TickInterval = time.Nanosecond
		c.ForceChannels = func() []string { return append([]string(nil), channels...) }
		c.ForceLookup = func(ctx context.Context, login string) (platform.Stream, bool, error) {
			return f.LookupChannel(ctx, platform.Session{}, login)
		}
	}
}

// dueRecheck makes the next force tick run its 5-minute recheck.
func dueRecheck(r *Runner) {
	r.mu.Lock()
	r.forceCheckAt = time.Time{}
	r.mu.Unlock()
}

func forceOn(t *testing.T, r *Runner, login string) Snapshot {
	t.Helper()
	s := r.Snapshot()
	if s.State != StateForceWatching || s.Channel != login || s.Current != nil {
		t.Fatalf("want force_watching on %s, got %+v", login, s)
	}
	return s
}

func TestForceWatch_StartsWhenQueueEmpty(t *testing.T) {
	f := platformtest.NewFake()
	setLive(f, "a", false)
	setLive(f, "b", true)
	r := newRunner(f, forceCfg(f, "a", "b"))
	steps(t, r, 1) // pick
	s := forceOn(t, r, "b")
	if s.ViewerCount != 7 {
		t.Fatalf("viewers=%d", s.ViewerCount)
	}
	if left := time.Until(s.NextScanAt); left > recheckInterval || left < recheckInterval-time.Minute {
		t.Fatalf("next scan in %v, want about %v", left, recheckInterval)
	}
	if callIndex(f, "Lookup:a", 1) > callIndex(f, "Lookup:b", 1) {
		t.Fatalf("lookups out of order: %v", f.Calls)
	}
	r.mu.Lock()
	st := r.stream
	r.mu.Unlock()
	if st == nil || st.ChannelID != "id-b" || st.BroadcastID != "bc-b" {
		t.Fatalf("stream passed to StartWatch: %+v", st)
	}
	if !hasCall(f, "Start:b") {
		t.Fatalf("calls=%v", f.Calls)
	}
	steps(t, r, 1) // force tick
	if !hasCall(f, "Heartbeat:b") {
		t.Fatalf("no heartbeat: %v", f.Calls)
	}
	forceOn(t, r, "b")
}

func TestForceWatch_DropWins(t *testing.T) {
	f := platformtest.NewFake()
	setLive(f, "b", true)
	r := newRunner(f, forceCfg(f, "b"))
	steps(t, r, 2) // pick, force tick
	forceOn(t, r, "b")

	f.Mu.Lock()
	f.Campaigns = []platform.Campaign{camp("c1", "Rust", "d1")}
	f.Streams["c1"] = []platform.Stream{{Channel: "x", ViewerCount: 3}}
	f.Mu.Unlock()
	steps(t, r, 1) // force tick, no recheck due
	forceOn(t, r, "b")

	dueRecheck(r)
	steps(t, r, 1) // force tick with recheck: drop wins
	if !hasCall(f, "Stop:b") || r.State() != StatePicking {
		t.Fatalf("state=%s calls=%v", r.State(), f.Calls)
	}
	steps(t, r, 2) // pick, start
	s := r.Snapshot()
	if s.State != StateWatching || s.Channel != "x" || s.Current == nil || s.Current.Benefit.ID != "d1" {
		t.Fatalf("%+v", s)
	}
}

func TestForceWatch_SwitchesToHigherRankAndOffline(t *testing.T) {
	f := platformtest.NewFake()
	setLive(f, "a", false)
	setLive(f, "b", true)
	r := newRunner(f, forceCfg(f, "a", "b"))
	steps(t, r, 1)
	forceOn(t, r, "b")

	setLive(f, "a", true)
	steps(t, r, 1) // no recheck due: stays
	forceOn(t, r, "b")
	dueRecheck(r)
	steps(t, r, 1)
	if !hasCall(f, "Stop:b") || !hasCall(f, "Start:a") {
		t.Fatalf("calls=%v", f.Calls)
	}
	forceOn(t, r, "a")
	steps(t, r, 1) // the new channel beacons
	if !hasCall(f, "Heartbeat:a") {
		t.Fatalf("calls=%v", f.Calls)
	}

	setLive(f, "a", false)
	setLive(f, "b", false)
	dueRecheck(r)
	steps(t, r, 1)
	if !hasCall(f, "Stop:a") {
		t.Fatalf("calls=%v", f.Calls)
	}
	s := r.Snapshot()
	if s.State != StateSleeping || s.Channel != "" {
		t.Fatalf("%+v", s)
	}
	if left := time.Until(s.NextScanAt); left > recheckInterval || left < recheckInterval-time.Minute {
		t.Fatalf("next scan in %v, want about %v", left, recheckInterval)
	}
}

// No force channel live at pick: sleep with the 5-minute recheck, not the
// empty-queue idle wait.
func TestForceWatch_NoneLiveSleepsShort(t *testing.T) {
	f := platformtest.NewFake()
	setLive(f, "a", false)
	r := newRunner(f, forceCfg(f, "a"))
	steps(t, r, 1)
	s := r.Snapshot()
	if s.State != StateSleeping {
		t.Fatalf("%+v", s)
	}
	if left := time.Until(s.NextScanAt); left > recheckInterval || left < recheckInterval-time.Minute {
		t.Fatalf("next scan in %v, want about %v", left, recheckInterval)
	}
	if f.CallCount("Start:") != 0 {
		t.Fatalf("calls=%v", f.Calls)
	}
}

func TestForceWatch_NoGamesStillWatches(t *testing.T) {
	f := platformtest.NewFake()
	setLive(f, "b", true)
	r := newRunner(f, func(c *Config) {
		forceCfg(f, "b")(c)
		c.Games = nil
	})
	steps(t, r, 1)
	forceOn(t, r, "b")
	if f.CallCount("List") != 0 {
		t.Fatalf("scanned with no games: %v", f.Calls)
	}
	dueRecheck(r)
	steps(t, r, 1)
	forceOn(t, r, "b")
	if f.CallCount("List") != 0 {
		t.Fatalf("rechecked the plan with no games: %v", f.Calls)
	}

	f2 := platformtest.NewFake()
	setLive(f2, "b", true)
	r2 := newRunner(f2, func(c *Config) {
		forceCfg(f2)(c)
		c.Games = nil
	})
	steps(t, r2, 1)
	if r2.State() != StateNoGames {
		t.Fatalf("state=%s", r2.State())
	}

	r3 := newRunner(f2, func(c *Config) {
		forceCfg(f2, "b")(c)
		c.ForceLookup = nil // feature off
		c.Games = nil
	})
	steps(t, r3, 1)
	if r3.State() != StateNoGames {
		t.Fatalf("state=%s", r3.State())
	}
}

func TestForceWatch_LookupErrorSkipsChannel(t *testing.T) {
	f := platformtest.NewFake()
	f.Lookups["a"] = platformtest.Lookup{Err: errors.New("boom")}
	setLive(f, "b", true)
	r := newRunner(f, forceCfg(f, "a", "b"))
	steps(t, r, 1)
	forceOn(t, r, "b")
}

func TestForceWatch_StreamDownSwitches(t *testing.T) {
	p := psFake()
	setLive(p.Fake, "a", false)
	setLive(p.Fake, "b", true)
	r := New(Config{AccountID: "acc", Platform: "twitch", Backend: p, Games: rustGames})
	r.cfg.ForceChannels = func() []string { return []string{"a", "b"} }
	r.cfg.ForceLookup = func(ctx context.Context, login string) (platform.Stream, bool, error) {
		return p.Fake.LookupChannel(ctx, platform.Session{}, login)
	}
	steps(t, r, 1)
	forceOn(t, r, "b")
	h := hooksOf(t, p)

	h.OnStreamDown("id-other")
	forceOn(t, r, "b")
	if hasCall(p.Fake, "Stop:b") {
		t.Fatalf("other channel acted: %v", p.Calls)
	}

	setLive(p.Fake, "a", true)
	setLive(p.Fake, "b", false)
	h.OnStreamDown("id-b")
	if !hasCall(p.Fake, "Stop:b") || r.State() != StatePicking {
		t.Fatalf("state=%s calls=%v", r.State(), p.Calls)
	}
	steps(t, r, 1)
	forceOn(t, r, "a")
}

// A force channel whose watch fails to start is passed over for
// recheckInterval, so a live lower-ranked channel is watched instead of the
// runner retrying the broken one forever.
func TestForceWatch_StartFailureFallsThrough(t *testing.T) {
	f := platformtest.NewFake()
	setLive(f, "a", true)
	setLive(f, "b", true)
	f.StartErrs = map[string]error{"a": errors.New("no sidecar")}
	r := newRunner(f, forceCfg(f, "a", "b"))
	steps(t, r, 1) // pick: a fails, b starts
	forceOn(t, r, "b")
	if c := f.CallCount("Start:a"); c != 1 {
		t.Fatalf("start a=%d calls=%v", c, f.Calls)
	}

	// Within recheckInterval the recheck keeps b and does not retry a.
	dueRecheck(r)
	steps(t, r, 1)
	forceOn(t, r, "b")
	if c := f.CallCount("Start:a"); c != 1 {
		t.Fatalf("start a=%d calls=%v", c, f.Calls)
	}

	// After recheckInterval a is eligible again and, now working, wins.
	r.mu.Lock()
	for login := range r.forceFailed {
		r.forceFailed[login] = time.Now().Add(-time.Second)
	}
	r.mu.Unlock()
	f.Mu.Lock()
	f.StartErrs = nil
	f.Mu.Unlock()
	dueRecheck(r)
	steps(t, r, 1)
	forceOn(t, r, "a")
	if c := f.CallCount("Start:a"); c != 2 {
		t.Fatalf("start a=%d calls=%v", c, f.Calls)
	}
}

// Every live force channel failing to start leaves the runner asleep at the
// 5-minute pace, not in a tight retry loop.
func TestForceWatch_AllStartsFailSleeps(t *testing.T) {
	f := platformtest.NewFake()
	setLive(f, "a", true)
	f.StartErr = errors.New("no sidecar")
	r := newRunner(f, forceCfg(f, "a"))
	err := r.step(context.Background())
	if err == nil {
		t.Fatalf("want start error, state=%s", r.State())
	}
	r.setState(context.Background(), StatePicking)
	steps(t, r, 1) // re-pick after backoff: a is still marked failed
	if s := r.Snapshot(); s.State != StateSleeping {
		t.Fatalf("state=%s calls=%v", s.State, f.Calls)
	}
	if c := f.CallCount("Start:a"); c != 1 {
		t.Fatalf("start a=%d", c)
	}
	r.mu.Lock()
	slow := r.sleepNoStream
	r.mu.Unlock()
	if !slow {
		t.Fatal("want 5-minute recheck pace")
	}
}

func TestForceWatch_ClaimsReadyDrops(t *testing.T) {
	f := platformtest.NewFake()
	sf := &platformtest.SweepFake{Fake: f}
	setLive(f, "b", true)
	f.Ready = []platform.ReadyDrop{{
		Campaign: platform.Campaign{ID: "cx", Platform: "twitch", Name: "Spring Drops", Game: "Rust", AccountLinked: true},
		Benefit:  platform.DropBenefit{ID: "r1", CampaignID: "cx", Name: "Ready1", RequiredMinutes: 30, InstanceID: "inst"},
	}}
	n := &spyNotifier{}
	cl := &fakeClaims{}
	cfg := Config{AccountID: "acc", Platform: "twitch", Backend: sf, Games: rustGames, Notifier: n, Claims: cl}
	forceCfg(f, "b")(&cfg)
	r := New(cfg)
	steps(t, r, 2) // pick, force tick
	forceOn(t, r, "b")
	if c := f.CallCount("Claim:r1"); c != 1 {
		t.Fatalf("claims=%d calls=%v", c, f.Calls)
	}
	if got := cl.recorded(); len(got) != 1 || got[0] != "acc:r1:" {
		t.Fatalf("recorded=%v", got)
	}
	cl.mu.Lock()
	rec := cl.recs[0].Target
	cl.mu.Unlock()
	if rec.Campaign.ID != "cx" || rec.Campaign.Platform != "twitch" || rec.Benefit.InstanceID != "inst" ||
		rec.Campaign.Name != "Spring Drops" || rec.Campaign.Game != "Rust" {
		t.Fatalf("target=%+v", rec)
	}
	cn := n.of("claim")
	if len(cn) != 1 || cn[0].fields["drop"] != "Ready1" || cn[0].fields["channel"] != "b" ||
		cn[0].fields["cur_min"] != 30 || cn[0].fields["req_min"] != 30 ||
		cn[0].fields["game"] != "Rust" || cn[0].fields["campaign"] != "Spring Drops" {
		t.Fatalf("notify=%+v", cn)
	}
	if sf.SweepCount() != 1 {
		t.Fatalf("sweeps=%d", sf.SweepCount())
	}

	// Inventory lag: the drop still reads ready. It is not claimed twice.
	steps(t, r, 1)
	if c := f.CallCount("Claim:r1"); c != 1 {
		t.Fatalf("claims=%d", c)
	}
	if sf.SweepCount() != 2 {
		t.Fatalf("sweeps=%d", sf.SweepCount())
	}
	forceOn(t, r, "b")
	r.mu.Lock()
	lc := r.lastClaimed
	r.mu.Unlock()
	if lc == nil || lc.Benefit.ID != "r1" {
		t.Fatalf("lastClaimed=%+v", lc)
	}
}

// A ready drop whose campaign needs a publisher account link the account has
// not made is never attempted: the claim would be refused.
func TestForceWatch_ReadyDropUnlinkedNotClaimed(t *testing.T) {
	f := platformtest.NewFake()
	setLive(f, "b", true)
	f.Ready = []platform.ReadyDrop{
		{
			Campaign: platform.Campaign{ID: "cu", Platform: "twitch", AccountLinked: false,
				AccountLinkChecked: true, AccountLinkURL: "https://publisher.example/link"},
			Benefit: platform.DropBenefit{ID: "unlinked", CampaignID: "cu", RequiredMinutes: 30},
		},
		{
			Campaign: platform.Campaign{ID: "cl", Platform: "twitch", AccountLinked: false, AccountLinkChecked: true},
			Benefit:  platform.DropBenefit{ID: "nolink", CampaignID: "cl", RequiredMinutes: 30},
		},
	}
	r := newRunner(f, forceCfg(f, "b"))
	steps(t, r, 3) // pick, tick, tick
	forceOn(t, r, "b")
	if c := f.CallCount("Claim:unlinked"); c != 0 {
		t.Fatalf("unlinked claims=%d calls=%v", c, f.Calls)
	}
	if c := f.CallCount("Claim:nolink"); c != 1 {
		t.Fatalf("no-link-needed claims=%d calls=%v", c, f.Calls)
	}
}

// A failed ready-drop claim is retried only after recheckInterval, so a
// refused claim cannot fire every tick.
func TestForceWatch_ReadyClaimFailureBacksOff(t *testing.T) {
	f := platformtest.NewFake()
	setLive(f, "b", true)
	f.Ready = []platform.ReadyDrop{{Benefit: platform.DropBenefit{ID: "r1", CampaignID: "cx", RequiredMinutes: 30}}}
	f.ClaimErr = errors.New("nope")
	r := newRunner(f, forceCfg(f, "b"))
	steps(t, r, 3) // pick, tick, tick
	if c := f.CallCount("Claim:r1"); c != 1 {
		t.Fatalf("claims=%d", c)
	}
	forceOn(t, r, "b")
	r.mu.Lock()
	for id := range r.forceClaimRetry {
		r.forceClaimRetry[id] = time.Now().Add(-time.Second)
	}
	r.mu.Unlock()
	f.Mu.Lock()
	f.ClaimErr = nil
	f.Mu.Unlock()
	steps(t, r, 1)
	if c := f.CallCount("Claim:r1"); c != 2 {
		t.Fatalf("claims=%d", c)
	}
}

func TestForceWatch_RescanEndsIt(t *testing.T) {
	f := platformtest.NewFake()
	setLive(f, "b", true)
	r := newRunner(f, func(c *Config) {
		forceCfg(f, "b")(c)
		c.TickInterval = time.Hour // Rescan must not wait for the next beacon
	})
	stop := runUntil(t, r)
	defer stop()
	waitSnap(t, r, func(s Snapshot) bool { return s.State == StateForceWatching && s.Channel == "b" })
	if !waitFor(func() bool { return f.CallCount("Heartbeat:b") == 1 }, time.Second) {
		t.Fatalf("calls=%v", f.Calls)
	}

	r.Rescan()
	if !waitFor(func() bool { return hasCall(f, "Stop:b") && f.CallCount("Start:b") == 2 }, 2*time.Second) {
		t.Fatalf("rescan did not restart the force watch: %v", f.Calls)
	}
	waitSnap(t, r, func(s Snapshot) bool { return s.State == StateForceWatching && s.Channel == "b" })
	if f.CallCount("List") < 2 {
		t.Fatalf("no plan scan after Rescan: %v", f.Calls)
	}
}

func TestForceWatch_MineNowEndsIt(t *testing.T) {
	f := platformtest.NewFake()
	setLive(f, "b", true)
	r := newRunner(f, func(c *Config) {
		forceCfg(f, "b")(c)
		c.TickInterval = time.Hour
	})
	stop := runUntil(t, r)
	defer stop()
	waitSnap(t, r, func(s Snapshot) bool { return s.State == StateForceWatching })

	// A drop shows up between rechecks and the user mines it now.
	c1 := camp("c1", "Rust", "d1")
	f.Mu.Lock()
	f.Campaigns = []platform.Campaign{c1}
	f.Streams["c1"] = []platform.Stream{{Channel: "x", ViewerCount: 3}}
	f.Mu.Unlock()
	r.mu.Lock()
	r.queue = []plan.Target{{Campaign: c1, Benefit: c1.Benefits[0]}}
	r.mu.Unlock()
	if err := r.MineNow("d1"); err != nil {
		t.Fatal(err)
	}
	waitSnap(t, r, mining("d1"))
	if !hasCall(f, "Stop:b") {
		t.Fatalf("calls=%v", f.Calls)
	}
}

// A drop the claim log already holds (just claimed by the plan path, with
// the inventory lagging) is not claimed again.
func TestForceWatch_ReadySkipsLoggedClaim(t *testing.T) {
	f := platformtest.NewFake()
	setLive(f, "b", true)
	f.Ready = []platform.ReadyDrop{{Benefit: platform.DropBenefit{ID: "r1", CampaignID: "cx", RequiredMinutes: 30}}}
	cl := &fakeClaims{ids: map[string]bool{"r1": true}}
	r := newRunner(f, func(c *Config) {
		forceCfg(f, "b")(c)
		c.Claims = cl
	})
	steps(t, r, 2)
	forceOn(t, r, "b")
	if c := f.CallCount("Claim:"); c != 0 {
		t.Fatalf("claims=%d", c)
	}
}

// A queued drop with no live stream does not end the force watch: only a
// queued drop that can be watched wins.
func TestForceWatch_QueuedNoStreamKeepsForce(t *testing.T) {
	f := platformtest.NewFake()
	f.Campaigns = []platform.Campaign{camp("c1", "Rust", "d1")} // no streams
	setLive(f, "b", true)
	r := newRunner(f, forceCfg(f, "b"))
	steps(t, r, 2) // pick, start: no stream -> force watch
	forceOn(t, r, "b")
	for range 3 {
		dueRecheck(r)
		steps(t, r, 1)
		forceOn(t, r, "b")
	}
	if n := f.CallCount("Start:"); n != 1 {
		t.Fatalf("starts=%d calls=%v", n, f.Calls)
	}
	if hasCall(f, "Stop:b") {
		t.Fatalf("calls=%v", f.Calls)
	}

	f.Mu.Lock()
	f.Streams["c1"] = []platform.Stream{{Channel: "x", ViewerCount: 3}}
	f.Mu.Unlock()
	dueRecheck(r)
	steps(t, r, 1)
	if !hasCall(f, "Stop:b") || r.State() != StatePicking {
		t.Fatalf("state=%s calls=%v", r.State(), f.Calls)
	}
	steps(t, r, 2) // pick, start
	if s := r.Snapshot(); s.State != StateWatching || s.Channel != "x" {
		t.Fatalf("%+v", s)
	}
}

// A stale rescan token or early wake must not beacon before TickInterval.
func TestForceWatch_NoEarlyBeat(t *testing.T) {
	f := platformtest.NewFake()
	setLive(f, "b", true)
	r := newRunner(f, func(c *Config) {
		forceCfg(f, "b")(c)
		c.TickInterval = time.Hour
	})
	steps(t, r, 3) // pick, tick (beats), tick (too early)
	if n := f.CallCount("Heartbeat:"); n != 1 {
		t.Fatalf("heartbeats=%d", n)
	}
	forceOn(t, r, "b")
}

// A ready drop whose claim keeps failing is given up after
// claimFailSkipThreshold attempts.
func TestForceWatch_ReadyClaimGivesUp(t *testing.T) {
	f := platformtest.NewFake()
	setLive(f, "b", true)
	f.Ready = []platform.ReadyDrop{{Benefit: platform.DropBenefit{ID: "r1", CampaignID: "cx", RequiredMinutes: 30}}}
	f.ClaimErr = errors.New("nope")
	r := newRunner(f, forceCfg(f, "b"))
	steps(t, r, 1)
	for range claimFailSkipThreshold + 2 {
		r.mu.Lock()
		for id := range r.forceClaimRetry {
			r.forceClaimRetry[id] = time.Now().Add(-time.Second)
		}
		r.mu.Unlock()
		steps(t, r, 1)
	}
	if c := f.CallCount("Claim:r1"); c != claimFailSkipThreshold {
		t.Fatalf("claims=%d want %d", c, claimFailSkipThreshold)
	}
	forceOn(t, r, "b")
}

// A ready drop refused until linked sends the link notice once and is not
// retried.
func TestForceWatch_ReadyNeedsLinkNotifies(t *testing.T) {
	f := platformtest.NewFake()
	setLive(f, "b", true)
	f.Ready = []platform.ReadyDrop{{Benefit: platform.DropBenefit{ID: "r1", CampaignID: "cx", Name: "Ready1", RequiredMinutes: 30}}}
	f.ClaimErr = fmt.Errorf("x: %w", platform.ErrClaimNeedsLink)
	n := &spyNotifier{}
	r := newRunner(f, func(c *Config) {
		forceCfg(f, "b")(c)
		c.Notifier = n
	})
	steps(t, r, 4)
	if c := f.CallCount("Claim:r1"); c != 1 {
		t.Fatalf("claims=%d", c)
	}
	errs := n.of("error")
	if len(errs) != 1 || errs[0].fields["drop"] != "Ready1" {
		t.Fatalf("error notices=%+v", errs)
	}
	forceOn(t, r, "b")
}

// A transient scan error with force channels still force watches.
func TestForceWatch_ScanErrorStillWatches(t *testing.T) {
	f := platformtest.NewFake()
	f.ListErr = errors.New("boom")
	setLive(f, "b", true)
	r := newRunner(f, forceCfg(f, "b"))
	steps(t, r, 1)
	forceOn(t, r, "b")

	f2 := platformtest.NewFake()
	f2.ListErr = errors.New("boom")
	r2 := newRunner(f2, nil)
	if err := r2.step(context.Background()); err == nil {
		t.Fatal("want the scan error without force channels")
	}
}

// An edited force list is rechecked by a force-watching runner at its next
// beat. It keeps its watch when its channel is still the best live one and
// moves when a better one is live, without a rebuild or a forced restart.
func TestForceWatch_ListChangedRechecksInPlace(t *testing.T) {
	f := platformtest.NewFake()
	setLive(f, "a", false)
	setLive(f, "b", true)
	setLive(f, "c", true)
	var mu sync.Mutex
	list := []string{"a", "b"}
	r := newRunner(f, func(c *Config) {
		forceCfg(f)(c)
		c.ForceChannels = func() []string {
			mu.Lock()
			defer mu.Unlock()
			return append([]string(nil), list...)
		}
	})
	steps(t, r, 1)
	forceOn(t, r, "b")

	// Reorder: b is still the best live channel. The watch is kept.
	mu.Lock()
	list = []string{"b", "a"}
	mu.Unlock()
	r.ForceListChanged()
	if r.forceScan.Load() {
		t.Fatal("a force watch must not be ended by a list edit")
	}
	lookups := f.CallCount("Lookup:b")
	steps(t, r, 1)
	forceOn(t, r, "b")
	if f.CallCount("Lookup:b") == lookups {
		t.Fatalf("no recheck after the edit: %v", f.Calls)
	}
	if hasCall(f, "Stop:b") || f.CallCount("Start:b") != 1 {
		t.Fatalf("watch restarted: %v", f.Calls)
	}

	// c is added on top and live: the runner moves to it.
	mu.Lock()
	list = []string{"c", "b", "a"}
	mu.Unlock()
	r.ForceListChanged()
	steps(t, r, 1)
	forceOn(t, r, "c")
	if !hasCall(f, "Stop:b") {
		t.Fatalf("calls=%v", f.Calls)
	}
}

// An idle runner re-picks at once on a list edit; a runner mining a drop is
// left alone.
func TestForceWatch_ListChangedWakesIdleOnly(t *testing.T) {
	f := platformtest.NewFake()
	setLive(f, "a", false)
	r := newRunner(f, forceCfg(f, "a"))
	steps(t, r, 1)
	if r.State() != StateSleeping {
		t.Fatalf("state=%s", r.State())
	}
	r.ForceListChanged()
	if len(r.rescan) != 1 {
		t.Fatal("sleeping runner not woken")
	}

	w := watching(t, platformtest.NewFake(), nil)
	w.ForceListChanged()
	if len(w.rescan) != 0 || w.forceScan.Load() {
		t.Fatal("a runner mining a drop must be left alone")
	}
}
