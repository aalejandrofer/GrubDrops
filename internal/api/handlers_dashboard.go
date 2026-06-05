package api

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/alexedwards/scs/v2"

	mlog "github.com/aalejandrofer/rust-drops-miner/internal/log"
	"github.com/aalejandrofer/rust-drops-miner/internal/scheduler"
	"github.com/aalejandrofer/rust-drops-miner/internal/store/gen"
	"github.com/aalejandrofer/rust-drops-miner/internal/watcher"
)

type dashboardDeps struct {
	q     *gen.Queries
	t     Renderer
	sm    *scs.SessionManager
	sch   *scheduler.Scheduler
	ring  *mlog.Ring
	start time.Time
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
	Sub  string // "twitch · Campaign A"
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

// dashLiveChannel is one card in the full-width "Live channels — eligible
// for whitelisted drops" grid that lives under the live-events drawer.
// Populated by liveChannelsFor() from watcher snapshots; the discovery
// cache will later widen this beyond the currently-watched stream.
type dashLiveChannel struct {
	Login    string
	Platform string // "twitch" | "kick"
	URL      string // https://www.twitch.tv/<login> or https://kick.com/<login>
	Initial  string
	Game     string
	Campaign string
	Views    string // formatted, e.g. "62.4k"
	ViewerN  int    // raw, used for sorting
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
	LiveChannels  []dashLiveChannel // wide grid under the events drawer
	Events        []dashEvent
	UpdatedAt     string // "1.2s ago"
	NodeAddr      string // "10.10.2.40"
	Uptime        string // "17h 42m"
}

func (d dashboardDeps) collectPage(r *http.Request) dashPage {
	accs, _ := d.q.ListAllAccounts(r.Context())
	snapshots := d.sch.WatcherSnapshots()
	snapByID := map[string]watcher.Snapshot{}
	for _, s := range snapshots {
		snapByID[s.AccountID] = s
	}

	cards := make([]dashMineCard, 0, len(accs))
	for _, a := range accs {
		snap, ok := snapByID[a.ID]
		if !ok {
			snap = watcher.Snapshot{AccountID: a.ID, State: "stopped"}
		}
		cards = append(cards, mineCardFromSnap(a, snap))
	}

	allowed := allowedLoginsFor(r, d.q, accs)
	page := dashPage{
		Tele:         telemetryFrom(cards, snapshots),
		Mining:       cards,
		NextClaims:   nextClaimsFrom(cards),
		ActiveCamps:  stubActiveCamps(),
		Priority:     stubPriority(),
		LiveChannels: liveChannelsFor(accs, snapshots, allowed),
		Events:       eventsFromRing(d.ring, ""),
		UpdatedAt:    nowPoll(time.Now()),
		Uptime:       formatUptime(time.Since(d.start)),
	}
	return page
}

// allowedLoginsFor returns the union of game-slug + game-name whitelists
// across every account, lowercased. A non-nil but empty result means
// "no whitelist configured anywhere" — callers should treat that as
// "show nothing" rather than "show everything" (matches watcher behaviour).
func allowedLoginsFor(r *http.Request, q *gen.Queries, accs []gen.Account) map[string]struct{} {
	allow := map[string]struct{}{}
	for _, a := range accs {
		rows, err := q.ListAccountGames(r.Context(), a.ID)
		if err != nil {
			continue
		}
		for _, g := range rows {
			if g.Name != "" {
				allow[strings.ToLower(g.Name)] = struct{}{}
			}
			if g.Slug != "" {
				allow[strings.ToLower(g.Slug)] = struct{}{}
			}
		}
	}
	return allow
}

// liveChannelsFor aggregates the currently-watched stream from every
// account's watcher snapshot, filtered by the union of per-account game
// whitelists, sorted by viewer count desc, capped at 24.
//
// This is a stub aggregator until the parallel scheduler-side discovery
// cache lands: today we only know about channels we're actively watching.
// Once that cache exists, fold its per-campaign eligible-channels in
// here too. Whitelist filtering already matches watcher semantics
// (game name OR slug, lowercased) so the cache-backed widening will
// just need to pass the same `allowed` set.
func liveChannelsFor(accs []gen.Account, snaps []watcher.Snapshot, allowed map[string]struct{}) []dashLiveChannel {
	platformByID := map[string]string{}
	for _, a := range accs {
		platformByID[a.ID] = a.Platform
	}
	seen := map[string]bool{} // platform|login dedupe
	out := make([]dashLiveChannel, 0, len(snaps))
	for _, s := range snaps {
		if s.State != "watching" || s.Channel == "" {
			continue
		}
		// Per-account whitelist union — if the whitelist is empty,
		// allow everything (no whitelist configured = legacy behaviour).
		if len(allowed) > 0 && s.CampaignGame != "" {
			g := strings.ToLower(s.CampaignGame)
			if _, ok := allowed[g]; !ok {
				continue
			}
		}
		plat := platformByID[s.AccountID]
		key := plat + "|" + strings.ToLower(s.Channel)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, dashLiveChannel{
			Login:    s.Channel,
			Platform: plat,
			URL:      channelURL(plat, s.Channel),
			Initial:  initial(s.Channel),
			Game:     s.CampaignGame,
			Campaign: s.CampaignName,
			Views:    formatViews(s.ViewerCount),
			ViewerN:  s.ViewerCount,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].ViewerN > out[j].ViewerN
	})
	if len(out) > 24 {
		out = out[:24]
	}
	return out
}

func channelURL(plat, login string) string {
	switch plat {
	case "kick":
		return "https://kick.com/" + login
	case "twitch":
		return "https://www.twitch.tv/" + login
	}
	return "#"
}

// formatViews renders a viewer count compactly: 1234 -> "1.2k",
// 62400 -> "62.4k", 1_200_000 -> "1.2M". Zero renders as "—" so empty
// cards stay legible rather than showing "0".
func formatViews(n int) string {
	if n <= 0 {
		return "—"
	}
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fk", float64(n)/1_000)
	}
	return fmt.Sprintf("%d", n)
}

func mineCardFromSnap(a gen.Account, s watcher.Snapshot) dashMineCard {
	c := dashMineCard{
		ID:       a.ID,
		Name:     a.DisplayName,
		Login:    "@" + a.Login,
		Platform: a.Platform,
		State:    s.State,
		Enabled:  a.Enabled == 1,
	}
	switch s.State {
	case "watching":
		c.StateSub = "live"
		c.Uptime = formatShort(time.Since(s.StartedAt))
		c.Channel = s.Channel
		c.ChannelInitial = initial(s.Channel)
		c.ChannelGame = s.CampaignGame
		c.ChannelViews = "—"
		c.DropName = s.BenefitName
		c.DropCampaign = s.CampaignName
		c.DropMins, c.DropReq = s.MinutesWatched, max1(s.RequiredMinutes)
		c.DropPercent = pct(s.MinutesWatched, s.RequiredMinutes)
		c.DropETA = etaFrom(s.MinutesWatched, s.RequiredMinutes)
	case "claiming":
		c.StateSub = "claim in flight"
		c.DropName = s.BenefitName
		c.DropCampaign = s.CampaignName
		c.DropMins, c.DropReq = s.RequiredMinutes, max1(s.RequiredMinutes)
		c.DropPercent = 100
		c.DropETA = "claiming…"
	case "pick_stream":
		c.StateSub = "scanning channels"
		c.DropName = s.BenefitName
		c.DropCampaign = s.CampaignName
	case "pick_campaign", "idle":
		c.StateSub = "looking for work"
	case "sleeping":
		c.StateSub = "no eligible campaign"
	case "needs_auth":
		c.StateSub = "login required"
	}
	c.WatchToday = "—"
	c.ClaimsToday = 0
	return c
}

func telemetryFrom(cards []dashMineCard, snaps []watcher.Snapshot) dashTelemetry {
	var nextETA time.Duration = -1
	var nextName string
	for _, s := range snaps {
		if s.State != "watching" || s.RequiredMinutes <= 0 {
			continue
		}
		remain := time.Duration(max(0, s.RequiredMinutes-s.MinutesWatched)) * time.Minute
		if nextETA < 0 || remain < nextETA {
			nextETA = remain
			nextName = s.BenefitName
		}
	}
	tele := dashTelemetry{
		InProgress:    countActive(cards),
		ActiveCamps:   distinctCampaigns(snaps),
		NextClaimName: nextName,
	}
	if nextETA >= 0 {
		tele.NextClaimETA = formatHM(nextETA)
	} else {
		tele.NextClaimETA = "—"
	}
	tele.WatchTimeToday = "—"
	return tele
}

func distinctCampaigns(snaps []watcher.Snapshot) int {
	seen := map[string]bool{}
	for _, s := range snaps {
		if s.CampaignID != "" {
			seen[s.CampaignID] = true
		}
	}
	return len(seen)
}

func nextClaimsFrom(cards []dashMineCard) []dashMineCard {
	active := make([]dashMineCard, 0, len(cards))
	for _, c := range cards {
		if c.State == "watching" || c.State == "claiming" {
			active = append(active, c)
		}
	}
	sort.SliceStable(active, func(i, j int) bool {
		return active[i].DropMins*active[j].DropReq < active[j].DropMins*active[i].DropReq
	})
	if len(active) > 4 {
		active = active[:4]
	}
	return active
}

func eventsFromRing(ring *mlog.Ring, filter string) []dashEvent {
	if ring == nil {
		return nil
	}
	lines := ring.Snapshot()
	out := make([]dashEvent, 0, len(lines))
	// reverse — newest first
	for i := len(lines) - 1; i >= 0 && len(out) < 60; i-- {
		l := lines[i]
		category := classifyEvent(l.Msg)
		if filter != "" && filter != "all" && category != filter {
			continue
		}
		out = append(out, dashEvent{
			Time:     l.TS.UTC().Format("15:04:05"),
			Color:    colorFor(l.Level),
			BodyHTML: fmt.Sprintf("<em>%s</em> · %s", category, htmlEscape(l.Msg)),
			Account:  fieldStr(l.Fields, "account"),
		})
	}
	return out
}

func classifyEvent(msg string) string {
	m := strings.ToLower(msg)
	switch {
	case strings.Contains(m, "claim"):
		return "claim"
	case strings.Contains(m, "progress") || strings.Contains(m, "heartbeat"):
		return "progress"
	case strings.Contains(m, "state") || strings.Contains(m, "watcher pickcampaign") || strings.Contains(m, "watcher pickstream"):
		return "state"
	case strings.Contains(m, "error") || strings.Contains(m, "failed"):
		return "error"
	}
	return "info"
}

func colorFor(level string) string {
	switch strings.ToUpper(level) {
	case "ERROR":
		return "red"
	case "WARN":
		return "amber"
	case "INFO":
		return "green"
	case "DEBUG":
		return "muted"
	}
	return "muted"
}

func fieldStr(f map[string]any, k string) string {
	if v, ok := f[k]; ok {
		return fmt.Sprintf("%v", v)
	}
	return "—"
}

func htmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	return r.Replace(s)
}

func formatUptime(d time.Duration) string {
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if h == 0 {
		return fmt.Sprintf("%dm", m)
	}
	return fmt.Sprintf("%dh %02dm", h, m)
}

func formatHM(d time.Duration) string {
	if d <= 0 {
		return "00:00"
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	return fmt.Sprintf("%02d:%02d", h, m)
}

func formatShort(d time.Duration) string {
	m := int(d.Minutes())
	s := int(d.Seconds()) % 60
	if m == 0 {
		return fmt.Sprintf("%ds", s)
	}
	return fmt.Sprintf("%dm%02ds", m, s)
}

func initial(s string) string {
	for _, r := range s {
		return strings.ToUpper(string(r))
	}
	return "?"
}

func max1(n int) int {
	if n <= 0 {
		return 1
	}
	return n
}

func pct(num, den int) int {
	if den <= 0 {
		return 0
	}
	p := num * 100 / den
	if p > 100 {
		return 100
	}
	return p
}

func etaFrom(watched, required int) string {
	rem := required - watched
	if rem <= 0 {
		return "claiming…"
	}
	return formatHM(time.Duration(rem) * time.Minute)
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
	// stub: placeholder data, not real campaigns
	return []dashCampaign{
		{Name: "Active Campaign A", Platform: "twitch", Drops: 3, Channels: 12, EndsIn: "12d", Claimed: 1, Total: 3},
		{Name: "Active Campaign B", Platform: "twitch", Drops: 2, Channels: 4, EndsIn: "5d", Claimed: 0, Total: 2},
		{Name: "Active Campaign C", Platform: "kick", Drops: 5, Channels: 8, EndsIn: "21d", Claimed: 2, Total: 5},
		{Name: "Active Campaign D", Platform: "kick", Drops: 1, Channels: 3, EndsIn: "8d", Claimed: 0, Total: 1},
		{Name: "Active Campaign E", Platform: "twitch", Drops: 2, Channels: 6, EndsIn: "18h", EndsUrgent: true, Claimed: 0, Total: 2},
	}
}

func stubPriority() []dashPrioItem {
	// stub: placeholder priority list
	return []dashPrioItem{
		{Rank: 1, Name: "Campaign A", Sub: "twitch · ends 18h", Enabled: true},
		{Rank: 2, Name: "Campaign B", Sub: "twitch · main", Enabled: true},
		{Rank: 3, Name: "Campaign C", Sub: "kick · main", Enabled: true},
		{Rank: 4, Name: "Campaign D", Sub: "twitch · seasonal", Enabled: true},
		{Rank: 5, Name: "Campaign E", Sub: "kick · seasonal", Enabled: false},
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
	var flash string
	if d.sm != nil {
		flash = d.sm.PopString(r.Context(), "flash")
	}
	render(w, d.t, "dashboard.html", templateData{
		AuthedAdmin: true,
		CSRFToken:   csrfToken(r),
		Active:      "dashboard",
		Flash:       flash,
		Page:        d.collectPage(r),
	})
}

func (d dashboardDeps) cards(w http.ResponseWriter, r *http.Request) {
	// HTMX partial — refreshes just the mining cards block.
	renderPartial(w, d.t, "dashboard_cards", d.collectPage(r).Mining)
}

func (d dashboardDeps) events(w http.ResponseWriter, r *http.Request) {
	filter := r.URL.Query().Get("filter")
	renderPartial(w, d.t, "dashboard_events", eventsFromRing(d.ring, filter))
}

// nowPoll formats how long ago t was, for the "last poll" display.
func nowPoll(t time.Time) string {
	return fmt.Sprintf("%.1fs ago", time.Since(t).Seconds())
}
