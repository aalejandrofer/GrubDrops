package scheduler

import "github.com/aalejandrofer/rust-drops-miner/internal/watcher"

type AccountState struct {
	AccountID string
	State     string
}

func (s *Scheduler) Snapshot() []AccountState {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]AccountState, 0, len(s.entries))
	for _, e := range s.entries {
		w, ok := e.runner.(*watcher.Watcher)
		if !ok {
			out = append(out, AccountState{AccountID: e.id, State: "needs_auth"})
			continue
		}
		out = append(out, AccountState{AccountID: e.id, State: w.State().String()})
	}
	return out
}

// WatcherSnapshots returns the dashboard-friendly view of every
// active watcher in the scheduler. nopRunner-backed entries are
// represented with State="needs_auth" and otherwise empty fields.
func (s *Scheduler) WatcherSnapshots() []watcher.Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]watcher.Snapshot, 0, len(s.entries))
	for _, e := range s.entries {
		w, ok := e.runner.(*watcher.Watcher)
		if !ok {
			out = append(out, watcher.Snapshot{AccountID: e.id, State: "needs_auth"})
			continue
		}
		out = append(out, w.Snapshot())
	}
	return out
}
