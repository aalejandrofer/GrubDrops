package runner

import (
	"sync"
	"time"
)

// wakeDebounce is how long a CampaignWaker gathers new campaigns before it
// wakes the other accounts, so a burst of scans makes one wake.
const wakeDebounce = 30 * time.Second

// CampaignWaker wakes the other accounts on a platform when one account's
// scan finds a campaign no account there listed before. Each runner scans on
// its own clock, so without it a second account can start a new campaign up
// to a full scan pace late. It tracks the campaigns the plan considered: on
// the account's whitelist and active. An account's first scan only seeds the
// set, so a start or a runner rebuild wakes nobody.
type CampaignWaker struct {
	rescan   func(platform, except string)
	debounce time.Duration

	mu      sync.Mutex
	seen    map[string]map[string]bool // platform -> campaign IDs
	seeded  map[string]bool            // account IDs whose first scan seeded seen
	pending map[string]map[string]bool // platform -> accounts that found a new campaign
}

// NewCampaignWaker returns a waker that calls rescan(platform, except) once
// per debounce window after a new campaign shows up. except is the account
// that found every campaign in the window, or "" when several did. A
// debounce of 0 or less means wakeDebounce.
func NewCampaignWaker(rescan func(platform, except string), debounce time.Duration) *CampaignWaker {
	if debounce <= 0 {
		debounce = wakeDebounce
	}
	return &CampaignWaker{
		rescan:   rescan,
		debounce: debounce,
		seen:     map[string]map[string]bool{},
		seeded:   map[string]bool{},
		pending:  map[string]map[string]bool{},
	}
}

// OnScan takes one runner scan. A failed scan is ignored. Nil-safe.
func (w *CampaignWaker) OnScan(s Scan) {
	if w == nil || s.Err != nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	seen := w.seen[s.Platform]
	if seen == nil {
		seen = map[string]bool{}
		w.seen[s.Platform] = seen
	}
	first := !w.seeded[s.AccountID]
	w.seeded[s.AccountID] = true
	found := false
	for _, id := range resultCampaigns(s) {
		if id != "" && !seen[id] {
			seen[id] = true
			found = true
		}
	}
	if first || !found {
		return
	}
	finders := w.pending[s.Platform]
	if finders == nil {
		finders = map[string]bool{}
		w.pending[s.Platform] = finders
		plat := s.Platform
		time.AfterFunc(w.debounce, func() { w.fire(plat) })
	}
	finders[s.AccountID] = true
}

// fire wakes the platform's accounts for the pending window.
func (w *CampaignWaker) fire(plat string) {
	w.mu.Lock()
	finders := w.pending[plat]
	delete(w.pending, plat)
	w.mu.Unlock()
	except := ""
	if len(finders) == 1 {
		for id := range finders {
			except = id
		}
	}
	w.rescan(plat, except)
}

// resultCampaigns lists the campaign IDs the scan's plan considered: queued,
// blocked or claimed. The plan leaves out campaigns off the whitelist and
// campaigns that are not active.
func resultCampaigns(s Scan) []string {
	var ids []string
	for _, t := range s.Result.Queue {
		ids = append(ids, t.Campaign.ID)
	}
	for _, b := range s.Result.Blocked {
		ids = append(ids, b.Campaign.ID)
	}
	for _, t := range s.Result.Claimed {
		ids = append(ids, t.Campaign.ID)
	}
	return ids
}
