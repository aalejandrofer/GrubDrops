// Package plan decides, per account, which drops to mine and in what order.
// It is pure: no I/O, no clocks.
package plan

import (
	"sort"
	"strings"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// Game is a chosen game. TwitchName, when set, is Twitch's name for it, so a
// game Twitch renamed still matches its Twitch campaigns while Kick keeps
// matching Name.
type Game struct{ Name, Slug, TwitchName string }

type Input struct {
	Games     []Game // chosen games, priority order (index 0 = top)
	Campaigns []platform.Campaign
	Progress  []platform.Progress
	// Benefit IDs in GrubDrops' claim log for this account. A log entry counts as
	// claimed unless Progress holds an unclaimed row for the benefit with fewer
	// MinutesWatched than its RequiredMinutes (the inventory says it is not done).
	// An inventory row with Claimed set always counts.
	Claimed map[string]bool
	// Benefit IDs the user marked collected by hand. They count as claimed
	// whatever the inventory says.
	Marked    map[string]bool
	Skipped   map[string]bool // benefit IDs the runner gave up on
	NotLinked map[string]bool // benefit IDs whose claim the platform refused until linked
	Order     []string        // session Up next override, benefit IDs
	Priority  Priority        // campaign order; zero value is PriorityOrdered
}

// Priority picks how campaigns are ordered in the queue.
type Priority string

const (
	// PriorityOrdered mines games in the user's list order.
	PriorityOrdered Priority = "ordered"
	// PriorityEndingSoonest mines the campaign that ends first, across all
	// chosen games. Campaigns without an end date go last.
	PriorityEndingSoonest Priority = "ending_soonest"
)

type Target struct {
	Campaign platform.Campaign
	Benefit  platform.DropBenefit
	Minutes  int // minutes watched per inventory, 0 if absent
}

type Reason string

const (
	ReasonNotLinked    Reason = "not_linked"
	ReasonPrecondition Reason = "precondition"
	ReasonSkipped      Reason = "skipped"
)

type Blocked struct {
	Target
	Reason Reason
}

type Result struct {
	Queue   []Target
	Blocked []Blocked
	Claimed []Target // chosen-game, active benefits that count as claimed
	// Queued targets whose campaign the platform reports unlinked: they are
	// mined, but the claim waits until the user links the game account.
	NeedsLink []Target
}

// Match returns the index of the game whose name, slug or Twitch name
// equals game, case-insensitively.
func Match(games []Game, game string) (rank int, ok bool) {
	g := strings.ToLower(game)
	for i, c := range games {
		if g == strings.ToLower(c.Name) || g == strings.ToLower(c.Slug) || (c.TwitchName != "" && g == strings.ToLower(c.TwitchName)) {
			return i, true
		}
	}
	return 0, false
}

// ApplyOrder moves targets named in order to the front, in order's sequence.
// The rest keep their incoming order. IDs not in q are ignored.
func ApplyOrder(q []Target, order []string) []Target {
	out := make([]Target, 0, len(q))
	used := make(map[int]bool, len(q))
	for _, id := range order {
		for i, t := range q {
			if !used[i] && t.Benefit.ID == id {
				used[i] = true
				out = append(out, t)
				break
			}
		}
	}
	for i, t := range q {
		if !used[i] {
			out = append(out, t)
		}
	}
	return out
}

type entry struct {
	rank       int
	restricted bool
	remaining  int
	endsAt     time.Time
	unlinked   bool
	targets    []Target
	blocked    []Blocked
	claimed    []Target
}

func Build(in Input) Result {
	minutes := map[string]int{}
	hasRow := map[string]bool{}
	invClaimed := map[string]bool{}
	for _, p := range in.Progress {
		hasRow[p.BenefitID] = true
		if p.MinutesWatched > minutes[p.BenefitID] {
			minutes[p.BenefitID] = p.MinutesWatched
		}
		if p.Claimed {
			invClaimed[p.BenefitID] = true
		}
	}
	// Required minutes per benefit, to resolve the log-versus-inventory override.
	required := map[string]int{}
	for _, c := range in.Campaigns {
		for _, b := range c.Benefits {
			if b.RequiredMinutes > required[b.ID] {
				required[b.ID] = b.RequiredMinutes
			}
		}
	}
	claimed := map[string]bool{}
	for id := range invClaimed {
		claimed[id] = true
	}
	for id, v := range in.Marked {
		if v {
			claimed[id] = true
		}
	}
	for id, v := range in.Claimed {
		if !v || invClaimed[id] {
			continue
		}
		if hasRow[id] && minutes[id] < required[id] {
			continue // inventory shows it unfinished: the log is stale
		}
		claimed[id] = true
	}

	seen := map[string]bool{}
	var entries []entry
	for _, c := range in.Campaigns {
		rank, ok := Match(in.Games, c.Game)
		if !ok || (c.Status != "" && c.Status != "active") || c.Kind == "reward" {
			continue
		}
		unlinked := Unlinked(c)

		benefits := append([]platform.DropBenefit(nil), c.Benefits...)
		sort.SliceStable(benefits, func(i, j int) bool { return benefits[i].RequiredMinutes < benefits[j].RequiredMinutes })

		e := entry{rank: rank, restricted: len(c.AllowedChannels) > 0, endsAt: c.EndsAt, unlinked: unlinked}
		for _, b := range benefits {
			if b.RequiredMinutes <= 0 || seen[b.ID] {
				continue
			}
			seen[b.ID] = true
			if claimed[b.ID] {
				m := b.RequiredMinutes
				if hasRow[b.ID] {
					m = minutes[b.ID]
				}
				e.claimed = append(e.claimed, Target{Campaign: c, Benefit: b, Minutes: m})
				continue
			}
			t := Target{Campaign: c, Benefit: b, Minutes: minutes[b.ID]}
			switch {
			case in.NotLinked[b.ID]:
				e.blocked = append(e.blocked, Blocked{t, ReasonNotLinked})
			case in.Skipped[b.ID]:
				e.blocked = append(e.blocked, Blocked{t, ReasonSkipped})
			case unmetPrecondition(b, claimed):
				e.blocked = append(e.blocked, Blocked{t, ReasonPrecondition})
			default:
				e.targets = append(e.targets, t)
			}
		}
		// Min remaining counts queued targets only; Blocked benefits must not pull a campaign forward.
		for i, t := range e.targets {
			if rem := max(0, t.Benefit.RequiredMinutes-t.Minutes); i == 0 || rem < e.remaining {
				e.remaining = rem
			}
		}
		entries = append(entries, e)
	}

	sort.SliceStable(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if in.Priority == PriorityEndingSoonest {
			if !a.endsAt.Equal(b.endsAt) {
				if a.endsAt.IsZero() || b.endsAt.IsZero() {
					return b.endsAt.IsZero()
				}
				return a.endsAt.Before(b.endsAt)
			}
			if a.remaining != b.remaining {
				return a.remaining < b.remaining
			}
			return a.rank < b.rank
		}
		if a.rank != b.rank {
			return a.rank < b.rank
		}
		if a.restricted != b.restricted {
			return a.restricted
		}
		return a.remaining < b.remaining
	})

	var res Result
	for _, e := range entries {
		res.Queue = append(res.Queue, e.targets...)
		if e.unlinked {
			res.NeedsLink = append(res.NeedsLink, e.targets...)
		}
		res.Blocked = append(res.Blocked, e.blocked...)
		res.Claimed = append(res.Claimed, e.claimed...)
	}
	res.Queue = ApplyOrder(res.Queue, in.Order)
	return res
}

// Unlinked reports whether the platform says the account is not linked for
// the campaign. Twitch leaves isAccountConnected false on campaigns that need
// no link, so only a publisher link page counts there.
func Unlinked(c platform.Campaign) bool {
	if c.Platform == "twitch" {
		return platform.TwitchNeedsLink(c)
	}
	return c.AccountLinkChecked && !c.AccountLinked
}

func unmetPrecondition(b platform.DropBenefit, claimed map[string]bool) bool {
	for _, p := range b.Preconditions {
		if !claimed[p] {
			return true
		}
	}
	return false
}
