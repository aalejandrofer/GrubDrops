package scheduler

import (
	"context"
	"sync"
)

// runState tracks an in-flight Start. Each entry runs under its OWN child
// context (derived from the run's parent) so a single entry can be
// restarted — ReloadAccount — without disturbing the others. cancel cancels
// the parent (→ every child) for a full Stop.
type runState struct {
	parent context.Context
	cancel context.CancelFunc
	wg     *sync.WaitGroup

	mu    sync.Mutex
	units map[string]*runUnit
}

// runUnit is one running entry: its cancel + a channel closed when the
// supervise goroutine exits (so ReloadAccount can wait for a clean stop
// before respawning).
type runUnit struct {
	cancel context.CancelFunc
	done   chan struct{}
}

func (s *Scheduler) startInternal(parent context.Context) *runState {
	ctx, cancel := context.WithCancel(parent)
	rs := &runState{
		parent: ctx,
		cancel: cancel,
		wg:     &sync.WaitGroup{},
		units:  map[string]*runUnit{},
	}

	s.mu.Lock()
	entries := append([]entry(nil), s.entries...)
	s.mu.Unlock()

	for _, e := range entries {
		rs.startUnit(s, e)
	}
	return rs
}

// startUnit spawns one entry under its own child context + done channel.
func (rs *runState) startUnit(s *Scheduler, e entry) {
	cctx, ccancel := context.WithCancel(rs.parent)
	done := make(chan struct{})
	rs.mu.Lock()
	rs.units[e.id] = &runUnit{cancel: ccancel, done: done}
	rs.mu.Unlock()

	rs.wg.Add(1)
	go func() {
		defer rs.wg.Done()
		defer close(done)
		s.supervise(cctx, e)
	}()
}

// Stop cancels the in-flight run and waits for goroutines to exit. It also
// tears down the long-lived base context so a fully-stopped scheduler starts
// clean: the NEXT Start re-derives baseCtx from its parent. This is the
// process-shutdown / full-stop path. Reload uses stopRun (below) instead so
// it preserves baseCtx across the swap.
func (s *Scheduler) Stop(_ context.Context) {
	s.stopRun()
	s.runMu.Lock()
	if s.baseCancel != nil {
		s.baseCancel()
		s.baseCancel = nil
		s.baseCtx = nil
	}
	s.runMu.Unlock()
}

// stopRun cancels the in-flight run and waits for its goroutines to exit,
// leaving baseCtx intact so a subsequent Start (e.g. from Reload) reuses it.
func (s *Scheduler) stopRun() {
	s.runMu.Lock()
	r := s.current
	s.current = nil
	s.runMu.Unlock()
	if r == nil {
		return
	}
	r.cancel()
	r.wg.Wait()
}

// Reload swaps the entry set and restarts. Serialized so that concurrent
// callers (e.g. double-clicks on the GUI "Apply changes" button) cannot
// interleave their entry sets or race on Start.
func (s *Scheduler) Reload(parent context.Context, builders []EntryBuilder) error {
	s.reloadMu.Lock()
	defer s.reloadMu.Unlock()

	// stopRun (not Stop) so the long-lived base context survives the swap —
	// the rebuilt watchers must keep running under it. parent is only used to
	// seed baseCtx on the very first Start; once baseCtx exists it's ignored
	// for run lifetime, which is what keeps an HTTP-request-context-triggered
	// reload from tearing the roster down when the request finishes.
	s.stopRun()
	s.mu.Lock()
	s.entries = nil
	s.mu.Unlock()
	for _, b := range builders {
		s.AddEntry(b())
	}
	return s.Start(parent)
}

// ReloadAccount restarts a SINGLE account's entry (build a fresh one) while
// every other account keeps running untouched. Used for targeted account
// edits so we don't tear down the whole roster. If the scheduler isn't
// running, it just updates the stored entry set. Serialized via reloadMu.
func (s *Scheduler) ReloadAccount(_ context.Context, id string, build EntryBuilder) {
	s.reloadMu.Lock()
	defer s.reloadMu.Unlock()

	e := build()
	s.replaceEntry(e)

	s.runMu.Lock()
	rs := s.current
	s.runMu.Unlock()
	if rs == nil {
		return // not running; entry set updated, next Start picks it up
	}

	// Stop the old unit (if any) and wait for it to exit, then spawn fresh.
	rs.mu.Lock()
	u := rs.units[id]
	delete(rs.units, id)
	rs.mu.Unlock()
	if u != nil {
		u.cancel()
		<-u.done
	}
	rs.startUnit(s, e)
}

// replaceEntry swaps the stored entry with matching id, or appends it.
func (s *Scheduler) replaceEntry(e entry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.entries {
		if s.entries[i].id == e.id {
			s.entries[i] = e
			return
		}
	}
	s.entries = append(s.entries, e)
}

// EntryBuilder produces a fresh Entry on demand.
type EntryBuilder func() Entry

// Entry is exposed as an alias so external packages can hand entries to
// the scheduler via NewEntry without touching the unexported field.
type Entry = entry

// NewEntry builds an Entry from an id and a runner-compatible value (anything
// with Run(ctx) error). *watcher.Watcher satisfies this.
func NewEntry(id string, r runner) Entry { return Entry{id: id, runner: r} }

// AddEntry registers a pre-built entry (used by Reload).
func (s *Scheduler) AddEntry(e Entry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = append(s.entries, e)
}
