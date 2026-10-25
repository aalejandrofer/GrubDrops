package runner

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

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
	entry  Entry
	cancel context.CancelFunc
	done   chan struct{}
}

// Pool runs one Unit per account and holds the session-only Up next order.
// A unit that panics or returns nil (needs user action) stays in the roster
// and is not restarted, as in 1.x supervise; plan 3 decides restart policy.
type Pool struct {
	n Notifier

	reloadMu sync.Mutex // serializes Reload, ReloadAccount and Stop

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

func (p *Pool) spawn(base context.Context, e Entry) *poolUnit {
	ctx, cancel := context.WithCancel(base)
	u := &poolUnit{entry: e, cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(u.done)
		p.supervise(ctx, e)
	}()
	return u
}

func (p *Pool) supervise(ctx context.Context, e Entry) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("runner panic", "kind", "error", "account", e.ID, "panic", fmt.Sprint(r))
			p.notifyErr(map[string]any{"account": e.ID, "panic": fmt.Sprint(r)})
		}
	}()
	if err := e.Unit.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		slog.Error("runner exited", "kind", "error", "account", e.ID, "err", err)
		p.notifyErr(map[string]any{"account": e.ID, "error": err.Error()})
	}
}

func (p *Pool) notifyErr(f map[string]any) {
	if p.n != nil {
		_ = p.n.Notify(context.Background(), "error", f)
	}
}

func stop(u *poolUnit) {
	u.cancel()
	<-u.done
}

// Reload replaces the whole roster: it builds the new entries, stops every
// running unit, then starts the new ones. Accounts absent from the new set
// release their backend registrations.
func (p *Pool) Reload(parent context.Context, builders []Builder) error {
	p.reloadMu.Lock()
	defer p.reloadMu.Unlock()
	base := p.base(parent)

	entries := make([]Entry, 0, len(builders))
	keep := map[string]bool{}
	for _, b := range builders {
		e := b()
		entries = append(entries, e)
		keep[e.ID] = true
	}

	// Old entries stay visible in Snapshots until the swap below.
	p.mu.Lock()
	old := slices.Clone(p.units)
	p.mu.Unlock()
	for _, u := range old {
		u.cancel()
	}
	for _, u := range old {
		<-u.done
	}
	next := make(map[string]Unit, len(entries))
	for _, e := range entries {
		next[e.ID] = e.Unit
	}
	for _, u := range old {
		releaseUnlessReplaced(u.entry, next[u.entry.ID])
	}

	units := make([]*poolUnit, 0, len(entries))
	for _, e := range entries {
		units = append(units, p.spawn(base, e))
	}
	p.mu.Lock()
	p.units = units
	p.mu.Unlock()
	return nil
}

func release(e Entry) {
	if r, ok := e.Unit.(releaser); ok {
		r.Release()
	}
}

// releaseUnlessReplaced clears the old unit's backend registrations unless the
// new unit is itself a releaser: a new Runner re-registered its hooks in New,
// and releasing would wipe them. A non-Runner replacement (Idle) or no
// replacement (nil) leaves the old closures stale, so they are cleared.
func releaseUnlessReplaced(old Entry, repl Unit) {
	if _, ok := repl.(releaser); ok {
		return
	}
	release(old)
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
		stop(old)
	}

	if old != nil {
		releaseUnlessReplaced(old.entry, e.Unit)
	}
	if b == nil {
		if old != nil {
			p.mu.Lock()
			p.units = slices.DeleteFunc(p.units, func(u *poolUnit) bool { return u == old })
			p.mu.Unlock()
		}
		return
	}

	nu := p.spawn(base, e)
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
		out = append(out, u.entry.Unit.Snapshot())
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

// Release clears this account's PubSub hooks so a removed runner leaves no
// stale closure in the backend. Pool calls it when the account is removed.
func (r *Runner) Release() {
	if pa, ok := r.cfg.Backend.(platform.PubSubAware); ok {
		pa.SetAccountPubSubHooks(r.cfg.AccountID, platform.PubSubHooks{})
	}
}
