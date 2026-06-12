package scheduler

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/platform/platformtest"
	"github.com/aalejandrofer/grubdrops/internal/watcher"
)

type captureNotifier struct{ claims atomic.Int64 }

func (c *captureNotifier) Notify(_ context.Context, ev string, _ map[string]any) error {
	if ev == "claim" {
		c.claims.Add(1)
	}
	return nil
}

func TestScheduler_RunsMultipleAccountsConcurrently(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	notif := &captureNotifier{}
	s := New(Options{Notifier: notif})

	for i := 0; i < 3; i++ {
		backend := platformtest.New()
		sess := platform.Session{AccessToken: "x"}
		w := watcher.New(watcher.Config{
			AccountID:         fmt.Sprintf("acc%d", i),
			Backend:           backend,
			Session:           sess,
			Notifier:          notif,
			TickInterval:      5 * time.Millisecond,
			HeartbeatInterval: 5 * time.Millisecond,
		})
		s.Add(fmt.Sprintf("acc%d", i), w)
	}

	require.NoError(t, s.Start(ctx))
	s.Wait()

	assert.Equal(t, int64(3), notif.claims.Load())
}
