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

// snapshotter is any runner that can describe its current work. Both the v1
// *watcher.Watcher and the pipeline v2 loop implement it.
type snapshotter interface {
	Snapshot() watcher.Snapshot
}

func (s *Scheduler) Snapshot() []AccountState {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]AccountState, 0, len(s.entries))
	for _, e := range s.entries {
		if sn, ok := e.runner.(snapshotter); ok {
			out = append(out, AccountState{AccountID: e.id, State: sn.Snapshot().State})
			continue
		}
		out = append(out, AccountState{AccountID: e.id, State: idleState(e.runner)})
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
		if sn, ok := e.runner.(snapshotter); ok {
			out = append(out, sn.Snapshot())
			continue
		}
		out = append(out, watcher.Snapshot{AccountID: e.id, State: idleState(e.runner)})
	}
	return out
}
