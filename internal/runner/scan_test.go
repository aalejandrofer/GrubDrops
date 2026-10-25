package runner

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

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
	_, p := psWatching(t, func(c *Config) { c.Claims = cl })
	hooksOf(t, p).OnRewardCode("n", "XYZ", "t")
	if len(cl.recs) != 1 || cl.recs[0].Code != "XYZ" || cl.recs[0].Target.Benefit.ID != "d1" ||
		cl.recs[0].Target.Campaign.Game != "Rust" {
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
