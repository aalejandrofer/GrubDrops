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

// liveRunner is a runner that stays alive (incrementing a tick counter on a
// fast loop) until its OWN context is cancelled. It records the last time it
// ticked so a test can assert "still running well after a trigger context was
// cancelled".
type liveRunner struct {
	ticks  atomic.Int64
	lastNs atomic.Int64 // UnixNano of the most recent tick
}

func (r *liveRunner) Run(ctx context.Context) error {
	t := time.NewTicker(2 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			r.ticks.Add(1)
			r.lastNs.Store(time.Now().UnixNano())
		}
	}
}

func (r *liveRunner) running(within time.Duration) bool {
	last := r.lastNs.Load()
	if last == 0 {
		return false
	}
	return time.Since(time.Unix(0, last)) < within
}

// TestScheduler_ReloadSurvivesTriggerContextCancel reproduces the v1.0.1
// prod stall: the Kick re-login handler called Reload with the HTTP REQUEST
// context, so when the request finished and that context was cancelled, every
// freshly-rebuilt watcher's context was cancelled with it and the whole roster
// tore down (the Twitch handler avoided this by reloading with the long-lived
// root context). A fresh process start was fine because boot reloaded under
// the root context; only reload-after-login broke. The fix makes the scheduler
// run watchers under a long-lived base context captured on first start, so a
// transient trigger context can never tear the roster down.
//
// FAILS before the fix: cancelling triggerCtx kills the runner.
func TestScheduler_ReloadSurvivesTriggerContextCancel(t *testing.T) {
	root, rootCancel := context.WithCancel(context.Background())
	defer rootCancel()

	s := New(Options{Notifier: &counterNotifier{}})

	r := &liveRunner{}
	build := func() Entry { return NewEntry("acc1", r) }

	// First start under the root context (mimics boot loadAndStart(ctx)).
	require.NoError(t, s.Reload(root, []EntryBuilder{build}))
	require.Eventually(t, func() bool { return r.ticks.Load() > 0 },
		time.Second, 2*time.Millisecond, "runner should start on first reload")

	// Now a Kick re-login arrives. The handler reloads with the REQUEST
	// context, then the request finishes and that context is cancelled.
	triggerCtx, triggerCancel := context.WithCancel(root)
	require.NoError(t, s.Reload(triggerCtx, []EntryBuilder{build}))
	ticksAfterReload := r.ticks.Load()
	triggerCancel() // request completed → its context dies

	// Give cancellation time to propagate, then assert the runner is STILL
	// ticking (it must be rooted in the long-lived base context, not the
	// dead request context).
	time.Sleep(80 * time.Millisecond)
	require.True(t, r.running(40*time.Millisecond),
		"watcher must keep running after the reload trigger context is cancelled")
	require.Greater(t, r.ticks.Load(), ticksAfterReload,
		"watcher must keep making progress after the trigger context dies")

	// Sanity: cancelling the true root DOES stop it.
	rootCancel()
	require.Eventually(t, func() bool { return !r.running(20 * time.Millisecond) },
		time.Second, 2*time.Millisecond, "watcher should stop when the root context is cancelled")
}

// TestScheduler_RealWatcherSurvivesTriggerCancel is the same scenario with a
// real watcher.Watcher + mock backend, asserting it keeps reaching the
// WATCHING state (heartbeats keep firing) after a trigger-context cancel —
// exactly the "watchers never resume watching" symptom from the incident.
func TestScheduler_RealWatcherSurvivesTriggerCancel(t *testing.T) {
	root, rootCancel := context.WithCancel(context.Background())
	defer rootCancel()

	notif := &counterNotifier{}
	s := New(Options{Notifier: notif})

	backend := platformtest.New()
	build := func() Entry {
		w := watcher.New(watcher.Config{
			AccountID:         "acc1",
			Backend:           backend,
			Session:           platform.Session{AccessToken: "x"},
			Notifier:          notif,
			TickInterval:      5 * time.Millisecond,
			HeartbeatInterval: 5 * time.Millisecond,
		})
		return NewEntry("acc1", w)
	}

	require.NoError(t, s.Reload(root, []EntryBuilder{build}))

	triggerCtx, triggerCancel := context.WithCancel(root)
	require.NoError(t, s.Reload(triggerCtx, []EntryBuilder{build}))
	triggerCancel()

	// After the trigger context dies, the watcher (a fresh backend) must
	// still drive a watch to completion and claim. Before the fix the
	// watcher's context is dead, so it never heartbeats and never claims.
	hbBefore := backend.Heartbeats()
	require.Eventually(t, func() bool {
		return backend.Heartbeats() > hbBefore
	}, 2*time.Second, 5*time.Millisecond,
		"watcher must keep heartbeating after the reload trigger context is cancelled")

	s.Stop(root)
}
