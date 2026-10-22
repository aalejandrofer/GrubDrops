package watcher

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/platform/platformtest"
	"github.com/stretchr/testify/assert"
)

// campaignCountingBackend offers a live whitelisted campaign and counts how
// often the watcher goes looking for drops.
type campaignCountingBackend struct {
	*platformtest.MockBackend
	lists atomic.Int64
}

func (b *campaignCountingBackend) ListActiveCampaigns(context.Context, platform.Session) ([]platform.Campaign, error) {
	b.lists.Add(1)
	return []platform.Campaign{{ID: "c", Game: "Game", Status: "active", AccountLinked: true,
		Benefits: []platform.DropBenefit{{ID: "b", CampaignID: "c", RequiredMinutes: 60}}}}, nil
}

func runPointsOnly(t *testing.T, force ForceWatchSource) (*campaignCountingBackend, int64) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	backend := &campaignCountingBackend{MockBackend: platformtest.New()}
	w := New(Config{
		AccountID: "acc1", Backend: backend, Session: platform.Session{AccessToken: "tok"},
		Notifier: &recordingNotifier{}, TickInterval: 5 * time.Millisecond,
		AllowGame:  func(string) bool { return true }, // a drop IS mineable
		PointsOnly: true, ForceWatcher: force,
	})
	_ = w.Run(ctx)
	return backend, backend.Heartbeats()
}

// Points-only never looks for or mines drops, even with a whitelisted live
// campaign, and watches the selected channel instead.
func TestPointsOnly_SkipsDropsAndWatchesSelectedChannel(t *testing.T) {
	b, beats := runPointsOnly(t, fakeForce{channel: "xqc"})
	assert.Zero(t, b.lists.Load(), "must not run drop discovery")
	assert.Greater(t, beats, int64(0), "watches the selected points channel")
}

// With no channel selected a points-only account watches nothing at all.
func TestPointsOnly_NothingSelectedWatchesNothing(t *testing.T) {
	b, beats := runPointsOnly(t, fakeForce{channel: ""})
	assert.Zero(t, b.lists.Load())
	assert.Zero(t, beats)
}
