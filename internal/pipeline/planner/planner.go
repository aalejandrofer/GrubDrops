// Package planner turns drop state, campaigns and live channels into one
// decision: which channel to watch and which drops that serves. Pure: no
// I/O, no clock reads, no goroutines.
package planner

import (
	"sort"
	"strings"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/pipeline/dropstate"
	"github.com/aalejandrofer/grubdrops/internal/platform"
)

type Kind int

const (
	Idle Kind = iota
	Mine
	ForceWatch
)

func (k Kind) String() string {
	switch k {
	case Mine:
		return "mine"
	case ForceWatch:
		return "force_watch"
	default:
		return "idle"
	}
}

// SwapHold is the minimum time on a channel before the planner switches to
// a better one, unless the current channel is dead or cooling down.
const SwapHold = 10 * time.Minute

const (
	ReasonNothingToMine  = "nothing_to_mine"
	ReasonNoLiveChannels = "no_live_channels"
)

type Settings struct {
	GameRank         func(game string) int
	PriorityMode     string
	StreamerPriority []string
	ForceWatch       []platform.Stream
}

type Input struct {
	Now       time.Time
	Rows      map[string]dropstate.Row
	Campaigns []platform.Campaign
	Settings  Settings
	Live      map[string][]platform.Stream
	Cooldowns map[string]time.Time
	Current   *Decision
	LastSwap  time.Time
}

type Decision struct {
	Kind       Kind
	CampaignID string
	Channel    platform.Stream
	Serves     []string
	Reason     string
}

// Same reports whether two decisions watch the same thing.
func (d Decision) Same(o Decision) bool {
	return d.Kind == o.Kind && strings.EqualFold(d.Channel.Channel, o.Channel.Channel)
}

func key(ch string) string { return strings.ToLower(ch) }

func cooling(in Input, ch string) bool {
	until, ok := in.Cooldowns[key(ch)]
	return ok && in.Now.Before(until)
}

func inWindow(c platform.Campaign, now time.Time) bool {
	if c.Status != "" && c.Status != "active" {
		return false
	}
	if !c.StartsAt.IsZero() && now.Before(c.StartsAt) {
		return false
	}
	return c.EndsAt.IsZero() || now.Before(c.EndsAt)
}

// mineable returns the campaign's mineable drop ids, lowest tier first.
func mineable(in Input, c platform.Campaign) []string {
	type d struct {
		id  string
		req int
	}
	seen := map[string]bool{}
	var ds []d
	for _, b := range c.Benefits {
		if seen[b.ID] || !dropstate.Mineable(in.Rows[b.ID]) {
			continue
		}
		ok := true
		for _, p := range b.Preconditions {
			if in.Rows[p].Status != dropstate.Claimed {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		seen[b.ID] = true
		ds = append(ds, d{b.ID, b.RequiredMinutes})
	}
	sort.SliceStable(ds, func(i, j int) bool {
		if ds[i].req != ds[j].req {
			return ds[i].req < ds[j].req
		}
		return ds[i].id < ds[j].id
	})
	out := make([]string, len(ds))
	for i, x := range ds {
		out[i] = x.id
	}
	return out
}

func ends(c platform.Campaign) time.Time {
	if c.EndsAt.IsZero() {
		return time.Unix(1<<62, 0)
	}
	return c.EndsAt
}

// Candidates returns in-window campaigns with at least one mineable drop,
// best first: game rank then ending soonest (ending soonest first when
// PriorityMode is "ending_soonest").
func Candidates(in Input) []platform.Campaign {
	var out []platform.Campaign
	for _, c := range in.Campaigns {
		if inWindow(c, in.Now) && len(mineable(in, c)) > 0 {
			out = append(out, c)
		}
	}
	rank := func(g string) int {
		if in.Settings.GameRank == nil {
			return 0
		}
		return in.Settings.GameRank(g)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if in.Settings.PriorityMode == "ending_soonest" && !ends(a).Equal(ends(b)) {
			return ends(a).Before(ends(b))
		}
		if ra, rb := rank(a.Game), rank(b.Game); ra != rb {
			return ra < rb
		}
		if !ends(a).Equal(ends(b)) {
			return ends(a).Before(ends(b))
		}
		return a.ID < b.ID
	})
	return out
}

func hasChannel(streams []platform.Stream, ch string) bool {
	for _, s := range streams {
		if strings.EqualFold(s.Channel, ch) {
			return true
		}
	}
	return false
}

func servedBy(in Input, cands []platform.Campaign, ch string) []string {
	var out []string
	for _, c := range cands {
		if hasChannel(in.Live[c.ID], ch) {
			out = append(out, mineable(in, c)...)
		}
	}
	return out
}

func pickChannel(in Input, c platform.Campaign) (platform.Stream, bool) {
	prio := map[string]int{}
	for i, l := range in.Settings.StreamerPriority {
		if _, dup := prio[key(l)]; !dup {
			prio[key(l)] = i
		}
	}
	live := append([]platform.Stream(nil), in.Live[c.ID]...)
	sort.SliceStable(live, func(i, j int) bool {
		pi, iok := prio[key(live[i].Channel)]
		pj, jok := prio[key(live[j].Channel)]
		if iok != jok {
			return iok
		}
		if iok && pi != pj {
			return pi < pj
		}
		return live[i].ViewerCount > live[j].ViewerCount
	})
	for _, s := range live {
		if !cooling(in, s.Channel) {
			return s, true
		}
	}
	return platform.Stream{}, false
}

func forceOrIdle(in Input, reason string) Decision {
	for _, s := range in.Settings.ForceWatch {
		if !cooling(in, s.Channel) {
			return Decision{Kind: ForceWatch, Channel: s, Reason: "force-watch " + s.Channel}
		}
	}
	return Decision{Kind: Idle, Reason: reason}
}

// Plan decides what the account should watch now.
func Plan(in Input) Decision {
	cands := Candidates(in)
	if len(cands) == 0 {
		return forceOrIdle(in, ReasonNothingToMine)
	}
	if cur := in.Current; cur != nil && cur.Kind == Mine && in.Now.Sub(in.LastSwap) < SwapHold && !cooling(in, cur.Channel.Channel) {
		if serves := servedBy(in, cands, cur.Channel.Channel); len(serves) > 0 {
			d := *cur
			d.Serves, d.Reason = serves, "holding current channel"
			return d
		}
	}
	for _, c := range cands {
		ch, ok := pickChannel(in, c)
		if !ok {
			continue
		}
		reason := "top viewers"
		for _, l := range in.Settings.StreamerPriority {
			if strings.EqualFold(l, ch.Channel) {
				reason = "priority streamer"
				break
			}
		}
		return Decision{Kind: Mine, CampaignID: c.ID, Channel: ch, Serves: servedBy(in, cands, ch.Channel), Reason: reason}
	}
	return forceOrIdle(in, ReasonNoLiveChannels)
}
