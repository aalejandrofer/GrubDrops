package runner

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"
)

// panicRestart is how long a panicked unit waits before it is rebuilt.
// A var so tests can shorten it.
var panicRestart = time.Minute

// Unit is one supervised account worker. *Runner satisfies it.
type Unit interface {
	Run(ctx context.Context) error
	Snapshot() Snapshot
}

// Idle is a Unit for an account that cannot run (no login, no games).
type Idle struct {
	AccountID, Platform string
	State               State
}

// Run blocks until ctx is done.
func (i *Idle) Run(ctx context.Context) error {
	<-ctx.Done()
	return ctx.Err()
}

func (i *Idle) Snapshot() Snapshot {
	return Snapshot{AccountID: i.AccountID, Platform: i.Platform, State: i.State}
}

// Entry is one pool member.
type Entry struct {
	ID   string
	Unit Unit
}

// Builder makes a fresh Entry.
type Builder func() Entry

type poolUnit struct {
	pool   *Pool
	build  Builder
	entry  Entry // guarded by pool.mu; replaced when the unit restarts
	cancel context.CancelFunc
	done   chan struct{}
}

// Pool runs one Unit per account and holds the session-only Up next order.
// A unit that panics is rebuilt from its Builder after panicRestart. A unit
// that returns nil (needs user action) stays in the roster and is not restarted.
type Pool struct {
	n Notifier

	reloadMu sync.Mutex // serializes Reload, ReloadAccount and Stop
	// buildMu serializes a restart's check-and-build against Reload and
	// ReloadAccount, which hold it from their builds through cancelling the
	// old units, never while waiting for them to exit.
	buildMu sync.Mutex

	mu    sync.Mutex
	units []*poolUnit // entry order
	// baseCtx is derived from the first Reload's parent and outlives every
	// later parent, so a request-scoped reload context cannot kill the roster.
	baseCtx    context.Context
	baseCancel context.CancelFunc
	orders     map[string][]string
}

func NewPool(n Notifier) *Pool {
	return &Pool{n: n, orders: map[string][]string{}}
}

// releaser is implemented by units that hold backend registrations.
type releaser interface{ Release() }

func (p *Pool) base(parent context.Context) context.Context {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.baseCtx == nil {
		p.baseCtx, p.baseCancel = context.WithCancel(parent)
	}
	return p.baseCtx
}

func (u *poolUnit) current() Entry {
	u.pool.mu.Lock()
	defer u.pool.mu.Unlock()
	return u.entry
}

func (p *Pool) spawn(base context.Context, b Builder, e Entry) *poolUnit {
	ctx, cancel := context.WithCancel(base)
	u := &poolUnit{pool: p, build: b, entry: e, cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(u.done)
		rebuild := false
		for {
			if !rebuild && (!p.supervise(ctx, u.current()) || u.build == nil) {
				return
			}
			// Panicked: wait, then rebuild in the same slot. The unit's own
			// context ends on Stop, Reload and ReloadAccount, so a removed or
			// replaced unit is never resurrected.
			t := time.NewTimer(panicRestart)
			select {
			case <-ctx.Done():
				t.Stop()
				return
			case <-t.C:
			}
			switch p.restart(ctx, u) {
			case restartCancelled:
				return
			case restartPanicked:
				rebuild = true
			default:
				rebuild = false
			}
		}
	}()
	return u
}

type restartResult int

const (
	restartOK restartResult = iota
	restartCancelled
	restartPanicked
)

// restart rebuilds u's entry. It holds buildMu and re-checks ctx, so a
// Reload or ReloadAccount that already built a replacement and cancelled u
// cannot have its PubSub ownership taken by this build.
func (p *Pool) restart(ctx context.Context, u *poolUnit) (res restartResult) {
	p.buildMu.Lock()
	defer p.buildMu.Unlock()
	if ctx.Err() != nil {
		return restartCancelled
	}
	old := u.current()
	defer func() {
		if r := recover(); r != nil {
			res = restartPanicked
			slog.Error("runner build panic", "kind", "error", "account", old.ID, "panic", fmt.Sprint(r))
			p.notifyErr(map[string]any{"account": old.ID, "panic": fmt.Sprint(r)})
		}
	}()
	release(old)
	ne := u.build()
	ne.ID = old.ID
	p.mu.Lock()
	u.entry = ne
	p.mu.Unlock()
	return restartOK
}

// supervise runs e once and reports whether it panicked.
func (p *Pool) supervise(ctx context.Context, e Entry) (panicked bool) {
	defer func() {
		if r := recover(); r != nil {
			panicked = true
			slog.Error("runner panic", "kind", "error", "account", e.ID, "panic", fmt.Sprint(r))
			p.notifyErr(map[string]any{"account": e.ID, "panic": fmt.Sprint(r)})
		}
	}()
	if err := e.Unit.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		slog.Error("runner exited", "kind", "error", "account", e.ID, "err", err)
		p.notifyErr(map[string]any{"account": e.ID, "error": err.Error()})
	}
	return false
}

func (p *Pool) notifyErr(f map[string]any) {
	if p.n != nil {
		_ = p.n.Notify(context.Background(), "error", f)
	}
}

// Reload replaces the whole roster: it builds the new entries, stops every
// running unit, then starts the new ones. Each stopped Runner releases its
// PubSub hooks unless a new Runner already took the account over.
func (p *Pool) Reload(parent context.Context, builders []Builder) error {
	p.reloadMu.Lock()
	defer p.reloadMu.Unlock()
	base := p.base(parent)

	p.buildMu.Lock()
	entries := make([]Entry, 0, len(builders))
	for _, b := range builders {
		entries = append(entries, b())
	}

	// Old entries stay visible in Snapshots until the swap below.
	p.mu.Lock()
	old := slices.Clone(p.units)
	p.mu.Unlock()
	for _, u := range old {
		u.cancel()
	}
	p.buildMu.Unlock()
	for _, u := range old {
		<-u.done
	}
	for _, u := range old {
		release(u.current())
	}

	units := make([]*poolUnit, 0, len(entries))
	for i, e := range entries {
		units = append(units, p.spawn(base, builders[i], e))
	}
	p.mu.Lock()
	p.units = units
	p.mu.Unlock()
	return nil
}

// release unhooks a stopped unit. A Runner only unhooks itself if it still
// owns the account, so a replacement Runner built before the stop stays wired.
func release(e Entry) {
	if r, ok := e.Unit.(releaser); ok {
		r.Release()
	}
}

// ReloadAccount restarts one account from a fresh Builder; others keep
// running. A nil Builder removes the account. Before the first Reload it is a
// no-op.
func (p *Pool) ReloadAccount(id string, b Builder) {
	p.reloadMu.Lock()
	defer p.reloadMu.Unlock()

	p.mu.Lock()
	base := p.baseCtx
	p.mu.Unlock()
	if base == nil {
		return // not running; the next Reload builds the roster
	}

	p.buildMu.Lock()
	var e Entry
	if b != nil {
		e = b()
		e.ID = id
	}

	p.mu.Lock()
	idx := slices.IndexFunc(p.units, func(u *poolUnit) bool { return u.entry.ID == id })
	var old *poolUnit
	if idx >= 0 {
		old = p.units[idx]
	}
	p.mu.Unlock()
	if old != nil {
		old.cancel()
	}
	p.buildMu.Unlock()
	if old != nil {
		<-old.done
		release(old.current())
	}
	if b == nil {
		if old != nil {
			p.mu.Lock()
			p.units = slices.DeleteFunc(p.units, func(u *poolUnit) bool { return u == old })
			p.mu.Unlock()
		}
		return
	}

	nu := p.spawn(base, func() Entry { r := b(); r.ID = id; return r }, e)
	p.mu.Lock()
	if idx >= 0 && idx < len(p.units) && p.units[idx] == old {
		p.units[idx] = nu
	} else {
		p.units = append(p.units, nu)
	}
	p.mu.Unlock()
}

// Stop cancels every unit, waits for all to exit, and drops the base context.
func (p *Pool) Stop() {
	p.reloadMu.Lock()
	defer p.reloadMu.Unlock()
	p.mu.Lock()
	old := p.units
	p.units = nil
	cancel := p.baseCancel
	p.baseCtx, p.baseCancel = nil, nil
	p.mu.Unlock()
	for _, u := range old {
		u.cancel()
	}
	for _, u := range old {
		<-u.done
	}
	if cancel != nil {
		cancel()
	}
}

// Snapshots returns one snapshot per unit, in entry order.
func (p *Pool) Snapshots() []Snapshot {
	p.mu.Lock()
	units := slices.Clone(p.units)
	p.mu.Unlock()
	out := make([]Snapshot, 0, len(units))
	for _, u := range units {
		out = append(out, u.current().Unit.Snapshot())
	}
	return out
}

// SetOrder sets the session-only Up next order for an account.
func (p *Pool) SetOrder(accountID string, benefitIDs []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.orders[accountID] = slices.Clone(benefitIDs)
}

func (p *Pool) ResetOrder(accountID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.orders, accountID)
}

// OrderFunc returns a reader of the account's current order, for Config.Order.
func (p *Pool) OrderFunc(accountID string) func() []string {
	return func() []string {
		p.mu.Lock()
		defer p.mu.Unlock()
		return slices.Clone(p.orders[accountID])
	}
}
