package api

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/dropsminer/internal/platform"
	"github.com/aalejandrofer/dropsminer/internal/platform/platformtest"
	"github.com/aalejandrofer/dropsminer/internal/scheduler"
	"github.com/aalejandrofer/dropsminer/internal/watcher"
)

// stubChannelCounter is a deterministic ChannelCounter for unit tests.
// It returns the per-id count seeded at construction time; unknown ids
// return zero so it matches the production fallback behaviour.
type stubChannelCounter struct {
	counts map[string]int
}

func (s stubChannelCounter) AllowedChannelCount(campaignID string) int {
	return s.counts[campaignID]
}

// stubClaimedCounter is a minimal claimedCounter implementation. It
// returns the seeded count for a known campaign and surfaces an error
// for the special id "boom" so we can assert the handler treats errors
// as a zero count (instead of panicking or short-circuiting the row).
type stubClaimedCounter struct {
	counts map[string]int64
}

func (s stubClaimedCounter) CountClaimedForCampaign(_ context.Context, campaignID string) (int64, error) {
	if campaignID == "boom" {
		return 0, errors.New("simulated db error")
	}
	return s.counts[campaignID], nil
}

// silentNotifier is the same no-op notifier the scheduler tests use,
// re-declared locally so this file doesn't depend on the scheduler
// package's unexported test helpers.
type silentNotifier struct{}

func (silentNotifier) Notify(_ context.Context, _ string, _ map[string]any) error { return nil }

// TestActiveCampsFromDiscovery_PopulatesChannelsAndClaimed exercises
// the dashboard projection end-to-end against the platformtest mock
// backend. The mock surfaces a single campaign ("camp1"); the stubs
// pretend 4 channels are eligible and 2 benefits have been claimed.
// We expect those values to land verbatim on the dashCampaign row,
// and Total to match len(Benefits) (the mock ships exactly one).
func TestActiveCampsFromDiscovery_PopulatesChannelsAndClaimed(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sch := scheduler.New(scheduler.Options{Notifier: silentNotifier{}})
	w := watcher.New(watcher.Config{
		AccountID:    "acc-a",
		Backend:      platformtest.New(),
		Session:      platform.Session{AccessToken: "x"},
		Notifier:     silentNotifier{},
		TickInterval: 5 * time.Millisecond,
		AllowGame:    func(g string) bool { return g == "Mock" },
	})
	sch.AddEntry(scheduler.NewEntry("acc-a", w))

	require.NoError(t, sch.Start(ctx))
	sch.Wait()

	// MockBackend.ListActiveCampaigns leaves Platform empty — key the
	// channel-counter map by "" so it matches what discovery surfaces.
	counters := map[string]ChannelCounter{
		"": stubChannelCounter{counts: map[string]int{"camp1": 4}},
	}
	claimed := stubClaimedCounter{counts: map[string]int64{"camp1": 2}}

	rows := activeCampsFromDiscovery(ctx, sch, counters, claimed)
	require.Len(t, rows, 1, "expected the single mock campaign in the snapshot")
	row := rows[0]
	assert.Equal(t, "camp1", row.ID)
	assert.Equal(t, 4, row.Channels, "Channels must come from the platform channel counter")
	assert.Equal(t, 2, row.Claimed, "Claimed must come from the queries stub")
	assert.Equal(t, 1, row.Total, "Total mirrors len(Benefits) — mock ships one drop")
}

// TestActiveCampsFromDiscovery_NilDepsFallBackToZero verifies the
// handler stays safe when the registry didn't surface a channel
// counter for the campaign's platform AND when the queries handle is
// nil (e.g. tests that don't bother wiring a database). Both columns
// should render as zero rather than panic.
func TestActiveCampsFromDiscovery_NilDepsFallBackToZero(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sch := scheduler.New(scheduler.Options{Notifier: silentNotifier{}})
	w := watcher.New(watcher.Config{
		AccountID:    "acc-a",
		Backend:      platformtest.New(),
		Session:      platform.Session{AccessToken: "x"},
		Notifier:     silentNotifier{},
		TickInterval: 5 * time.Millisecond,
		AllowGame:    func(g string) bool { return g == "Mock" },
	})
	sch.AddEntry(scheduler.NewEntry("acc-a", w))

	require.NoError(t, sch.Start(ctx))
	sch.Wait()

	rows := activeCampsFromDiscovery(ctx, sch, nil, nil)
	require.Len(t, rows, 1)
	assert.Equal(t, 0, rows[0].Channels)
	assert.Equal(t, 0, rows[0].Claimed)
}

// TestActiveCampsFromDiscovery_NilScheduler returns an empty slice
// rather than panicking. The dashboard renders fine with nil for the
// ActiveCamps field, so an early return preserves that contract.
func TestActiveCampsFromDiscovery_NilScheduler(t *testing.T) {
	assert.Nil(t, activeCampsFromDiscovery(context.Background(), nil, nil, nil))
}
