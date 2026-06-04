package scheduler

import (
	"context"
	"sync"
)

// runState tracks an in-flight Start so Stop can cancel + wait.
type runState struct {
	cancel context.CancelFunc
	wg     *sync.WaitGroup
}

func (s *Scheduler) startInternal(parent context.Context) *runState {
	ctx, cancel := context.WithCancel(parent)
	wg := &sync.WaitGroup{}

	s.mu.Lock()
	entries := append([]entry(nil), s.entries...)
	s.mu.Unlock()

	for _, e := range entries {
		wg.Add(1)
		go func(e entry) {
			defer wg.Done()
			s.supervise(ctx, e)
		}(e)
	}
	return &runState{cancel: cancel, wg: wg}
}

// Stop cancels the in-flight run and waits for goroutines to exit.
func (s *Scheduler) Stop(_ context.Context) {
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

// Reload swaps the entry set and restarts.
func (s *Scheduler) Reload(parent context.Context, builders []EntryBuilder) error {
	s.Stop(parent)
	s.mu.Lock()
	s.entries = nil
	s.mu.Unlock()
	for _, b := range builders {
		s.AddEntry(b())
	}
	return s.Start(parent)
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
