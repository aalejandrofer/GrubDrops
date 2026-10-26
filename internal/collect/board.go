package collect

import (
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/plan"
	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/runner"
)

// CampaignSummary is one active campaign as the Games page shows it.
type CampaignSummary struct {
	ID, Platform, Name string
	Drops              int // benefits with RequiredMinutes > 0
	EndsAt             time.Time
}

// LinkNeed is one account that must link an external account (Riot, Steam,
// ...) before drops of a game can be claimed.
type LinkNeed struct {
	AccountID, Platform string
	URL                 string // http(s) link page, "" when none is known
}

// GameStatus is one chosen game with the campaigns every account can see.
type GameStatus struct {
	Game       plan.Game
	Campaigns  []CampaignSummary // active, non-reward, at least one drop; deduped by platform+ID; EndsAt ascending
	NeedsLink  []LinkNeed        // one per account, AccountID ascending
	LastScanAt time.Time         // latest successful scan of any account; zero = none yet
}

// SafeLinkURL returns raw when it is an absolute http or https URL with a
// host, else "".
func SafeLinkURL(raw string) string {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	return raw
}

// addLinkNeed records that s.AccountID must link for game i, keeping the
// first valid URL seen.
func addLinkNeed(out []GameStatus, idx []map[string]int, i int, s runner.Scan, c platform.Campaign) {
	if idx[i] == nil {
		idx[i] = map[string]int{}
	}
	u := SafeLinkURL(c.AccountLinkURL)
	if j, ok := idx[i][s.AccountID]; ok {
		if out[i].NeedsLink[j].URL == "" {
			out[i].NeedsLink[j].URL = u
		}
		return
	}
	idx[i][s.AccountID] = len(out[i].NeedsLink)
	out[i].NeedsLink = append(out[i].NeedsLink, LinkNeed{AccountID: s.AccountID, Platform: s.Platform, URL: u})
}

// Board keeps the latest successful scan per account and merges them per game.
type Board struct {
	mu    sync.Mutex
	scans map[string]runner.Scan
}

func NewBoard() *Board {
	return &Board{scans: map[string]runner.Scan{}}
}

// Observe records s as the account's latest scan. Failed scans are ignored.
func (b *Board) Observe(s runner.Scan) {
	if s.Err != nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.scans[s.AccountID] = s
}

// Forget drops an account's scan, e.g. after the account is deleted.
func (b *Board) Forget(accountID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.scans, accountID)
}

// Games returns one status per game, in the given order. The result shares no
// state with the board.
func (b *Board) Games(games []plan.Game) []GameStatus {
	b.mu.Lock()
	defer b.mu.Unlock()

	out := make([]GameStatus, len(games))
	for i, g := range games {
		out[i].Game = g
	}
	seen := make([]map[string]bool, len(games))
	links := make([]map[string]int, len(games))
	// Accounts merge in ID order, so the first account to report a campaign
	// wins the same way on every call.
	accounts := make([]string, 0, len(b.scans))
	for id := range b.scans {
		accounts = append(accounts, id)
	}
	sort.Strings(accounts)
	var last time.Time
	for _, id := range accounts {
		s := b.scans[id]
		if s.At.After(last) {
			last = s.At
		}
		// Unclaimed drops the planner blocked as not linked (Twitch), then
		// campaigns whose claim the platform rejected until linked (Kick).
		for _, bl := range s.Result.Blocked {
			if bl.Reason != plan.ReasonNotLinked {
				continue
			}
			if i, ok := plan.Match(games, bl.Campaign.Game); ok {
				addLinkNeed(out, links, i, s, bl.Campaign)
			}
		}
		for _, c := range s.Campaigns {
			if (c.Status != "" && c.Status != "active") || c.Kind == "reward" {
				continue
			}
			if c.ClaimNeedsLink {
				if i, ok := plan.Match(games, c.Game); ok {
					addLinkNeed(out, links, i, s, c)
				}
			}
			drops := 0
			for _, ben := range c.Benefits {
				if ben.RequiredMinutes > 0 {
					drops++
				}
			}
			if drops == 0 {
				continue
			}
			i, ok := plan.Match(games, c.Game)
			if !ok {
				continue
			}
			key := c.Platform + "/" + c.ID
			if seen[i] == nil {
				seen[i] = map[string]bool{}
			}
			if seen[i][key] {
				continue
			}
			seen[i][key] = true
			out[i].Campaigns = append(out[i].Campaigns, CampaignSummary{
				ID: c.ID, Platform: c.Platform, Name: c.Name, Drops: drops, EndsAt: c.EndsAt,
			})
		}
	}
	for i := range out {
		out[i].LastScanAt = last
		cs := out[i].Campaigns
		// Ties on EndsAt break by ID, then platform, for a stable order.
		sort.Slice(cs, func(x, y int) bool {
			if !cs[x].EndsAt.Equal(cs[y].EndsAt) {
				return cs[x].EndsAt.Before(cs[y].EndsAt)
			}
			if cs[x].ID != cs[y].ID {
				return cs[x].ID < cs[y].ID
			}
			return cs[x].Platform < cs[y].Platform
		})
	}
	return out
}
