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
				AccountID:    id,
				Backend:      platformtest.New(),
				Session:      platform.Session{AccessToken: "x"},
				Notifier:     notif,
				TickInterval: 5 * time.Millisecond,
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
