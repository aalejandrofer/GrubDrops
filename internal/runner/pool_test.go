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
