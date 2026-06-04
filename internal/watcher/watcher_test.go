package watcher

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chano-fernandez/rust-drops-miner/internal/platform"
	"github.com/chano-fernandez/rust-drops-miner/internal/platform/fake"
)

type recordingNotifier struct{ events []string }

func (r *recordingNotifier) Notify(_ context.Context, ev string, _ map[string]any) error {
	r.events = append(r.events, ev)
	return nil
}

func TestWatcher_MinesUntilClaim(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	backend := fake.New(fake.WithFastTime())
	notif := &recordingNotifier{}

	sess, err := backend.PollDeviceLogin(ctx, platform.DeviceChallenge{})
	require.NoError(t, err)

	w := New(Config{
		AccountID:    "acc1",
		Backend:      backend,
		Session:      sess,
		Notifier:     notif,
		TickInterval: 5 * time.Millisecond,
	})

	err = w.Run(ctx)
	require.NoError(t, err)

	assert.Contains(t, notif.events, "claim")
	// After claiming the only benefit, pickCampaign finds nothing unclaimed
	// and transitions to StateSleeping before Run returns.
	assert.Equal(t, StateSleeping, w.State())
}
