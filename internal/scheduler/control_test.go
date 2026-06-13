package scheduler

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/platform/platformtest"
	"github.com/aalejandrofer/grubdrops/internal/watcher"
)

type counterNotifier struct{ claims atomic.Int64 }

func (c *counterNotifier) Notify(_ context.Context, ev string, _ map[string]any) error {
	if ev == "claim" {
		c.claims.Add(1)
	}
	return nil
}

func TestScheduler_StopThenReloadAddsAccount(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	notif := &counterNotifier{}
	s := New(Options{Notifier: notif})

	mkBuilder := func(id string) EntryBuilder {
		return func() Entry {
			w := watcher.New(watcher.Config{
				AccountID:         id,
				Backend:           platformtest.New(),
				Session:           platform.Session{AccessToken: "x"},
				Notifier:          notif,
				TickInterval:      5 * time.Millisecond,
				HeartbeatInterval: 5 * time.Millisecond,
			})
			return NewEntry(id, w)
		}
	}

	// Watchers no longer self-terminate after claiming (sleeping re-arms on
	// recheckInterval), so s.Wait() would block until ctx cancel. Poll the
	// cumulative claim count instead. The platformtest backend has one
	// claimable benefit per account, so each account contributes exactly
	// one claim and the count is stable once reached.
	s.AddEntry(mkBuilder("acc1")())
	require.NoError(t, s.Start(ctx))
	require.Eventually(t, func() bool {
		return notif.claims.Load() == 1
	}, 4*time.Second, 5*time.Millisecond, "acc1 should claim its benefit")

	require.NoError(t, s.Reload(ctx, []EntryBuilder{mkBuilder("acc2"), mkBuilder("acc3")}))
	require.Eventually(t, func() bool {
		return notif.claims.Load() == 3
	}, 4*time.Second, 5*time.Millisecond, "acc2 + acc3 should each claim after reload")

	// Targeted reload: restart only acc2 (fresh backend → claims its benefit
	// again, +1) while acc3 keeps running untouched.
	s.ReloadAccount(ctx, "acc2", mkBuilder("acc2"))
	require.Eventually(t, func() bool {
		return notif.claims.Load() == 4
	}, 4*time.Second, 5*time.Millisecond, "reloaded acc2 should re-claim; acc3 untouched")

	s.Stop(ctx)
}

// TestScheduler_ReloadAccountLeavesOthersRunning asserts the core per-account
// reload guarantee: restarting ONE account's watcher must not cancel or
// disturb any other account's watcher. The "other" runner keeps ticking
// continuously across the reload (its context is never cancelled), while the
// target runner is observably torn down and respun.
func TestScheduler_ReloadAccountLeavesOthersRunning(t *testing.T) {
	root, rootCancel := context.WithCancel(context.Background())
	defer rootCancel()

	s := New(Options{Notifier: &counterNotifier{}})

	target := &liveRunner{}
	other := &liveRunner{}
	buildTarget := func() Entry { return NewEntry("target", target) }
	buildOther := func() Entry { return NewEntry("other", other) }

	require.NoError(t, s.Reload(root, []EntryBuilder{buildTarget, buildOther}))
	require.Eventually(t, func() bool {
		return target.ticks.Load() > 0 && other.ticks.Load() > 0
	}, time.Second, 2*time.Millisecond, "both runners should start")

	// Replace the target's runner with a brand-new instance and reload only it.
	fresh := &liveRunner{}
	otherTicksBefore := other.ticks.Load()
	s.ReloadAccount(root, "target", func() Entry { return NewEntry("target", fresh) })

	// The fresh runner for the target must start ticking (proving the old one
	// was stopped and a new entry was spun up under the base context)…
	require.Eventually(t, func() bool { return fresh.ticks.Load() > 0 },
		time.Second, 2*time.Millisecond, "reloaded target should restart under the base context")

	// …and the OTHER runner must keep ticking the whole time — its context was
	// never cancelled by the targeted reload. Poll rather than read once: the
	// other runner ticks on its own cadence, so an instantaneous compare right
	// after the reload races its tick interval (flaky "1 is not greater than 1").
	require.Eventually(t, func() bool { return other.ticks.Load() > otherTicksBefore },
		time.Second, 2*time.Millisecond,
		"other account's watcher must keep running uninterrupted across a per-account reload")
	require.True(t, other.running(40*time.Millisecond),
		"other account's watcher must still be live after the per-account reload")

	s.Stop(root)
}

// TestScheduler_ReloadAccountSurvivesTriggerContextCancel mirrors the global
// reload-ctx test for the per-account path: a per-account reload triggered by
// a short-lived (e.g. HTTP request) context must keep running after that
// trigger context is cancelled, because the watcher is rooted in the long-lived
// base context — never the caller's parent.
func TestScheduler_ReloadAccountSurvivesTriggerContextCancel(t *testing.T) {
	root, rootCancel := context.WithCancel(context.Background())
	defer rootCancel()

	s := New(Options{Notifier: &counterNotifier{}})

	r := &liveRunner{}
	build := func() Entry { return NewEntry("acc1", r) }

	require.NoError(t, s.Reload(root, []EntryBuilder{build}))
	require.Eventually(t, func() bool { return r.ticks.Load() > 0 },
		time.Second, 2*time.Millisecond, "runner should start")

	// Per-account reload under a request-scoped context that we then cancel.
	fresh := &liveRunner{}
	triggerCtx, triggerCancel := context.WithCancel(root)
	s.ReloadAccount(triggerCtx, "acc1", func() Entry { return NewEntry("acc1", fresh) })
	require.Eventually(t, func() bool { return fresh.ticks.Load() > 0 },
		time.Second, 2*time.Millisecond, "reloaded runner should start")
	ticksAfterReload := fresh.ticks.Load()
	triggerCancel() // request finished → its context dies

	time.Sleep(80 * time.Millisecond)
	require.True(t, fresh.running(40*time.Millisecond),
		"per-account-reloaded watcher must survive its trigger context being cancelled")
	require.Greater(t, fresh.ticks.Load(), ticksAfterReload,
		"per-account-reloaded watcher must keep progressing after the trigger context dies")

	rootCancel()
	require.Eventually(t, func() bool { return !fresh.running(20 * time.Millisecond) },
		time.Second, 2*time.Millisecond, "watcher should stop when the root context is cancelled")
}
