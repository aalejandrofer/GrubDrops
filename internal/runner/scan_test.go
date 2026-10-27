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

type hookRec struct {
	mu       sync.Mutex
	scans    []Scan
	progress [][]platform.Progress
	beats    int
}

func (h *hookRec) apply(c *Config) {
	c.OnScan = func(_ context.Context, s Scan) {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.scans = append(h.scans, s)
	}
	c.OnProgress = func(_ context.Context, acc string, p []platform.Progress) {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.progress = append(h.progress, p)
	}
	c.OnBeat = func(_ context.Context, acc string) {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.beats++
	}
}

func (h *hookRec) nScans() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.scans)
}

func TestRunner_OnScanCarriesPlan(t *testing.T) {
	f := platformtest.NewFake()
	f.Campaigns = []platform.Campaign{camp("c1", "Rust", "b1", "b2")}
	h := &hookRec{}
	r := newRunner(f, func(c *Config) {
		c.Claims = &fakeClaims{ids: map[string]bool{"b2": true}}
		h.apply(c)
	})
	steps(t, r, 1)
	if len(h.scans) != 1 {
		t.Fatalf("scans=%d", len(h.scans))
	}
	s := h.scans[0]
	if s.AccountID != "acc" || !s.InventoryOK || !s.Logged["b2"] || len(s.Logged) != 1 || s.At.IsZero() || s.Err != nil {
		t.Fatalf("%+v", s)
	}
	if len(s.Result.Queue) != 1 || s.Result.Queue[0].Benefit.ID != "b1" ||
		len(s.Result.Claimed) != 1 || s.Result.Claimed[0].Benefit.ID != "b2" {
		t.Fatalf("%+v", s.Result)
	}
}

func TestRunner_OnScanReportsListError(t *testing.T) {
	f := platformtest.NewFake()
	f.ListErr = errors.New("boom")
	h := &hookRec{}
	r := newRunner(f, h.apply)
	if err := r.step(context.Background()); !errors.Is(err, f.ListErr) {
		t.Fatalf("err=%v", err)
	}
	if len(h.scans) != 1 || h.scans[0].Err == nil || len(h.scans[0].Campaigns) != 0 {
		t.Fatalf("%+v", h.scans)
	}
}

func TestRunner_RescanWhileWatching(t *testing.T) {
	old := rescanEvery
	t.Cleanup(func() { rescanEvery = old })

	// Default cadence: a tick makes no extra campaigns call.
	f := platformtest.NewFake()
	r := watching(t, f, nil)
	steps(t, r, 1)
	if n := f.CallCount("List"); n != 1 {
		t.Fatalf("default: list=%d", n)
	}

	rescanEvery = 0
	f = platformtest.NewFake()
	f.Progress["d1"] = &platform.Progress{BenefitID: "d1", MinutesWatched: 1}
	h := &hookRec{}
	r = watching(t, f, h.apply)
	before := r.Snapshot().Current.Benefit.ID
	stops := f.CallCount("Stop:")
	steps(t, r, 1)
	if f.CallCount("List") != 2 || f.CallCount("Inventory") != 2 {
		t.Fatalf("list=%d inventory=%d", f.CallCount("List"), f.CallCount("Inventory"))
	}
	s := r.Snapshot()
	if h.nScans() != 2 || s.State != StateWatching || s.Current.Benefit.ID != before || f.CallCount("Stop:") != stops {
		t.Fatalf("scans=%d %+v", h.nScans(), s)
	}
	if !h.scans[1].InventoryOK || len(h.scans[1].Progress) == 0 {
		t.Fatalf("%+v", h.scans[1])
	}
}

func TestRunner_RescanErrorKeepsWatching(t *testing.T) {
	old := rescanEvery
	t.Cleanup(func() { rescanEvery = old })
	f := platformtest.NewFake()
	r := watching(t, f, nil)
	rescanEvery = 0
	f.Mu.Lock()
	f.ListErr = errors.New("boom")
	f.Mu.Unlock()
	steps(t, r, 1)
	if r.State() != StateWatching || r.Snapshot().Current == nil {
		t.Fatal(r.State())
	}
}

func TestRunner_OnProgressAfterPoll(t *testing.T) {
	f := platformtest.NewFake()
	f.Progress["d1"] = &platform.Progress{BenefitID: "d1", MinutesWatched: 3}
	h := &hookRec{}
	r := watching(t, f, h.apply)
	steps(t, r, 1)
	if len(h.progress) != 1 || len(h.progress[0]) != 1 || h.progress[0][0].BenefitID != "d1" {
		t.Fatalf("%+v", h.progress)
	}
	f.Mu.Lock()
	f.InventoryErr = errors.New("down")
	f.Mu.Unlock()
	if err := r.step(context.Background()); err == nil {
		t.Fatal("want inventory error")
	}
	if len(h.progress) != 1 {
		t.Fatalf("progress=%d", len(h.progress))
	}
}

func TestRunner_OnBeatPerHeartbeat(t *testing.T) {
	f := platformtest.NewFake()
	h := &hookRec{}
	r := watching(t, f, h.apply)
	steps(t, r, 3)
	if h.beats != 3 {
		t.Fatalf("beats=%d", h.beats)
	}
	f.Mu.Lock()
	f.HeartbeatErr = errors.New("down")
	f.Mu.Unlock()
	if err := r.step(context.Background()); err == nil {
		t.Fatal("want heartbeat error")
	}
	if h.beats != 3 {
		t.Fatalf("beats=%d", h.beats)
	}
}

func TestRunner_ClaimRecordsTarget(t *testing.T) {
	f := platformtest.NewFake()
	r, _, cl, _ := claimSetup(f, 2)
	f.Campaigns[0].Name = "Camp"
	steps(t, r, 5)
	if len(cl.recs) != 1 {
		t.Fatalf("recs=%v", cl.recs)
	}
	g := cl.recs[0]
	if g.Target.Campaign.Game != "Rust" || g.Target.Campaign.Name != "Camp" ||
		g.Target.Benefit.ID != "d1" || g.Code != "" {
		t.Fatalf("%+v", g)
	}
}

func TestRunner_RewardCodeRecordsTarget(t *testing.T) {
	cl := &fakeClaims{}
	r, p := psWatching(t, func(c *Config) { c.Claims = cl })
	p.Progress["d1"] = &platform.Progress{BenefitID: "d1", MinutesWatched: 60}
	steps(t, r, 2) // tick -> claiming, claim
	if r.State() != StatePicking || len(cl.recs) != 1 {
		t.Fatalf("state=%s recs=%v", r.State(), cl.recorded())
	}
	hooksOf(t, p).OnRewardCode("n", "XYZ", "t")
	if len(cl.recs) != 2 || cl.recs[1].Code != "XYZ" || cl.recs[1].Target.Benefit.ID != "d1" ||
		cl.recs[1].Target.Campaign.Game != "Rust" {
		t.Fatalf("%+v", cl.recs)
	}
}

func TestRunner_FailedRescanWaitsFullInterval(t *testing.T) {
	old := rescanEvery
	t.Cleanup(func() { rescanEvery = old })
	f := platformtest.NewFake()
	r := watching(t, f, nil)
	rescanEvery = time.Hour
	f.Mu.Lock()
	f.ListErr = errors.New("boom")
	f.Mu.Unlock()
	// Force the first rescan due, let it fail, then expect no retry on later ticks.
	r.mu.Lock()
	r.lastScanTry = time.Now().Add(-2 * time.Hour)
	r.mu.Unlock()
	before := f.CallCount("List")
	steps(t, r, 3)
	if n := f.CallCount("List") - before; n != 1 {
		t.Fatalf("list calls after a failed rescan=%d, want 1", n)
	}
	if r.State() != StateWatching {
		t.Fatal(r.State())
	}
}

func TestRunner_IntegrityBlockedListNeedsLogin(t *testing.T) {
	f := platformtest.NewFake()
	f.ListErr = platform.ErrIntegrityBlocked
	r := newRunner(f, nil)
	steps(t, r, 1)
	if r.State() != StateNeedsLogin {
		t.Fatalf("state=%s", r.State())
	}
}

func TestRunner_InventoryFailureScanFlag(t *testing.T) {
	f := platformtest.NewFake()
	f.Campaigns = []platform.Campaign{camp("c1", "Rust", "b1")}
	f.Progress["b1"] = &platform.Progress{BenefitID: "b1", MinutesWatched: 9}
	f.InventoryErr = errors.New("inventory down")
	h := &hookRec{}
	r := newRunner(f, h.apply)
	steps(t, r, 1)
	if len(h.scans) != 1 {
		t.Fatalf("scans=%d", len(h.scans))
	}
	s := h.scans[0]
	if s.Err != nil || s.InventoryOK || s.Progress != nil || s.InventoryErr != "inventory down" {
		t.Fatalf("%+v", s)
	}
	if r.State() != StateStarting || r.Snapshot().Current == nil || r.Snapshot().Current.Benefit.ID != "b1" {
		t.Fatalf("state=%s current=%+v", r.State(), r.Snapshot().Current)
	}
}

func TestRunner_ClaimLogReadFailureIsScanError(t *testing.T) {
	old := rescanEvery
	t.Cleanup(func() { rescanEvery = old })

	// Pick: the failed read is a step error, nothing is picked.
	f := platformtest.NewFake()
	f.Campaigns = []platform.Campaign{camp("c1", "Rust", "b1")}
	cl := &fakeClaims{idsErr: errors.New("db locked")}
	h := &hookRec{}
	r := newRunner(f, func(c *Config) { c.Claims = cl; h.apply(c) })
	if err := r.step(context.Background()); err == nil {
		t.Fatal("want step error")
	}
	if len(h.scans) != 1 || h.scans[0].Err == nil || len(h.scans[0].Result.Queue) != 0 {
		t.Fatalf("%+v", h.scans)
	}
	if r.Snapshot().Current != nil {
		t.Fatalf("picked %+v", r.Snapshot().Current)
	}

	// Rescan while watching: the previous queue stays, the watch goes on.
	f = platformtest.NewFake()
	cl = &fakeClaims{}
	h = &hookRec{}
	r = watching(t, f, func(c *Config) { c.Claims = cl; h.apply(c) })
	r.mu.Lock()
	before := append([]plan.Target(nil), r.queue...)
	r.mu.Unlock()
	cl.mu.Lock()
	cl.idsErr = errors.New("db locked")
	cl.mu.Unlock()
	rescanEvery = 0
	steps(t, r, 1)
	if h.nScans() != 2 || h.scans[1].Err == nil {
		t.Fatalf("%+v", h.scans)
	}
	r.mu.Lock()
	after := r.queue
	r.mu.Unlock()
	if len(before) != 1 || len(after) != 1 || after[0].Benefit.ID != before[0].Benefit.ID {
		t.Fatalf("queue before=%v after=%v", before, after)
	}
	if r.State() != StateWatching {
		t.Fatal(r.State())
	}
}

func TestSnapshot_NextScanAt(t *testing.T) {
	f := platformtest.NewFake()
	f.Campaigns = []platform.Campaign{camp("c1", "Rust", "d1")}
	f.Streams["c1"] = []platform.Stream{{Channel: "a", ChannelID: "ida", ViewerCount: 10}}
	r := newRunner(f, nil)
	if got := r.Snapshot().NextScanAt; !got.IsZero() {
		t.Fatalf("before any scan: %v", got)
	}
	steps(t, r, 2)
	r.mu.Lock()
	last := r.lastScanTry
	r.mu.Unlock()
	if last.IsZero() {
		t.Fatal("no scan happened")
	}
	if got := r.Snapshot().NextScanAt; !got.Equal(last.Add(rescanEvery)) {
		t.Fatalf("NextScanAt=%v want %v", got, last.Add(rescanEvery))
	}
}

func TestSnapshot_NextScanAt_Sleeping(t *testing.T) {
	f := platformtest.NewFake() // no campaigns -> sleeping
	r := newRunner(f, nil)
	steps(t, r, 1)
	if r.State() != StateSleeping {
		t.Fatal(r.State())
	}
	r.mu.Lock()
	last := r.lastScanTry
	r.mu.Unlock()
	if got := r.Snapshot().NextScanAt; !got.Equal(last.Add(idleRecheck)) {
		t.Fatalf("fallback NextScanAt=%v want %v", got, last.Add(idleRecheck))
	}
	r.mu.Lock()
	r.state = StateAwaitingConnect
	r.mu.Unlock()
	if got := r.Snapshot().NextScanAt; !got.Equal(last.Add(recheckInterval)) {
		t.Fatalf("awaiting connect NextScanAt=%v want %v", got, last.Add(recheckInterval))
	}
	r.mu.Lock()
	r.state = StateSleeping
	r.mu.Unlock()
	dl := time.Now().Add(idleRecheck)
	r.mu.Lock()
	r.nextRecheck = dl
	r.mu.Unlock()
	if got := r.Snapshot().NextScanAt; !got.Equal(dl) {
		t.Fatalf("NextScanAt=%v want armed deadline %v", got, dl)
	}
}

func TestSnapshot_NextScanAt_NoStreamSleep(t *testing.T) {
	f := platformtest.NewFake()
	f.Campaigns = []platform.Campaign{camp("c1", "Rust", "d1")} // queued, no streams
	r := newRunner(f, nil)
	steps(t, r, 2) // pick, then start finds no stream
	if r.State() != StateSleeping {
		t.Fatal(r.State())
	}
	r.mu.Lock()
	last := r.lastScanTry
	r.mu.Unlock()
	if got := r.Snapshot().NextScanAt; !got.Equal(last.Add(5 * time.Minute)) {
		t.Fatalf("no-stream NextScanAt=%v want %v", got, last.Add(5*time.Minute))
	}

	f.Campaigns = nil // next pick has an empty queue
	r.setState(context.Background(), StatePicking)
	steps(t, r, 1)
	if r.State() != StateSleeping {
		t.Fatal(r.State())
	}
	r.mu.Lock()
	last = r.lastScanTry
	r.mu.Unlock()
	if got := r.Snapshot().NextScanAt; !got.Equal(last.Add(idleRecheck)) {
		t.Fatalf("empty-queue NextScanAt=%v want %v", got, last.Add(idleRecheck))
	}
}

func TestSnapshot_NextScanAt_StoppedIsZero(t *testing.T) {
	f := platformtest.NewFake()
	f.ListErr = fmt.Errorf("x: %w", platform.ErrIntegrityBlocked)
	r := newRunner(f, nil)
	steps(t, r, 1)
	if r.State() != StateNeedsLogin {
		t.Fatal(r.State())
	}
	if got := r.Snapshot().NextScanAt; !got.IsZero() {
		t.Fatalf("needs_login NextScanAt=%v want zero", got)
	}
}

func TestRunner_RescanForcesWatchScan(t *testing.T) {
	f := platformtest.NewFake()
	r := watching(t, f, nil)
	r.Rescan()
	steps(t, r, 1)
	if n := f.CallCount("List"); n != 2 {
		t.Fatalf("forced: list=%d", n)
	}
	steps(t, r, 1)
	if n := f.CallCount("List"); n != 2 {
		t.Fatalf("force cleared: list=%d", n)
	}
}

// Config.ScanEvery paces the watching rescan, the idle wait and NextScanAt.
func TestRunner_ScanEveryConfig(t *testing.T) {
	pace := 3 * time.Hour
	f := platformtest.NewFake()
	r := watching(t, f, func(c *Config) { c.ScanEvery = func() time.Duration { return pace } })
	r.mu.Lock()
	last := r.lastScanTry
	r.mu.Unlock()
	if got := r.Snapshot().NextScanAt; !got.Equal(last.Add(pace)) {
		t.Fatalf("NextScanAt=%v want %v", got, last.Add(pace))
	}
	// Not due yet: a tick makes no extra campaigns call.
	steps(t, r, 1)
	if n := f.CallCount("List"); n != 1 {
		t.Fatalf("list=%d", n)
	}
	// Due: the next tick rescans.
	r.mu.Lock()
	r.lastScanTry = time.Now().Add(-pace)
	r.mu.Unlock()
	steps(t, r, 1)
	if n := f.CallCount("List"); n != 2 {
		t.Fatalf("list after pace=%d", n)
	}

	idle := newRunner(platformtest.NewFake(), func(c *Config) { c.ScanEvery = func() time.Duration { return pace } })
	steps(t, idle, 1)
	if idle.State() != StateSleeping {
		t.Fatal(idle.State())
	}
	idle.mu.Lock()
	last = idle.lastScanTry
	idle.mu.Unlock()
	if got := idle.Snapshot().NextScanAt; !got.Equal(last.Add(pace)) {
		t.Fatalf("idle NextScanAt=%v want %v", got, last.Add(pace))
	}
}

// preemptSetup watches Rust/c1, then a Valorant campaign ranked above it
// appears for the next rescan.
func preemptSetup(t *testing.T, preempt bool) (*platformtest.Fake, *Runner) {
	t.Helper()
	old := rescanEvery
	t.Cleanup(func() { rescanEvery = old })
	f := platformtest.NewFake()
	games := []plan.Game{{Name: "Valorant", Slug: "valorant"}, {Name: "Rust", Slug: "rust"}}
	r := watching(t, f, func(c *Config) {
		c.Games = games
		c.Preempt = func() bool { return preempt }
	})
	f.Mu.Lock()
	f.Campaigns = append(f.Campaigns, camp("c0", "Valorant", "v1"))
	f.Streams["c0"] = []platform.Stream{{Channel: "b", ChannelID: "idb", ViewerCount: 5}}
	f.Mu.Unlock()
	rescanEvery = 0
	return f, r
}

func TestRunner_PreemptSwitchesToHigherRank(t *testing.T) {
	_, r := preemptSetup(t, true)
	steps(t, r, 1)
	if !r.switchNow.Load() {
		t.Fatal("no switch requested")
	}
	if !r.takeSwitch() {
		t.Fatal("takeSwitch=false")
	}
	r.stopWatch(context.Background())
	r.setState(context.Background(), StatePicking)
	steps(t, r, 1)
	if c := r.Snapshot().Current; c == nil || c.Benefit.ID != "v1" {
		t.Fatalf("current=%+v", c)
	}
}

func TestRunner_PreemptOffKeepsCurrent(t *testing.T) {
	_, r := preemptSetup(t, false)
	steps(t, r, 1)
	if r.switchNow.Load() {
		t.Fatal("switch requested with preempt off")
	}
}

func TestRunner_PreemptHoldsMineNowPin(t *testing.T) {
	_, r := preemptSetup(t, true)
	r.mu.Lock()
	p := *r.current
	r.pinned = &p
	r.mu.Unlock()
	steps(t, r, 1)
	if r.switchNow.Load() {
		t.Fatal("pin preempted")
	}
}

func TestRunner_PreemptSkipsOfflineCandidate(t *testing.T) {
	f, r := preemptSetup(t, true)
	f.Mu.Lock()
	delete(f.Streams, "c0")
	f.Mu.Unlock()
	steps(t, r, 1)
	if r.switchNow.Load() {
		t.Fatal("switched to a campaign with no stream")
	}
}

func TestRunner_PreemptSkipsStalledOnlyCandidate(t *testing.T) {
	_, r := preemptSetup(t, true)
	r.mu.Lock()
	r.stalled["b"] = time.Now().Add(time.Hour)
	r.mu.Unlock()
	steps(t, r, 1)
	if r.switchNow.Load() {
		t.Fatal("switched to a campaign whose only channel is stalled")
	}
}

// A preempt whose target start cannot watch falls back to the old drop and
// does not switch again at the next rescan.
func TestRunner_PreemptFailedTargetCoolsDown(t *testing.T) {
	f, r := preemptSetup(t, true)
	steps(t, r, 1)
	if !r.takeSwitch() {
		t.Fatal("no switch requested")
	}
	f.Mu.Lock()
	streams := f.Streams["c0"]
	delete(f.Streams, "c0")
	f.Mu.Unlock()
	r.stopWatch(context.Background())
	r.setState(context.Background(), StatePicking)
	steps(t, r, 2) // pick, then start
	if c := r.Snapshot().Current; c == nil || c.Benefit.ID == "v1" {
		t.Fatalf("current=%+v, want the fallback drop", c)
	}
	f.Mu.Lock()
	f.Streams["c0"] = streams
	f.Mu.Unlock()
	steps(t, r, 1)
	if r.switchNow.Load() {
		t.Fatal("preempted again to a campaign start could not watch")
	}
}
