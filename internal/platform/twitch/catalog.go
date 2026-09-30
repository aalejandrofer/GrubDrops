package twitch

import (
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/gameslug"
	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// catalogTTL bounds how long a published campaign list is trusted. The
// non-TV discovery pass republishes every tick, so an older entry means
// its publisher stopped (account removed, session dead).
const catalogTTL = 45 * time.Minute

// Catalog is a process-wide, read-only store of the Twitch campaign list
// found by non-TV (Android/legacy) sessions through ViewerDropsDashboard +
// DropCampaignDetails. Twitch hides both from TV-client tokens, and the
// TV channel-first path (AvailableDrops) returns one campaign per channel,
// so a global badge campaign can mask every other campaign of a game. TV
// backends merge this catalog into their own discovery. The data is
// campaign-level only: watch/claim requests keep each session's own
// client profile.
type Catalog struct {
	mu      sync.Mutex
	sources map[string]catalogEntry // publisher key -> its latest list
}

type catalogEntry struct {
	campaigns []platform.Campaign
	allowed   map[string][]string
	at        time.Time
}

// NewCatalog returns an empty catalog.
func NewCatalog() *Catalog {
	return &Catalog{sources: map[string]catalogEntry{}}
}

// Publish replaces source's campaign list. The stored copy is made
// account-neutral: link flags become optimistic (AccountLinked=true,
// AccountLinkChecked=false) so one account's link state never leaks to
// another; the campaign-level AccountLinkURL is kept.
func (c *Catalog) Publish(source string, camps []platform.Campaign, allowed map[string][]string) {
	if c == nil {
		return
	}
	cp := make([]platform.Campaign, 0, len(camps))
	for _, camp := range camps {
		camp.AccountLinked = true
		camp.AccountLinkChecked = false
		camp.Benefits = append([]platform.DropBenefit(nil), camp.Benefits...)
		camp.AllowedChannels = append([]string(nil), camp.AllowedChannels...)
		cp = append(cp, camp)
	}
	al := make(map[string][]string, len(allowed))
	for id, logins := range allowed {
		al[id] = append([]string(nil), logins...)
	}
	c.mu.Lock()
	c.sources[source] = catalogEntry{campaigns: cp, allowed: al, at: time.Now()}
	c.evictStale()
	c.mu.Unlock()
}

// evictStale drops sources whose last Publish is older than catalogTTL.
// Called by Publish and snapshot, both already holding mu. Without this a
// source keyed by backend pointer (catalogSource's no-AccountID fallback)
// accumulates forever across scheduler Reloads: the old *Backend becomes
// garbage, but nothing ever deletes its map entry — snapshot merely
// skipped it, so c.sources grew without bound over the process lifetime.
func (c *Catalog) evictStale() {
	now := time.Now()
	for k, e := range c.sources {
		if now.Sub(e.at) >= catalogTTL {
			delete(c.sources, k)
		}
	}
}

// snapshot unions every fresh source. For a campaign more than one source
// carries, the entry with the most benefits wins (a publisher whose
// whitelist lacks the game emits a benefit-less shell), newest breaking
// ties; its allow-list rides along. age is the freshest source's age; ok
// is false when no source is fresh.
func (c *Catalog) snapshot() (camps []platform.Campaign, allowed map[string][]string, age time.Duration, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.evictStale()
	keys := make([]string, 0, len(c.sources))
	for k := range c.sources {
		keys = append(keys, k)
	}
	sort.Strings(keys) // deterministic order across map iteration
	type pick struct {
		camp    platform.Campaign
		allowed []string
		hasAl   bool
		at      time.Time
	}
	best := map[string]pick{}
	var order []string
	var newest time.Time
	for _, k := range keys {
		e := c.sources[k]
		if time.Since(e.at) >= catalogTTL {
			continue
		}
		ok = true
		if e.at.After(newest) {
			newest = e.at
		}
		for _, camp := range e.campaigns {
			al, hasAl := e.allowed[camp.ID]
			cur, seen := best[camp.ID]
			if !seen {
				order = append(order, camp.ID)
			} else if len(camp.Benefits) < len(cur.camp.Benefits) ||
				(len(camp.Benefits) == len(cur.camp.Benefits) && !e.at.After(cur.at)) {
				continue
			}
			best[camp.ID] = pick{camp: camp, allowed: al, hasAl: hasAl, at: e.at}
		}
	}
	if !ok {
		return nil, nil, 0, false
	}
	allowed = map[string][]string{}
	for _, id := range order {
		p := best[id]
		camp := p.camp
		camp.Benefits = append([]platform.DropBenefit(nil), camp.Benefits...)
		camp.AllowedChannels = append([]string(nil), camp.AllowedChannels...)
		camps = append(camps, camp)
		if p.hasAl {
			allowed[id] = append([]string(nil), p.allowed...)
		}
	}
	return camps, allowed, time.Since(newest), true
}

// sessionWantsGame reports whether game is on the session's whitelist:
// GameFilter when set, else a gameslug match against Session.Games.
func sessionWantsGame(s platform.Session, game string) bool {
	if game == "" {
		return false
	}
	if s.GameFilter != nil {
		return s.GameFilter(game)
	}
	slug := gameslug.Slug(game)
	for _, g := range s.Games {
		if gameslug.Slug(g) == slug {
			return true
		}
	}
	return false
}

// mergeCatalog appends to camps the fresh catalog campaigns the TV pass
// didn't find: not already present by ID (so the TV account's own
// Inventory stays authoritative), active with a future end time, and on
// the session's whitelist. Their catalog allow-lists are added to allowed
// when the TV pass supplied none.
func mergeCatalog(cat *Catalog, s platform.Session, camps []platform.Campaign, allowed map[string][]string) []platform.Campaign {
	if cat == nil {
		return camps
	}
	cc, cal, age, ok := cat.snapshot()
	if !ok {
		return camps
	}
	have := make(map[string]struct{}, len(camps))
	for _, c := range camps {
		have[c.ID] = struct{}{}
	}
	now := time.Now()
	merged := 0
	for _, c := range cc {
		if _, dup := have[c.ID]; dup {
			continue
		}
		if c.Status != "active" || c.EndsAt.IsZero() || !c.EndsAt.After(now) {
			continue
		}
		if !sessionWantsGame(s, c.Game) {
			continue
		}
		camps = append(camps, c)
		have[c.ID] = struct{}{}
		if al, has := cal[c.ID]; has {
			if _, tvHas := allowed[c.ID]; !tvHas {
				allowed[c.ID] = al
			}
		}
		merged++
	}
	// Only interesting at INFO when it actually added a campaign; an
	// every-tick no-op merge (the common case once the catalog has
	// converged) would otherwise flood INFO logs for every account.
	if merged > 0 {
		slog.Info("tv discovery: merged shared catalog", "merged", merged, "catalog_age", age.Round(time.Second))
	} else {
		slog.Debug("tv discovery: merged shared catalog", "merged", merged, "catalog_age", age.Round(time.Second))
	}
	return camps
}
