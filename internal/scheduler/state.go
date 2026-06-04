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
			out = append(out, AccountState{AccountID: e.id, State: "unknown"})
			continue
		}
		out = append(out, AccountState{AccountID: e.id, State: w.State().String()})
	}
	return out
}
