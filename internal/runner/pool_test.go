package runner

import (
	"context"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/platform/platformtest"
)

// blockUnit blocks until ctx is done and counts starts.
type blockUnit struct {
	starts  *int32
	running atomic.Bool
}

func (u *blockUnit) Run(ctx context.Context) error {
	atomic.AddInt32(u.starts, 1)
	u.running.Store(true)
	defer u.running.Store(false)
	<-ctx.Done()
	return ctx.Err()
}
func (u *blockUnit) Snapshot() Snapshot { return Snapshot{} }

type panicUnit struct{}

func (panicUnit) Run(context.Context) error { panic("boom") }
func (panicUnit) Snapshot() Snapshot        { return Snapshot{AccountID: "p"} }

func blockBuilder(id string, starts *int32) (Builder, *blockUnit) {
	u := &blockUnit{starts: starts}
	return func() Entry { return Entry{ID: id, Unit: u} }, u
}

func TestPool_ReloadWithCancelledParentKeepsUnits(t *testing.T) {
	p := NewPool(nil)
	defer p.Stop()
	var s1, s2 int32
	b1, _ := blockBuilder("a", &s1)
	if err := p.Reload(context.Background(), []Builder{b1}); err != nil {
		t.Fatal(err)
	}
	dead, cancel := context.WithCancel(context.Background())
	cancel()
	b2, u2 := blockBuilder("a", &s2)
	if err := p.Reload(dead, []Builder{b2}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if !u2.running.Load() {
		t.Fatal("unit died with cancelled reload parent")
	}
}

func TestPool_ReloadAccountRestartsOnlyThatUnit(t *testing.T) {
	p := NewPool(nil)
	defer p.Stop()
	var sa, sb int32
	ba, _ := blockBuilder("a", &sa)
	bb, _ := blockBuilder("b", &sb)
	_ = p.Reload(context.Background(), []Builder{ba, bb})
	time.Sleep(30 * time.Millisecond)
	ba2, _ := blockBuilder("a", &sa)
	p.ReloadAccount("a", ba2)
	time.Sleep(30 * time.Millisecond)
	if atomic.LoadInt32(&sa) != 2 || atomic.LoadInt32(&sb) != 1 {
		t.Fatalf("starts a=%d b=%d", atomic.LoadInt32(&sa), atomic.LoadInt32(&sb))
	}
}

type spyN struct {
	mu sync.Mutex
	ev []map[string]any
}

func (n *spyN) Notify(_ context.Context, e string, f map[string]any) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if e == "error" {
		n.ev = append(n.ev, f)
	}
	return nil
}

func TestPool_PanicNotifiesError(t *testing.T) {
	n := &spyN{}
	p := NewPool(n)
	defer p.Stop()
	_ = p.Reload(context.Background(), []Builder{func() Entry { return Entry{ID: "p", Unit: panicUnit{}} }})
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		n.mu.Lock()
		got := len(n.ev)
		n.mu.Unlock()
		if got > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if len(n.ev) != 1 || n.ev[0]["panic"] == nil || n.ev[0]["account"] != "p" {
		t.Fatalf("events %v", n.ev)
	}
	if len(p.Snapshots()) != 1 {
		t.Fatal("snapshots broken")
	}
}

func TestPool_OrderSurvivesReloadAccount(t *testing.T) {
	p := NewPool(nil)
	defer p.Stop()
	var s int32
	b, _ := blockBuilder("a", &s)
	_ = p.Reload(context.Background(), []Builder{b})
	p.SetOrder("a", []string{"x"})
	b2, _ := blockBuilder("a", &s)
	p.ReloadAccount("a", b2)
	if got := p.OrderFunc("a")(); !reflect.DeepEqual(got, []string{"x"}) {
		t.Fatalf("got %v", got)
	}
}

func TestPool_ResetOrder(t *testing.T) {
	p := NewPool(nil)
	p.SetOrder("a", []string{"x"})
	p.ResetOrder("a")
	if got := p.OrderFunc("a")(); got != nil {
		t.Fatalf("got %v", got)
	}
}

func TestPool_SnapshotsInEntryOrder(t *testing.T) {
	p := NewPool(nil)
	defer p.Stop()
	_ = p.Reload(context.Background(), []Builder{
		func() Entry {
			return Entry{ID: "a", Unit: &Idle{AccountID: "a", Platform: "twitch", State: StateNeedsLogin}}
		},
		func() Entry {
			return Entry{ID: "b", Unit: &Idle{AccountID: "b", Platform: "kick", State: StateNoGames}}
		},
	})
	s := p.Snapshots()
	if len(s) != 2 || s[0].AccountID != "a" || s[0].State != StateNeedsLogin || s[1].AccountID != "b" || s[1].State != StateNoGames {
		t.Fatalf("got %+v", s)
	}
}

// poolRunner builds a Runner on f for "acc"; its first pick bootstraps f's
// PubSub client.
func poolRunner(t *testing.T, f *platformtest.PubSubFake, changes *atomic.Int32) (Builder, *Runner) {
	t.Helper()
	r := New(Config{AccountID: "acc", Platform: "twitch", Backend: f, Games: rustGames,
		OnChange: func(Snapshot) { changes.Add(1) }})
	return func() Entry { return Entry{ID: "acc", Unit: r} }, r
}

func waitWatching(t *testing.T, r *Runner) {
	t.Helper()
	if !waitFor(func() bool { return r.State() == StateWatching }, 2*time.Second) {
		t.Fatalf("state=%s", r.State())
	}
}

func poolFake() *platformtest.PubSubFake {
	f := psFake()
	f.Campaigns = []platform.Campaign{camp("c1", "Rust", "d1")}
	f.Campaigns[0].Benefits[0].RequiredMinutes = 100000
	f.Streams["c1"] = []platform.Stream{{Channel: "a", ChannelID: "42", ViewerCount: 1}}
	return f
}

// assertDeaf pushes to the bootstrapped hooks and fails if r reacts.
func assertDeaf(t *testing.T, f *platformtest.PubSubFake, r *Runner, changes *atomic.Int32) {
	t.Helper()
	h := hooksOf(t, f)
	before := changes.Load()
	h.OnDropProgress("d1", 500, 0)
	h.OnDropClaim("d1", "inst")
	h.OnStreamDown("42")
	if changes.Load() != before || r.Snapshot().MinutesWatched == 500 {
		t.Fatalf("dead runner reacted to a push: changes %d->%d", before, changes.Load())
	}
}

func TestPool_RemovedAccountGetsNoPushes(t *testing.T) {
	p := NewPool(nil)
	defer p.Stop()
	f := poolFake()
	var c atomic.Int32
	b, r := poolRunner(t, f, &c)
	_ = p.Reload(context.Background(), []Builder{b})
	waitWatching(t, r)
	_ = p.Reload(context.Background(), nil)
	assertDeaf(t, f, r, &c)
}

func TestPool_RunnerReplacedByIdleGetsNoPushes(t *testing.T) {
	p := NewPool(nil)
	defer p.Stop()
	f := poolFake()
	var c atomic.Int32
	b, r := poolRunner(t, f, &c)
	_ = p.Reload(context.Background(), []Builder{b})
	waitWatching(t, r)
	p.ReloadAccount("acc", func() Entry {
		return Entry{ID: "acc", Unit: &Idle{AccountID: "acc", State: StateNeedsLogin}}
	})
	assertDeaf(t, f, r, &c)

	// Reload keep path too.
	var c2 atomic.Int32
	b2, r2 := poolRunner(t, f, &c2)
	_ = p.Reload(context.Background(), []Builder{b2})
	waitWatching(t, r2)
	_ = p.Reload(context.Background(), []Builder{func() Entry {
		return Entry{ID: "acc", Unit: &Idle{AccountID: "acc", State: StateNoGames}}
	}})
	assertDeaf(t, f, r2, &c2)
}

func TestPool_RunnerReplacedByRunnerPushReachesNew(t *testing.T) {
	p := NewPool(nil)
	defer p.Stop()
	f := poolFake()
	var c1, c2 atomic.Int32
	b1, r1 := poolRunner(t, f, &c1)
	_ = p.Reload(context.Background(), []Builder{b1})
	waitWatching(t, r1) // bootstrapped with r1's registration
	b2, r2 := poolRunner(t, f, &c2)
	p.ReloadAccount("acc", b2)
	waitWatching(t, r2)
	before := c1.Load()
	hooksOf(t, f).OnDropProgress("d1", 500, 0)
	if got := r2.Snapshot().MinutesWatched; got != 500 {
		t.Fatalf("push did not reach the new Runner: minutes=%d", got)
	}
	if c1.Load() != before || r1.Snapshot().MinutesWatched == 500 {
		t.Fatal("push reached the replaced Runner")
	}
}

// panicBeat panics on the second Heartbeat, mid-watch.
type panicBeat struct {
	*platformtest.Fake
	beats atomic.Int32
}

func (p *panicBeat) Heartbeat(ctx context.Context, h platform.WatchHandle) error {
	if p.beats.Add(1) == 2 {
		panic("beacon boom")
	}
	return p.Fake.Heartbeat(ctx, h)
}

func TestPool_RunnerPanicStopsWatch(t *testing.T) {
	n := &spyN{}
	p := NewPool(n)
	defer p.Stop()
	f := platformtest.NewFake()
	f.Campaigns = []platform.Campaign{camp("c1", "Rust", "d1")}
	f.Campaigns[0].Benefits[0].RequiredMinutes = 100000
	f.Progress["d1"] = &platform.Progress{BenefitID: "d1"}
	f.Streams["c1"] = []platform.Stream{{Channel: "a", ViewerCount: 1}}
	r := New(Config{AccountID: "acc", Platform: "kick", Backend: &panicBeat{Fake: f}, Games: rustGames,
		TickInterval: time.Millisecond})
	_ = p.Reload(context.Background(), []Builder{func() Entry { return Entry{ID: "acc", Unit: r} }})
	if !waitFor(func() bool {
		n.mu.Lock()
		defer n.mu.Unlock()
		return len(n.ev) > 0
	}, 2*time.Second) {
		t.Fatal("no error notification")
	}
	if !hasCall(f, "Stop:a") {
		t.Fatalf("watch leaked on panic: %v", f.Calls)
	}
	if s := r.Snapshot(); s.State == StateWatching {
		t.Fatalf("snapshot still watching: %+v", s)
	}
}

// shortRestart shrinks panicRestart for one test.
func shortRestart(t *testing.T, d time.Duration) {
	old := panicRestart
	panicRestart = d
	t.Cleanup(func() { panicRestart = old })
}

type onceUnit struct {
	id      string
	panics  bool
	ran     chan struct{}
	runOnce sync.Once
}

func (u *onceUnit) Run(ctx context.Context) error {
	u.runOnce.Do(func() { close(u.ran) })
	if u.panics {
		panic("boom")
	}
	<-ctx.Done()
	return ctx.Err()
}
func (u *onceUnit) Snapshot() Snapshot { return Snapshot{AccountID: u.id} }

func TestPool_PanicRestartsFromBuilder(t *testing.T) {
	shortRestart(t, 10*time.Millisecond)
	var builds int32
	var secondP atomic.Pointer[onceUnit]
	b := func() Entry {
		n := atomic.AddInt32(&builds, 1)
		u := &onceUnit{id: "a", panics: n == 1, ran: make(chan struct{})}
		if n == 2 {
			secondP.Store(u)
		}
		return Entry{ID: "a", Unit: u}
	}
	p := NewPool(nil)
	defer p.Stop()
	_ = p.Reload(context.Background(), []Builder{b})
	deadline := time.Now().Add(2 * time.Second)
	for secondP.Load() == nil && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if atomic.LoadInt32(&builds) != 2 {
		t.Fatalf("builds %d", atomic.LoadInt32(&builds))
	}
	second := secondP.Load()
	select {
	case <-second.ran:
	case <-time.After(time.Second):
		t.Fatal("second unit never ran")
	}
	if got := p.Snapshots(); len(got) != 1 || got[0].AccountID != "a" {
		t.Fatalf("snapshots %v", got)
	}
	p.mu.Lock()
	cur := p.units[0].entry.Unit
	p.mu.Unlock()
	if cur != Unit(second) {
		t.Fatal("snapshots do not read the second unit")
	}
}

func TestPool_StopDuringPanicDelay(t *testing.T) {
	shortRestart(t, time.Hour)
	var builds int32
	p := NewPool(nil)
	ran := make(chan struct{})
	_ = p.Reload(context.Background(), []Builder{func() Entry {
		atomic.AddInt32(&builds, 1)
		return Entry{ID: "a", Unit: &onceUnit{id: "a", panics: true, ran: ran}}
	}})
	<-ran
	time.Sleep(50 * time.Millisecond)
	done := make(chan struct{})
	go func() { p.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Stop blocked on restart delay")
	}
	if atomic.LoadInt32(&builds) != 1 {
		t.Fatalf("builds %d", atomic.LoadInt32(&builds))
	}
}

func TestPool_ReloadAccountDuringPanicDelayWins(t *testing.T) {
	shortRestart(t, 200*time.Millisecond)
	var builds int32
	p := NewPool(nil)
	defer p.Stop()
	ran := make(chan struct{})
	_ = p.Reload(context.Background(), []Builder{func() Entry {
		atomic.AddInt32(&builds, 1)
		return Entry{ID: "a", Unit: &onceUnit{id: "a", panics: true, ran: ran}}
	}})
	<-ran
	p.ReloadAccount("a", nil)
	time.Sleep(400 * time.Millisecond)
	if atomic.LoadInt32(&builds) != 1 || len(p.Snapshots()) != 0 {
		t.Fatalf("removed unit resurrected: builds %d", builds)
	}
}

func TestPool_ErrStopNotRestarted(t *testing.T) {
	shortRestart(t, 10*time.Millisecond)
	var builds int32
	p := NewPool(nil)
	defer p.Stop()
	_ = p.Reload(context.Background(), []Builder{func() Entry {
		atomic.AddInt32(&builds, 1)
		return Entry{ID: "a", Unit: nilUnit{}}
	}})
	time.Sleep(100 * time.Millisecond)
	if atomic.LoadInt32(&builds) != 1 {
		t.Fatalf("builds %d", atomic.LoadInt32(&builds))
	}
}

type nilUnit struct{}

func (nilUnit) Run(context.Context) error { return nil }
func (nilUnit) Snapshot() Snapshot        { return Snapshot{} }

// ownUnit models a Runner: building it takes ownership of the account, and
// Release gives it up only if it still owns it (registerPubSub semantics).
type ownUnit struct {
	owner  *atomic.Pointer[ownUnit]
	panics bool
}

func newOwnUnit(owner *atomic.Pointer[ownUnit], panics bool) *ownUnit {
	u := &ownUnit{owner: owner, panics: panics}
	owner.Store(u)
	return u
}

func (u *ownUnit) Run(ctx context.Context) error {
	if u.panics {
		panic("boom")
	}
	<-ctx.Done()
	return ctx.Err()
}
func (u *ownUnit) Snapshot() Snapshot { return Snapshot{} }
func (u *ownUnit) Release()           { u.owner.CompareAndSwap(u, nil) }

func TestPool_RestartDoesNotStealOwnershipFromReplacement(t *testing.T) {
	shortRestart(t, time.Millisecond)
	for i := 0; i < 100; i++ {
		var owner atomic.Pointer[ownUnit]
		p := NewPool(nil)
		_ = p.Reload(context.Background(), []Builder{func() Entry {
			return Entry{ID: "a", Unit: newOwnUnit(&owner, true)}
		}})
		time.Sleep(time.Duration(i%5) * 300 * time.Microsecond)
		var repl *ownUnit
		p.ReloadAccount("a", func() Entry {
			repl = newOwnUnit(&owner, false)
			return Entry{ID: "a", Unit: repl}
		})
		time.Sleep(3 * time.Millisecond)
		if owner.Load() != repl {
			p.Stop()
			t.Fatalf("iteration %d: replacement lost ownership", i)
		}
		p.Stop()
	}
}

func TestPool_BuildPanicDuringRestartRetries(t *testing.T) {
	shortRestart(t, 5*time.Millisecond)
	var builds int32
	n := &spyN{}
	p := NewPool(n)
	defer p.Stop()
	ran := make(chan struct{})
	_ = p.Reload(context.Background(), []Builder{func() Entry {
		switch atomic.AddInt32(&builds, 1) {
		case 1:
			return Entry{ID: "a", Unit: &onceUnit{id: "a", panics: true, ran: make(chan struct{})}}
		case 2:
			panic("build boom")
		}
		return Entry{ID: "a", Unit: &onceUnit{id: "a", ran: ran}}
	}})
	select {
	case <-ran:
	case <-time.After(2 * time.Second):
		t.Fatal("unit never rebuilt after build panic")
	}
}

// reloadPanics runs f and reports whether it panicked.
func reloadPanics(f func()) (panicked bool) {
	defer func() { panicked = recover() != nil }()
	f()
	return false
}

func TestPool_BuilderPanicDoesNotWedgeLaterReloads(t *testing.T) {
	p := NewPool(nil)
	defer p.Stop()
	var s int32
	b, _ := blockBuilder("a", &s)
	if err := p.Reload(context.Background(), []Builder{b}); err != nil {
		t.Fatal(err)
	}
	bad := func() Entry { panic("build boom") }
	if !reloadPanics(func() { _ = p.Reload(context.Background(), []Builder{bad}) }) {
		t.Fatal("Reload did not panic")
	}
	if !reloadPanics(func() { p.ReloadAccount("a", bad) }) {
		t.Fatal("ReloadAccount did not panic")
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		b2, _ := blockBuilder("a", &s)
		_ = p.Reload(context.Background(), []Builder{b2})
		b3, _ := blockBuilder("a", &s)
		p.ReloadAccount("a", b3)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("reload wedged after a builder panic")
	}
}

func TestPool_RemovedAccountDropsOrder(t *testing.T) {
	p := NewPool(nil)
	defer p.Stop()
	var s int32
	b, _ := blockBuilder("a", &s)
	_ = p.Reload(context.Background(), []Builder{b})
	p.SetOrder("a", []string{"x"})
	p.ReloadAccount("a", nil)
	if got := p.OrderFunc("a")(); got != nil {
		t.Fatalf("got %v", got)
	}
}
