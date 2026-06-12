package api

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/platform/platformtest"
	"github.com/aalejandrofer/grubdrops/internal/scheduler"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
	"github.com/aalejandrofer/grubdrops/internal/watcher"
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

// stubCompletedSource is a deterministic completedDataSource for the
// "X / Y collected" tile tests. Link + claim rows are seeded per
// campaign id at construction time.
type stubCompletedSource struct {
	links  map[string][]gen.ListAccountLinksForCampaignRow
	claims map[string][]gen.ListClaimsForCampaignRow
}

func (s stubCompletedSource) ListAccountLinksForCampaign(_ context.Context, id string) ([]gen.ListAccountLinksForCampaignRow, error) {
	return s.links[id], nil
}

func (s stubCompletedSource) ListClaimsForCampaign(_ context.Context, id string) ([]gen.ListClaimsForCampaignRow, error) {
	return s.claims[id], nil
}

// TestCompletedByAllConnected covers the COMPLETED tile semantics: a drop
// counts only when EVERY account connected/linked to its campaign has a
// claim for that benefit. Total is every discovered drop regardless of
// claims.
//
//	camp-1: connected = {a, b}; benefits b1, b2, b3
//	  b1 claimed by a + b  -> completed
//	  b2 claimed by a only -> NOT completed (b missing)
//	  b3 claimed by nobody -> NOT completed
//	camp-2 (no link rows): falls back to EligibleAccounts = {a};
//	  benefit b4 claimed by a -> completed
//
// total = 4 drops, done = 2 (b1 + b4).
func TestCompletedByAllConnected(t *testing.T) {
	snap := []scheduler.DiscoveredCampaign{
		{
			Campaign: platform.Campaign{
				ID: "camp-1",
				Benefits: []platform.DropBenefit{
					{ID: "b1"}, {ID: "b2"}, {ID: "b3"},
				},
			},
			EligibleAccounts: []string{"a", "b"},
		},
		{
			Campaign: platform.Campaign{
				ID:       "camp-2",
				Benefits: []platform.DropBenefit{{ID: "b4"}},
			},
			EligibleAccounts: []string{"a"},
		},
	}
	src := stubCompletedSource{
		links: map[string][]gen.ListAccountLinksForCampaignRow{
			"camp-1": {
				{AccountID: "a", Linked: 1},
				{AccountID: "b", Linked: 1},
			},
			// camp-2 deliberately has no link rows -> EligibleAccounts fallback.
		},
		claims: map[string][]gen.ListClaimsForCampaignRow{
			"camp-1": {
				{BenefitID: "b1", AccountID: "a"},
				{BenefitID: "b1", AccountID: "b"},
				{BenefitID: "b2", AccountID: "a"},
			},
			"camp-2": {
				{BenefitID: "b4", AccountID: "a"},
			},
		},
	}

	done, total := completedByAllConnected(context.Background(), snap, src)
	assert.Equal(t, 4, total, "total = every discovered drop")
	assert.Equal(t, 2, done, "only b1 (all connected) + b4 (fallback) are complete")
}

// TestCompletedByAllConnected_PartialLinkExcludesUnlinked verifies that an
// account with linked=0 is NOT part of the connected set: a benefit all
// linked accounts collected counts as complete even if an unlinked account
// hasn't.
func TestCompletedByAllConnected_PartialLinkExcludesUnlinked(t *testing.T) {
	snap := []scheduler.DiscoveredCampaign{
		{
			Campaign: platform.Campaign{
				ID:       "camp-1",
				Benefits: []platform.DropBenefit{{ID: "b1"}},
			},
			EligibleAccounts: []string{"a", "b"},
		},
	}
	src := stubCompletedSource{
		links: map[string][]gen.ListAccountLinksForCampaignRow{
			"camp-1": {
				{AccountID: "a", Linked: 1},
				{AccountID: "b", Linked: 0}, // not connected -> ignored
			},
		},
		claims: map[string][]gen.ListClaimsForCampaignRow{
			"camp-1": {{BenefitID: "b1", AccountID: "a"}},
		},
	}
	done, total := completedByAllConnected(context.Background(), snap, src)
	assert.Equal(t, 1, total)
	assert.Equal(t, 1, done, "b1 is complete: the only connected account (a) collected it")
}

// TestCompletedByAllConnected_NilQueriesCountsTotalOnly ensures the tile
// stays safe with no store handle: total still reflects discovered drops,
// done is zero.
func TestCompletedByAllConnected_NilQueriesCountsTotalOnly(t *testing.T) {
	snap := []scheduler.DiscoveredCampaign{
		{Campaign: platform.Campaign{ID: "c", Benefits: []platform.DropBenefit{{ID: "x"}, {ID: "y"}}}},
	}
	done, total := completedByAllConnected(context.Background(), snap, nil)
	assert.Equal(t, 2, total)
	assert.Equal(t, 0, done)
}
