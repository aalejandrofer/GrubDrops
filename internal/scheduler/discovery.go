package scheduler

import (
	"sort"
	"strings"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/watcher"
)

// AccountDiscovery is the per-account view of the most recent
// ListActiveCampaigns result. CapturedAt is zero when the watcher has
// never completed a successful discovery tick yet (e.g. nopRunner-backed
// entries, or watchers that just started up).
type AccountDiscovery struct {
	AccountID  string
	CapturedAt time.Time
	Campaigns  []platform.Campaign
	// AllowGame is the account's whitelist predicate. Nil for legacy
	// "mine anything" configs and for nopRunner-backed entries.
	AllowGame func(game string) bool
}

// WatcherDiscoveries returns the cached ListActiveCampaigns result for
// every entry in the scheduler. nopRunner-backed entries surface as
// AccountDiscovery values with nil Campaigns and a zero CapturedAt.
func (s *Scheduler) WatcherDiscoveries() []AccountDiscovery {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]AccountDiscovery, 0, len(s.entries))
	for _, e := range s.entries {
		w, ok := e.runner.(*watcher.Watcher)
		if !ok {
			out = append(out, AccountDiscovery{AccountID: e.id})
			continue
		}
		camps, at := w.LastDiscovery()
		out = append(out, AccountDiscovery{
			AccountID:  e.id,
			CapturedAt: at,
			Campaigns:  camps,
			AllowGame:  w.AllowGame(),
		})
	}
	return out
}

// DiscoveredCampaign augments a platform.Campaign with the set of
// accounts that consider it eligible (the union of every enabled
// account's whitelist that matched the campaign's Game). The dashboard
// uses EligibleAccounts to populate the "which accounts have this"
// section of the campaign detail modal.
type DiscoveredCampaign struct {
	platform.Campaign
	EligibleAccounts []string
	// SourceAccounts is the set of accounts whose ListActiveCampaigns
	// returned this campaign. A campaign may be seen by accounts that
	// don't have its game whitelisted (the Twitch backend, for example,
	// reports everything live) — those accounts will not mine it but
	// confirm it's a real, live campaign right now.
	SourceAccounts []string
}

// DiscoverySnapshot returns the unioned, deduped list of active
// campaigns discovered by every watcher, filtered to the union of every
// enabled account's whitelist (i.e. a campaign is included iff at least
// one account has its Game whitelisted). Result is sorted by EndsAt
// ascending so the soonest-ending campaigns appear first.
func (s *Scheduler) DiscoverySnapshot() []DiscoveredCampaign {
	discoveries := s.WatcherDiscoveries()
	byID := map[string]*DiscoveredCampaign{}
	// Per-campaign sets to dedupe account lists.
	eligibleSets := map[string]map[string]struct{}{}
	sourceSets := map[string]map[string]struct{}{}

	for _, d := range discoveries {
		if len(d.Campaigns) == 0 {
			continue
		}
		for _, c := range d.Campaigns {
			// The dashboard's "Active Campaigns" sidebar only shows
			// in-flight work — past + upcoming live in the /drops page.
			// Treat empty Status as active for backwards compatibility
			// with the platformtest MockBackend.
			if c.Status != "" && c.Status != "active" {
				continue
			}
			// Record source — this account's backend returned the campaign.
			if _, ok := sourceSets[c.ID]; !ok {
				sourceSets[c.ID] = map[string]struct{}{}
			}
			sourceSets[c.ID][d.AccountID] = struct{}{}

			// Whitelist check. If this account has a whitelist and the
			// game matches, the account is "eligible" for the campaign.
			// If no whitelist is configured, treat the account as
			// eligible for everything (legacy behaviour).
			eligible := d.AllowGame == nil || d.AllowGame(c.Game)
			if !eligible {
				continue
			}
			if existing, ok := byID[c.ID]; ok {
				// Refresh fields from the most recent discovery so the
				// modal shows the freshest EndsAt/Benefits. Iteration
				// order isn't time-sorted but the backend response is
				// authoritative for any given account, so the last-write
				// wins approach is fine.
				existing.Campaign = c
			} else {
				cp := c
				dc := &DiscoveredCampaign{Campaign: cp}
				byID[c.ID] = dc
				eligibleSets[c.ID] = map[string]struct{}{}
			}
			eligibleSets[c.ID][d.AccountID] = struct{}{}
		}
	}

	out := make([]DiscoveredCampaign, 0, len(byID))
	for id, dc := range byID {
		dc.EligibleAccounts = sortedKeys(eligibleSets[id])
		dc.SourceAccounts = sortedKeys(sourceSets[id])
		out = append(out, *dc)
	}
	sort.SliceStable(out, func(i, j int) bool {
		// Campaigns without a known EndsAt sort last.
		ie, je := out[i].EndsAt.IsZero(), out[j].EndsAt.IsZero()
		switch {
		case ie && !je:
			return false
		case !ie && je:
			return true
		case ie && je:
			return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
		default:
			return out[i].EndsAt.Before(out[j].EndsAt)
		}
	})
	return out
}

// FindDiscoveredCampaign returns the DiscoveredCampaign with the given
// ID, or (zero, false). Used by the dashboard's campaign-detail handler
// to render the modal without hitting any backend.
func (s *Scheduler) FindDiscoveredCampaign(id string) (DiscoveredCampaign, bool) {
	for _, dc := range s.DiscoverySnapshot() {
		if dc.ID == id {
			return dc, true
		}
	}
	return DiscoveredCampaign{}, false
}

func sortedKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
