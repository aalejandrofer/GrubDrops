package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/platform/platformtest"
	"github.com/aalejandrofer/grubdrops/internal/watcher"
)

type silentNotifier struct{}

func (silentNotifier) Notify(_ context.Context, _ string, _ map[string]any) error { return nil }

// TestDiscoverySnapshot_UnionAndDedup verifies that DiscoverySnapshot
// (a) walks every watcher's cached ListActiveCampaigns result,
// (b) deduplicates by campaign.ID across accounts, and
// (c) applies the whitelist union — a campaign is included iff at
//
//	least one account has its game whitelisted.
//
// Note: the watcher only caches whitelisted campaigns (it filters out
// non-whitelisted entries before storing them, so the persister never
// touches non-opted-in rows). That means accounts whose whitelist
// rejects everything contribute no rows to the snapshot at all.
func TestDiscoverySnapshot_UnionAndDedup(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	s := New(Options{Notifier: silentNotifier{}})

	// Two accounts, both whitelisting "Mock" — they share the same
	// single campaign returned by the mock backend, so dedup should
	// collapse it to one entry with both accounts in the eligible set.
	allowMock := func(g string) bool { return g == "Mock" }

	mkWatcher := func(id string) *watcher.Watcher {
		return watcher.New(watcher.Config{
			AccountID:         id,
			Backend:           platformtest.New(),
			Session:           platform.Session{AccessToken: "x"},
			Notifier:          silentNotifier{},
			TickInterval:      5 * time.Millisecond,
			HeartbeatInterval: 5 * time.Millisecond,
			AllowGame:         allowMock,
		})
	}

	wA := mkWatcher("acc-a")
	wB := mkWatcher("acc-b")
	s.AddEntry(NewEntry("acc-a", wA))
	s.AddEntry(NewEntry("acc-b", wB))

	// Run scheduler until both watchers settle (they sleep after
	// claiming the single mock benefit). This guarantees both have
	// populated LastDiscovery at least once.
	require.NoError(t, s.Start(ctx))
	s.Wait()

	snap := s.DiscoverySnapshot()
	require.Len(t, snap, 1, "expected the one mock campaign deduped across both watchers")

	dc := snap[0]
	assert.Equal(t, "camp1", dc.ID)
	assert.Equal(t, "Mock", dc.Game)
	assert.ElementsMatch(t, []string{"acc-a", "acc-b"}, dc.EligibleAccounts)
}

// TestDiscoverySnapshot_SourceVsEligible separates "saw the campaign"
// from "is allowed to mine it". The watcher caches the FULL backend
// result so SourceAccounts can include accounts that observed the
// campaign even when their whitelist rejects it.
func TestDiscoverySnapshot_SourceVsEligible(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	s := New(Options{Notifier: silentNotifier{}})
	allowMock := func(g string) bool { return g == "Mock" }
	allowOther := func(g string) bool { return g == "Other" }

	mkWatcher := func(id string, allow func(string) bool) *watcher.Watcher {
		return watcher.New(watcher.Config{
			AccountID:         id,
			Backend:           platformtest.New(),
			Session:           platform.Session{AccessToken: "x"},
			Notifier:          silentNotifier{},
			TickInterval:      5 * time.Millisecond,
			HeartbeatInterval: 5 * time.Millisecond,
			AllowGame:         allow,
		})
	}

	s.AddEntry(NewEntry("acc-a", mkWatcher("acc-a", allowMock)))
	s.AddEntry(NewEntry("acc-b", mkWatcher("acc-b", allowOther)))

	require.NoError(t, s.Start(ctx))
	s.Wait()

	snap := s.DiscoverySnapshot()
	require.Len(t, snap, 1)
	dc := snap[0]
	assert.Equal(t, []string{"acc-a"}, dc.EligibleAccounts,
		"only acc-a has Mock whitelisted")
	assert.ElementsMatch(t, []string{"acc-a", "acc-b"}, dc.SourceAccounts,
		"both backends returned the campaign, even if acc-b can't mine it")
}

// TestDiscoverySnapshot_HidesNonWhitelisted ensures campaigns not
// whitelisted by ANY account are filtered out. The mock backend
// returns a "Mock" campaign; neither watcher's whitelist covers it,
// so the snapshot should be empty.
func TestDiscoverySnapshot_HidesNonWhitelisted(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	s := New(Options{Notifier: silentNotifier{}})
	allowNone := func(g string) bool { return g == "DoesNotExist" }

	w := watcher.New(watcher.Config{
		AccountID:         "acc-a",
		Backend:           platformtest.New(),
		Session:           platform.Session{AccessToken: "x"},
		Notifier:          silentNotifier{},
		TickInterval:      5 * time.Millisecond,
		HeartbeatInterval: 5 * time.Millisecond,
		AllowGame:         allowNone,
	})
	s.AddEntry(NewEntry("acc-a", w))

	require.NoError(t, s.Start(ctx))
	s.Wait()

	assert.Empty(t, s.DiscoverySnapshot(),
		"campaigns no account has whitelisted must not be surfaced")
}

// TestFindDiscoveredCampaign_ByID returns the cached entry that
// backs the modal handler, or ok=false when the ID is unknown.
func TestFindDiscoveredCampaign_ByID(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	s := New(Options{Notifier: silentNotifier{}})
	w := watcher.New(watcher.Config{
		AccountID:         "acc-a",
		Backend:           platformtest.New(),
		Session:           platform.Session{AccessToken: "x"},
		Notifier:          silentNotifier{},
		TickInterval:      5 * time.Millisecond,
		HeartbeatInterval: 5 * time.Millisecond,
		AllowGame:         func(g string) bool { return g == "Mock" },
	})
	s.AddEntry(NewEntry("acc-a", w))

	require.NoError(t, s.Start(ctx))
	s.Wait()

	dc, ok := s.FindDiscoveredCampaign("camp1")
	require.True(t, ok)
	assert.Equal(t, "Mock Campaign", dc.Name)

	_, ok = s.FindDiscoveredCampaign("does-not-exist")
	assert.False(t, ok)
}
