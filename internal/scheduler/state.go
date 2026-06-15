package scheduler

import "github.com/aalejandrofer/grubdrops/internal/watcher"

type AccountState struct {
	AccountID string
	State     string
}

// idleStateReporter is implemented by non-watcher (idle) runners that can
// explain WHY they are idle. Without it, every idle entry collapses to the
// misleading "needs_auth" — an account that is fully authed but simply has
// no whitelisted games would falsely surface as "session expired". An idle
// runner that implements this returns its own state string instead.
type idleStateReporter interface {
	IdleState() string
}

// idleState returns the dashboard state string for a non-watcher runner:
// its self-reported reason if it has one, else the "needs_auth" default.
func idleState(r runner) string {
	if isr, ok := r.(idleStateReporter); ok {
		if s := isr.IdleState(); s != "" {
			return s
		}
	}
	return "needs_auth"
}

func (s *Scheduler) Snapshot() []AccountState {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]AccountState, 0, len(s.entries))
	for _, e := range s.entries {
		w, ok := e.runner.(*watcher.Watcher)
		if !ok {
			out = append(out, AccountState{AccountID: e.id, State: idleState(e.runner)})
			continue
		}
		out = append(out, AccountState{AccountID: e.id, State: w.State().String()})
	}
	return out
}

// WatcherSnapshots returns the dashboard-friendly view of every active
// watcher in the scheduler. Idle (non-watcher) entries are represented with
// State from idleState — their self-reported reason, or "needs_auth" by
// default — and otherwise empty fields.
func (s *Scheduler) WatcherSnapshots() []watcher.Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]watcher.Snapshot, 0, len(s.entries))
	for _, e := range s.entries {
		w, ok := e.runner.(*watcher.Watcher)
		if !ok {
			out = append(out, watcher.Snapshot{AccountID: e.id, State: idleState(e.runner)})
			continue
		}
		out = append(out, w.Snapshot())
	}
	return out
}
