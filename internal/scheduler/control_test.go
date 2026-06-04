package scheduler

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/rust-drops-miner/internal/platform"
	"github.com/aalejandrofer/rust-drops-miner/internal/platform/fake"
	"github.com/aalejandrofer/rust-drops-miner/internal/watcher"
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
				Backend:      fake.New(fake.WithFastTime()),
				Session:      platform.Session{AccessToken: "x"},
				Notifier:     notif,
				TickInterval: 5 * time.Millisecond,
			})
			return NewEntry(id, w)
		}
	}

	s.AddEntry(mkBuilder("acc1")())
	require.NoError(t, s.Start(ctx))
	s.Wait()
	assert.Equal(t, int64(1), notif.claims.Load())

	require.NoError(t, s.Reload(ctx, []EntryBuilder{mkBuilder("acc2"), mkBuilder("acc3")}))
	s.Wait()
	assert.Equal(t, int64(3), notif.claims.Load())
}
