package scheduler

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

// bareRunner only satisfies the runner interface — it carries no idle
// reason, so the scheduler must fall back to "needs_auth".
type bareRunner struct{}

func (bareRunner) Run(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }

// reasonRunner reports a specific idle reason (e.g. an account that is
// authed but has no whitelisted games). The scheduler must surface this
// reason instead of the misleading "needs_auth".
type reasonRunner struct{ reason string }

func (reasonRunner) Run(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }
func (r reasonRunner) IdleState() string           { return r.reason }

// TestSnapshot_IdleReasonSurfaced verifies that an idle (non-watcher)
// entry which reports an IdleState has that state surfaced, while a bare
// idle entry defaults to "needs_auth". This is the fix for the false
// "session expired or never authenticated" banner shown to accounts that
// are actually authed but simply have no games whitelisted.
func TestSnapshot_IdleReasonSurfaced(t *testing.T) {
	s := New(Options{Notifier: silentNotifier{}})
	s.AddEntry(NewEntry("acc-nogames", reasonRunner{reason: "no_games"}))
	s.AddEntry(NewEntry("acc-bare", bareRunner{}))

	byID := map[string]string{}
	for _, st := range s.Snapshot() {
		byID[st.AccountID] = st.State
	}
	assert.Equal(t, "no_games", byID["acc-nogames"],
		"authed-but-no-games account must not be reported as needs_auth")
	assert.Equal(t, "needs_auth", byID["acc-bare"],
		"a bare idle runner with no reason defaults to needs_auth")
}

// TestWatcherSnapshots_IdleReasonSurfaced mirrors the above for the
// dashboard-facing WatcherSnapshots view.
func TestWatcherSnapshots_IdleReasonSurfaced(t *testing.T) {
	s := New(Options{Notifier: silentNotifier{}})
	s.AddEntry(NewEntry("acc-nogames", reasonRunner{reason: "no_games"}))
	s.AddEntry(NewEntry("acc-bare", bareRunner{}))

	byID := map[string]string{}
	for _, snap := range s.WatcherSnapshots() {
		byID[snap.AccountID] = snap.State
	}
	assert.Equal(t, "no_games", byID["acc-nogames"])
	assert.Equal(t, "needs_auth", byID["acc-bare"])
}
