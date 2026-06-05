package watcher

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/rust-drops-miner/internal/platform"
	"github.com/aalejandrofer/rust-drops-miner/internal/platform/platformtest"
)

type recordingNotifier struct{ events []string }

func (r *recordingNotifier) Notify(_ context.Context, ev string, _ map[string]any) error {
	r.events = append(r.events, ev)
	return nil
}

func TestWatcher_MinesUntilClaim(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	backend := platformtest.New()
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

// New() must plumb AllowGame into Session.GameFilter so backends can
// short-circuit non-whitelisted games at the source. Regression guard:
// if someone removes the plumbing the whitelist degrades to a
// watcher-only filter and backends waste bandwidth.
func TestWatcher_New_PropagatesAllowGameToSession(t *testing.T) {
	allow := func(g string) bool { return g == "Rust" }
	w := New(Config{
		AccountID: "acc1",
		Backend:   platformtest.New(),
		Session:   platform.Session{AccessToken: "tok"},
		Notifier:  &recordingNotifier{},
		AllowGame: allow,
	})
	require.NotNil(t, w.cfg.Session.GameFilter)
	assert.True(t, w.cfg.Session.GameFilter("Rust"))
	assert.False(t, w.cfg.Session.GameFilter("Fortnite"))
	assert.Equal(t, "acc1", w.cfg.Session.AccountID)
}
