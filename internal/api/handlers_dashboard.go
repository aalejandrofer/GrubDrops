package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/aalejandrofer/rust-drops-miner/internal/scheduler"
	"github.com/aalejandrofer/rust-drops-miner/internal/store/gen"
)

type dashboardDeps struct {
	q   *gen.Queries
	t   Renderer
	sch *scheduler.Scheduler
}

type dashTelemetry struct {
	WatchTimeToday string // "04:18 h:m"
	ClaimsWeek     int
	ActiveCamps    int
	InProgress     int
	NextClaimETA   string // "00:13 h:m" or "—"
	NextClaimName  string // "Wolf Helmet" or ""
	HeartbeatsHour int
}

type dashMineCard struct {
	ID       string
	Name     string
	Login    string
	Platform string // "twitch" | "kick"
	State    string // "watching" | "claiming" | "pick_stream" | "sleeping" | "idle" | "stopped"
	StateSub string // free-text aside
	Uptime   string // "17m on stream" or "—"
	Enabled  bool

	// Now-playing strip
	Channel        string
	ChannelInitial string
	ChannelGame    string
	ChannelViews   string // formatted, e.g. "62.4k" or "—"

	// Current drop
	DropName     string
	DropCampaign string
	DropMins     int
	DropReq      int
	DropPercent  int
	DropETA      string // "~01:13" or "—"

	// Queue
	Queue []dashQueueItem

	// Footer
	WatchToday  string // "watch 4h17m / 24h"
	ClaimsToday int
}

type dashQueueItem struct {
	N    int
	Name string
	Sub  string // "twitch · Rust Drops"
	Req  string // "60m"
}

type dashCampaign struct {
	Name       string
	Platform   string // "twitch" | "kick"
	Drops      int
	Channels   int
	EndsIn     string // "12d" or "18h"
	EndsUrgent bool
	Claimed    int
	Total      int
}

type dashPrioItem struct {
	Rank    int
	Name    string
	Sub     string // "twitch · ends 18h"
	Enabled bool
}

type dashChannel struct {
	Login    string
	Initial  string
	Game     string
	Live     bool
	Duration string // "4h27m" if live, "" otherwise
	LastLive string // "6h ago" if offline
	Views    string // "62.4k" or ""
}

type dashEvent struct {
	Time     string // "14:31:02"
	Color    string // CSS var name fragment, e.g. "green", "amber", "blue", "muted", "red"
	BodyHTML string // pre-escaped HTML (we control this)
	Account  string
}

type dashPage struct {
	Tele          dashTelemetry
	NextClaims    []dashMineCard // up to 4 items, sorted by ETA
	Mining        []dashMineCard
	ActiveCamps   []dashCampaign
	Priority      []dashPrioItem
	ChannelTabs   []string // campaign names for the tabs
	ChannelActive string
	Channels      []dashChannel
	Events        []dashEvent
	UpdatedAt     string // "1.2s ago"
	NodeAddr      string // "10.10.2.40"
	Uptime        string // "17h 42m"
}

func (d dashboardDeps) collectPage(r *http.Request) dashPage {
	accs, _ := d.q.ListAllAccounts(r.Context())
	stateByID := map[string]string{}
	for _, s := range d.sch.Snapshot() {
		stateByID[s.AccountID] = s.State
	}

	cards := make([]dashMineCard, 0, len(accs))
	for _, a := range accs {
		st, ok := stateByID[a.ID]
		if !ok {
			st = "stopped"
		}
		c := dashMineCard{
			ID:       a.ID,
			Name:     a.DisplayName,
			Login:    "@" + a.Login,
			Platform: a.Platform,
			State:    st,
			Enabled:  a.Enabled == 1,
		}
		// Placeholder enrichment until queries exist.
		switch st {
		case "watching":
			c.StateSub = "live"
			c.Uptime = "—"
			c.Channel = "scanning…"
			c.ChannelInitial = "?"
			c.ChannelGame = a.Platform + " · drops enabled"
			c.ChannelViews = "—"
			c.DropName = "—"
			c.DropCampaign = ""
			c.DropMins, c.DropReq = 0, 1
			c.DropPercent = 0
			c.DropETA = "—"
		case "claiming":
			c.StateSub = "claim in flight"
			c.DropName = "—"
			c.DropMins, c.DropReq = 1, 1
			c.DropPercent = 100
			c.DropETA = "claiming…"
		case "pick_stream":
			c.StateSub = "scanning channels"
		case "sleeping":
			c.StateSub = "no eligible campaign"
		case "needs_auth":
			c.StateSub = "login required"
		}
		c.WatchToday = "—"
		c.ClaimsToday = 0
		cards = append(cards, c)
	}

	page := dashPage{
		Tele: dashTelemetry{
			WatchTimeToday: "—",
			ClaimsWeek:     0,
			ActiveCamps:    0,
			InProgress:     countActive(cards),
			NextClaimETA:   "—",
			NextClaimName:  "",
			HeartbeatsHour: 0,
		},
		Mining:        cards,
		NextClaims:    stubNextClaims(cards),
		ActiveCamps:   stubActiveCamps(),
		Priority:      stubPriority(),
		ChannelTabs:   []string{"Rust Twitch Drops"},
		ChannelActive: "Rust Twitch Drops",
		Channels:      stubChannels(),
		Events:        stubEvents(),
		UpdatedAt:     nowPoll(time.Now()),
		NodeAddr:      "10.10.2.40",
		Uptime:        fmt.Sprintf("%dh %02dm", 17, 42),
	}
	return page
}

func countActive(cards []dashMineCard) int {
	n := 0
	for _, c := range cards {
		switch c.State {
		case "watching", "claiming", "pick_stream":
			n++
		}
	}
	return n
}

func stubNextClaims(cards []dashMineCard) []dashMineCard {
	out := []dashMineCard{}
	for i, c := range cards {
		if i >= 4 {
			break
		}
		out = append(out, c)
	}
	return out
}

func stubActiveCamps() []dashCampaign {
	return []dashCampaign{
		{Name: "Rust Twitch Drops", Platform: "twitch", Drops: 3, Channels: 12, EndsIn: "12d", Claimed: 1, Total: 3},
		{Name: "Holiday Drops", Platform: "twitch", Drops: 2, Channels: 4, EndsIn: "5d", Claimed: 0, Total: 2},
		{Name: "Kick Rust Drops", Platform: "kick", Drops: 5, Channels: 8, EndsIn: "21d", Claimed: 2, Total: 5},
		{Name: "Kick Holiday", Platform: "kick", Drops: 1, Channels: 3, EndsIn: "8d", Claimed: 0, Total: 1},
		{Name: "Rust Tournament Drops", Platform: "twitch", Drops: 2, Channels: 6, EndsIn: "18h", EndsUrgent: true, Claimed: 0, Total: 2},
	}
}

func stubPriority() []dashPrioItem {
	return []dashPrioItem{
		{Rank: 1, Name: "Rust Tournament", Sub: "twitch · ends 18h", Enabled: true},
		{Rank: 2, Name: "Rust Twitch Drops", Sub: "twitch · main", Enabled: true},
		{Rank: 3, Name: "Kick Rust Drops", Sub: "kick · main", Enabled: true},
		{Rank: 4, Name: "Holiday Drops", Sub: "twitch · seasonal", Enabled: true},
		{Rank: 5, Name: "Kick Holiday", Sub: "kick · seasonal", Enabled: false},
	}
}

func stubChannels() []dashChannel {
	return []dashChannel{
		{Login: "shroud", Initial: "S", Game: "rust", Live: true, Duration: "4h27m", Views: "62.4k"},
		{Login: "nickmercs", Initial: "N", Game: "rust", Live: true, Duration: "2h08m", Views: "31.2k"},
		{Login: "welyn", Initial: "W", Game: "rust", Live: true, Duration: "6h11m", Views: "18.7k"},
		{Login: "blooprint", Initial: "B", Game: "rust", Live: false, LastLive: "6h ago"},
		{Login: "mendo", Initial: "M", Game: "rust", Live: false, LastLive: "2d ago"},
		{Login: "facepunch", Initial: "F", Game: "rust", Live: false, LastLive: "1w ago"},
	}
}

func stubEvents() []dashEvent {
	return []dashEvent{
		{Time: "14:31:02", Color: "green", BodyHTML: "<em>claim</em> · Wolf Helmet recorded", Account: "helmet_farmer"},
		{Time: "14:30:44", Color: "amber", BodyHTML: "progress · Salvaged Cleaver 100% — claiming", Account: "demo_two"},
		{Time: "14:24:17", Color: "blue", BodyHTML: "state · pick_stream → watching (shroud)", Account: "helmet_farmer"},
		{Time: "14:22:01", Color: "muted", BodyHTML: "discovery · 8 active campaigns", Account: "—"},
		{Time: "14:18:33", Color: "green", BodyHTML: "<em>claim</em> · Crate Skin recorded", Account: "backup_acc"},
		{Time: "14:14:09", Color: "blue", BodyHTML: "auth · token refreshed", Account: "demo_two"},
		{Time: "14:09:55", Color: "red", BodyHTML: "error · sidecar timeout, retrying", Account: "demo_two"},
		{Time: "14:03:21", Color: "muted", BodyHTML: "heartbeat · 60 ticks / 60s", Account: "—"},
	}
}

func (d dashboardDeps) page(w http.ResponseWriter, r *http.Request) {
	render(w, d.t, "dashboard.html", templateData{
		AuthedAdmin: true,
		CSRFToken:   csrfToken(r),
		Active:      "dashboard",
		Page:        d.collectPage(r),
	})
}

func (d dashboardDeps) cards(w http.ResponseWriter, r *http.Request) {
	// HTMX partial — refreshes just the mining cards block.
	renderPartial(w, d.t, "dashboard_cards", d.collectPage(r).Mining)
}

// nowPoll formats how long ago t was, for the "last poll" display.
func nowPoll(t time.Time) string {
	return fmt.Sprintf("%.1fs ago", time.Since(t).Seconds())
}
