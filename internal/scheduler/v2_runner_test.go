package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/watcher"
)

type v2Runner struct{}

func (v2Runner) Run(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }
func (v2Runner) Snapshot() watcher.Snapshot {
	return watcher.Snapshot{AccountID: "acc", State: "watching", Channel: "ch1"}
}
func (v2Runner) LastDiscovery() ([]platform.Campaign, time.Time) {
	return []platform.Campaign{{ID: "c1"}}, time.Unix(1, 0)
}
func (v2Runner) AllowGame() func(string) bool { return func(string) bool { return true } }

func TestScheduler_NonWatcherRunnerWithSnapshot(t *testing.T) {
	s := New(Options{})
	s.AddEntry(NewEntry("acc", v2Runner{}))
	snaps := s.WatcherSnapshots()
	assert.Equal(t, "ch1", snaps[0].Channel)
	assert.Equal(t, "watching", s.Snapshot()[0].State, "must not fall back to needs_auth")
	disc := s.WatcherDiscoveries()
	assert.Len(t, disc[0].Campaigns, 1)
}
