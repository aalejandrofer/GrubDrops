package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	mlog "github.com/aalejandrofer/dropsminer/internal/log"
	"github.com/aalejandrofer/dropsminer/internal/platform"
	"github.com/aalejandrofer/dropsminer/internal/scheduler"
	"github.com/aalejandrofer/dropsminer/internal/store/gen"
	"github.com/aalejandrofer/dropsminer/internal/watcher"
)

// ChannelCounter is the backend-side surface the dashboard needs to
// fill the "channels" column on each Active Campaigns row. Both
// twitch.Backend and twitch.BrowserBackend implement it via their
// cached allow-list; kick.Backend implements it by counting distinct
// channels across registered accounts (campaignID is ignored there).
type ChannelCounter interface {
	AllowedChannelCount(campaignID string) int
}

// channelCountersFromRegistry projects the platform registry into the
// map dashboardDeps consumes. Backends that don't implement
// ChannelCounter are silently skipped — those platforms render as
// zero in the Active Campaigns "channels" column.
func channelCountersFromRegistry(reg *platform.Registry) map[string]ChannelCounter {
	if reg == nil {
		return nil
	}
	out := map[string]ChannelCounter{}
	for _, name := range []string{"twitch", "kick"} {
		b, ok := reg.Get(name)
		if !ok {
			continue
		}
		if cc, ok := b.(ChannelCounter); ok {
			out[name] = cc
		}
	}
	return out
}

type dashboardDeps struct {
	q     *gen.Queries
	t     Renderer
	sm    *scs.SessionManager
	sch   *scheduler.Scheduler
	ring  *mlog.Ring
	start time.Time
	// channelCounters is keyed by platform name ("twitch", "kick"). Nil
	// or missing entries make the dashboard fall back to zero for that
	// platform — safer than panicking when a backend isn't wired up.
	channelCounters map[string]ChannelCounter
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
	ID             string
	Name           string
	Login          string
	AccountInitial string // first letter of display name, "?" fallback
	Platform       string // "twitch" | "kick"
	State          string // "watching" | "claiming" | "pick_stream" | "sleeping" | "idle" | "stopped"
	StateSub       string // free-text aside
	Uptime         string // "17m on stream" or "—"
	Enabled        bool

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
	ID         string // platform-side campaign id; identifies the modal target
	Name       string
	Platform   string // "twitch" | "kick"
	Game       string
	Kind       string // "drop" | "reward"
	Drops      int
	Channels   int
	EndsIn     string // "12d" or "18h"
	EndsUrgent bool
	Claimed    int
	Total      int
}

// dashCampaignDetail backs the campaign-detail modal partial. It carries
// the full set of fields the operator wants to inspect when they click a
// row in the Active Campaigns sidebar.
type dashCampaignDetail struct {
	ID               string
	Name             string
	Platform         string
	Game             string
	Status           string
	Kind             string // "drop" | "reward"
	StartsAt         string // formatted, e.g. "2026-06-01 14:00 UTC" or "—"
	EndsAt           string // formatted
	EndsIn           string // "12d" or "18h"
	EndsUrgent       bool
	Benefits         []dashCampaignBenefit
	EligibleAccounts []string // account IDs that have this campaign's game whitelisted
	SourceAccounts   []string // account IDs whose backend surfaced the campaign
	AccountLinked    bool     // user's external account (Mojang etc) linked?
	AccountLinkURL   string   // where to go to link
	RawJSON          string   // pretty-printed JSON for debugging
}

type dashCampaignBenefit struct {
	ID              string
	Name            string
	RequiredMinutes int
	ImageURL        string
}

// dashMiningColumns groups account rows by platform so the dashboard
// can render two side-by-side compact columns ("TWITCH" and "KICK")
// without the template re-bucketing on every render. Ordering inside
// each slice mirrors the underlying account list (no resort) so the
// per-account whitelist priority — set elsewhere — stays the visible
// order on the dashboard.
type dashMiningColumns struct {
	Twitch []dashMineCard
	Kick   []dashMineCard
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
	ID       string // stable-ish identifier for the event row (used by the details toggle)
	Time     string // "14:31:02"
	Kind     string // "claim" | "progress" | "state" | "discovery" | "error" | "auth" | "info"
	Color    string // CSS var name fragment, e.g. "green", "amber", "blue", "muted", "red"
	BodyHTML string // pre-escaped HTML (we control this)
	Account  string
	Platform string // "twitch" | "kick" — drives account label color
	// Details is the structured key=value pairs from the underlying log
	// line. Rendered inside an expandable section under the row.
	Details []dashEventField
}

type dashEventField struct {
	Key   string
	Value string
}

// dashEventAccount is a {ID, Login} pair fed into the per-account
// filter dropdown. The handler matches incoming `?account=` against ID.
type dashEventAccount struct {
	ID    string
	Label string
}

type dashPage struct {
	Tele          dashTelemetry
	NextClaims    []dashMineCard // up to 4 items, sorted by ETA
	Mining        dashMiningColumns
	ActiveCamps   []dashCampaign
	LiveChannels  []dashLiveChannel // wide grid under the events drawer
	Events        []dashEvent
	EventAccounts []dashEventAccount // options for the per-account filter
	EventAccount  string             // currently selected account ID (or "")
	EventFilter   string             // currently selected kind filter (or "all")
	UpdatedAt     string             // "1.2s ago"
	NodeAddr      string             // "10.10.2.40"
	Uptime        string             // "17h 42m"
	Alerts        []dashAlert        // top-of-page CTA banner items
}

type dashAlert struct {
	Kind     string // "needs_auth" | "no_drops"
	Account  string // display @login
	URL      string // direct CTA link
	Action   string // button label
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
	// Build alerts: any account in needs_auth state or sleeping with 0
	// eligible drops gets a top banner pointing at the right CTA.
	var alerts []dashAlert
	for _, c := range cards {
		switch c.State {
		case "needs_auth":
			alerts = append(alerts, dashAlert{
				Kind: "needs_auth", Account: "@" + c.Login,
				URL: "/accounts/" + c.ID + "/login", Action: "Re-authenticate →",
			})
		case "sleeping":
			if c.Platform == "twitch" {
				alerts = append(alerts, dashAlert{
					Kind: "no_drops", Account: "@" + c.Login,
					URL: "/accounts/" + c.ID + "/login", Action: "Switch to device-code login →",
				})
			}
		}
	}

	page := dashPage{
		Tele:          telemetryWithClaims(telemetryFrom(cards, snapshots), d.ring, d.q, r.Context()),
		Alerts:        alerts,
		Mining:        bucketMiningByPlatform(cards),
		NextClaims:    nextClaimsFrom(cards),
		ActiveCamps:   activeCampsFromDiscovery(r.Context(), d.sch, d.channelCounters, d.q),
		LiveChannels:  liveChannelsFor(accs, snapshots, allowed),
		Events:        eventsFromRing(d.ring, "", "", accs),
		EventAccounts: eventAccountsFrom(accs),
		EventFilter:   "all",
		UpdatedAt:     nowPoll(time.Now()),
		Uptime:        formatUptime(time.Since(d.start)),
	}
	return page
}

// bucketMiningByPlatform splits the flat list of mining cards into the
// two platform-keyed slices the new dashboard renders. Unknown
// platforms are dropped — the dashboard only has columns for twitch
// and kick today; new platforms must be wired here explicitly.
func bucketMiningByPlatform(cards []dashMineCard) dashMiningColumns {
	out := dashMiningColumns{}
	for _, c := range cards {
		switch c.Platform {
		case "twitch":
			out.Twitch = append(out.Twitch, c)
		case "kick":
			out.Kick = append(out.Kick, c)
		}
	}
	return out
}

// claimedCounter is the subset of *gen.Queries the dashboard needs to
// fill the "claimed" column. Declared here so tests can inject a stub
// without spinning a real sqlite database.
type claimedCounter interface {
	CountClaimedForCampaign(ctx context.Context, campaignID string) (int64, error)
}

// activeCampsFromDiscovery projects the scheduler's discovery snapshot
// into the row shape the Active Campaigns sidebar renders. The whitelist
// filter is already applied inside DiscoverySnapshot — every row here
// has at least one account that opted into its game.
//
// `chanCounters` (keyed by platform) supplies the eligible-channel count
// per campaign; `q` supplies the cross-account claim count. Either may
// be nil — the corresponding column then renders as zero, matching the
// previous TODO behaviour.
func activeCampsFromDiscovery(ctx context.Context, sch *scheduler.Scheduler, chanCounters map[string]ChannelCounter, q claimedCounter) []dashCampaign {
	if sch == nil {
		return nil
	}
	snap := sch.DiscoverySnapshot()
	out := make([]dashCampaign, 0, len(snap))
	now := time.Now()
	for _, dc := range snap {
		ends := ""
		urgent := false
		if !dc.EndsAt.IsZero() {
			ends = formatEndsIn(dc.EndsAt.Sub(now))
			urgent = dc.EndsAt.Sub(now) < 24*time.Hour && dc.EndsAt.After(now)
		}
		channels := 0
		if cc, ok := chanCounters[dc.Platform]; ok && cc != nil {
			channels = cc.AllowedChannelCount(dc.ID)
		}
		claimed := 0
		if q != nil {
			if n, err := q.CountClaimedForCampaign(ctx, dc.ID); err == nil {
				claimed = int(n)
			}
		}
		out = append(out, dashCampaign{
			ID:         dc.ID,
			Name:       dc.Name,
			Platform:   dc.Platform,
			Game:       dc.Game,
			Kind:       dc.Kind,
			Drops:      len(dc.Benefits),
			Channels:   channels,
			EndsIn:     ends,
			EndsUrgent: urgent,
			Claimed:    claimed,
			Total:      len(dc.Benefits),
		})
	}
	return out
}

// formatEndsIn renders a duration as "12d", "18h", or "42m" so the
// Active Campaigns rows stay compact. Negative durations (already
// expired) render as "ended".
func formatEndsIn(d time.Duration) string {
	if d <= 0 {
		return "ended"
	}
	if d >= 24*time.Hour {
		days := int(d / (24 * time.Hour))
		return fmt.Sprintf("%dd", days)
	}
	if d >= time.Hour {
		return fmt.Sprintf("%dh", int(d/time.Hour))
	}
	return fmt.Sprintf("%dm", int(d/time.Minute))
}

// eventAccountsFrom projects the account list down to {ID, Label}
// pairs for the per-account event-filter dropdown. Sorted by display
// name to match the rest of the dashboard's account ordering.
func eventAccountsFrom(accs []gen.Account) []dashEventAccount {
	out := make([]dashEventAccount, 0, len(accs))
	for _, a := range accs {
		label := a.DisplayName
		if a.Login != "" {
			label = a.DisplayName + " (@" + a.Login + ")"
		}
		out = append(out, dashEventAccount{ID: a.ID, Label: label})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Label < out[j].Label })
	return out
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
		ID:             a.ID,
		Name:           a.DisplayName,
		Login:          "@" + a.Login,
		AccountInitial: initial(a.DisplayName),
		Platform:       a.Platform,
		State:          s.State,
		Enabled:        a.Enabled == 1,
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

// telemetryWithClaims layers the "Drops claimed · 7d" count onto a
// base telemetry struct. Counts two sources: the on-disk claims table
// (real drop claims via platform.Claim) AND any kind=claim event in
// the in-memory log ring (reward reaper claims, which don't go through
// the benefits table so they never make it to claims/). The union is
// deduped by (account_id, title) so the same reward isn't counted
// twice if it appears in both sources.
func telemetryWithClaims(base dashTelemetry, ring *mlog.Ring, q *gen.Queries, ctx context.Context) dashTelemetry {
	since := time.Now().Add(-7 * 24 * time.Hour).Unix()
	seen := map[string]bool{}
	count := 0
	// On-disk claims via ListRecentClaims (claims table is bounded — the
	// claim rows are rare events, so 500 is generous).
	if q != nil {
		if rows, err := q.ListRecentClaims(ctx, 500); err == nil {
			for _, r := range rows {
				if r.ClaimedAt < since {
					continue
				}
				k := r.AccountID + "|" + r.BenefitName
				if seen[k] {
					continue
				}
				seen[k] = true
				count++
			}
		}
	}
	// Reward-reaper claims live in the log ring as kind=claim events.
	// Field map carries account + title.
	if ring != nil {
		for _, l := range ring.Snapshot() {
			if l.Kind != "claim" {
				continue
			}
			if l.TS.Unix() < since {
				continue
			}
			acc := fieldStr(l.Fields, "account")
			title := fieldStr(l.Fields, "title")
			if title == "" {
				continue
			}
			k := acc + "|" + title
			if seen[k] {
				continue
			}
			seen[k] = true
			count++
		}
	}
	base.ClaimsWeek = count
	return base
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

// eventsFromRing transforms the in-memory log ring into the
// dashboard's event drawer model. The ring stores typed entries
// (LogLine.Kind, set by the watcher / login handlers / ringHandler);
// when Kind is empty we fall back to substring matching on the message
// so older un-tagged log lines still classify usefully.
//
// `kindFilter` is one of "" / "all" / "claim" / "progress" / "state" /
// "discovery" / "error" / "auth"; anything else is treated as "all".
// `accountFilter` is the account ID to keep ("" or "all" = keep all).
func eventsFromRing(ring *mlog.Ring, kindFilter, accountFilter string, accs []gen.Account) []dashEvent {
	if ring == nil {
		return nil
	}
	// Build account_id -> @login map so events render the human handle
	// instead of acc_XXXXXXXX... — matches how upstream
	// TwitchDropsMiner labels output.
	labelByID := make(map[string]string, len(accs))
	platformByID := make(map[string]string, len(accs))
	for _, a := range accs {
		labelByID[a.ID] = "@" + a.Login
		platformByID[a.ID] = a.Platform
	}
	lines := ring.Snapshot()
	out := make([]dashEvent, 0, len(lines))
	for i := len(lines) - 1; i >= 0 && len(out) < 80; i-- {
		l := lines[i]
		kind := l.Kind
		if kind == "" {
			kind = classifyEvent(l.Msg, l.Level)
		}
		if kindFilter != "" && kindFilter != "all" && kind != kindFilter {
			continue
		}
		accID := fieldStr(l.Fields, "account")
		if accountFilter != "" && accountFilter != "all" && accID != accountFilter {
			continue
		}
		label := labelByID[accID]
		if label == "" {
			label = accID
		}
		out = append(out, dashEvent{
			ID:       fmt.Sprintf("ev-%d-%d", l.TS.UnixNano(), i),
			Time:     l.TS.UTC().Format("15:04:05"),
			Kind:     kind,
			Color:    colorForKind(kind, l.Level),
			BodyHTML: fmt.Sprintf("<em>%s</em> · %s", kind, htmlEscape(l.Msg)),
			Account:  label,
			Platform: platformByID[accID],
			Details:  detailsFor(l),
		})
	}
	return out
}

// classifyEvent is the fallback for log lines without an explicit
// Kind. Conservative — only fires on unambiguous substrings. New
// structured emitters should set Kind directly instead of relying on
// this.
func classifyEvent(msg, level string) string {
	m := strings.ToLower(msg)
	switch {
	case strings.Contains(m, "claim"):
		return "claim"
	case strings.Contains(m, "progress") || strings.Contains(m, "heartbeat"):
		return "progress"
	case strings.Contains(m, "auth") || strings.Contains(m, "login") || strings.Contains(m, "session") || strings.Contains(m, "device-code") || strings.Contains(m, "cookies"):
		return "auth"
	case strings.Contains(m, "state") || strings.Contains(m, "pickcampaign") || strings.Contains(m, "pickstream") || strings.Contains(m, "starting watch"):
		return "state"
	case strings.Contains(m, "discovery") || strings.Contains(m, "campaign") || strings.Contains(m, "benefit") || strings.Contains(m, "inventory"):
		return "discovery"
	}
	switch strings.ToUpper(level) {
	case "ERROR", "WARN":
		return "error"
	}
	return "info"
}

// colorForKind maps a structured event kind to a CSS variable name.
// `level` is consulted as a fallback so unknown kinds still surface
// errors in red rather than the muted "info" grey.
func colorForKind(kind, level string) string {
	switch kind {
	case "claim":
		return "green"
	case "progress":
		return "amber"
	case "state":
		return "blue"
	case "discovery":
		return "muted"
	case "error":
		return "red"
	case "auth":
		return "accent"
	}
	switch strings.ToUpper(level) {
	case "ERROR":
		return "red"
	case "WARN":
		return "amber"
	}
	return "muted"
}

// detailsFor flattens the structured fields of a log line into a
// stable-ordered slice for rendering under each event row. Keys we
// surface first (account, channel, campaign, benefit, state) get a
// consistent ordering; remaining keys are sorted alphabetically.
// The `kind` field is dropped because it already appears in the row
// header as the colored chip.
func detailsFor(l mlog.LogLine) []dashEventField {
	if len(l.Fields) == 0 {
		return nil
	}
	priority := []string{"account", "platform", "state", "prev", "campaign", "game", "channel", "benefit", "benefit_name", "min_watched", "required", "err"}
	seen := map[string]bool{}
	out := make([]dashEventField, 0, len(l.Fields))
	for _, k := range priority {
		if v, ok := l.Fields[k]; ok {
			out = append(out, dashEventField{Key: k, Value: fmt.Sprintf("%v", v)})
			seen[k] = true
		}
	}
	rest := make([]string, 0, len(l.Fields))
	for k := range l.Fields {
		if k == "kind" || seen[k] {
			continue
		}
		rest = append(rest, k)
	}
	sort.Strings(rest)
	for _, k := range rest {
		out = append(out, dashEventField{Key: k, Value: fmt.Sprintf("%v", l.Fields[k])})
	}
	return out
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

func stubEvents() []dashEvent {
	return []dashEvent{
		{Time: "14:31:02", Kind: "claim", Color: "green", BodyHTML: "<em>claim</em> · Wolf Helmet recorded", Account: "helmet_farmer"},
		{Time: "14:30:44", Kind: "progress", Color: "amber", BodyHTML: "<em>progress</em> · Salvaged Cleaver 100% — claiming", Account: "demo_two"},
		{Time: "14:24:17", Kind: "state", Color: "blue", BodyHTML: "<em>state</em> · pick_stream → watching (shroud)", Account: "helmet_farmer"},
		{Time: "14:22:01", Kind: "discovery", Color: "muted", BodyHTML: "<em>discovery</em> · 8 active campaigns", Account: "—"},
		{Time: "14:18:33", Kind: "claim", Color: "green", BodyHTML: "<em>claim</em> · Crate Skin recorded", Account: "backup_acc"},
		{Time: "14:14:09", Kind: "auth", Color: "blue", BodyHTML: "<em>auth</em> · token refreshed", Account: "demo_two"},
		{Time: "14:09:55", Kind: "error", Color: "red", BodyHTML: "<em>error</em> · sidecar timeout, retrying", Account: "demo_two"},
		{Time: "14:03:21", Kind: "info", Color: "muted", BodyHTML: "<em>heartbeat</em> · 60 ticks / 60s", Account: "—"},
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
	// HTMX partial — refreshes just the mining columns block. The
	// template name stays "dashboard_mining_columns" so the polling
	// endpoint and the page render share the same partial.
	renderPartial(w, d.t, "dashboard_mining_columns", d.collectPage(r).Mining)
}

func (d dashboardDeps) events(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("filter")
	account := r.URL.Query().Get("account")
	accs, _ := d.q.ListAllAccounts(r.Context())
	renderPartial(w, d.t, "dashboard_events", eventsFromRing(d.ring, kind, account, accs))
}

// campaignDetail renders the modal partial for a single discovered
// campaign. HTMX hits this from each Active Campaigns row; the response
// is dropped into the dashboard's #modal target.
func (d dashboardDeps) campaignDetail(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		http.Error(w, "missing campaign id", http.StatusBadRequest)
		return
	}
	dc, ok := d.sch.FindDiscoveredCampaign(id)
	if !ok {
		http.Error(w, "campaign not in discovery cache", http.StatusNotFound)
		return
	}

	// Map account IDs to friendlier "DisplayName (@login)" labels so
	// the modal lists humans, not opaque UUIDs.
	accs, _ := d.q.ListAllAccounts(r.Context())
	labelByID := map[string]string{}
	for _, a := range accs {
		lbl := a.DisplayName
		if a.Login != "" {
			lbl = a.DisplayName + " (@" + a.Login + ")"
		}
		labelByID[a.ID] = lbl
	}
	relabel := func(ids []string) []string {
		out := make([]string, 0, len(ids))
		for _, id := range ids {
			if lbl, ok := labelByID[id]; ok {
				out = append(out, lbl)
			} else {
				out = append(out, id)
			}
		}
		return out
	}

	now := time.Now()
	endsIn := ""
	urgent := false
	if !dc.EndsAt.IsZero() {
		endsIn = formatEndsIn(dc.EndsAt.Sub(now))
		urgent = dc.EndsAt.Sub(now) < 24*time.Hour && dc.EndsAt.After(now)
	}
	startsAt := "—"
	if !dc.StartsAt.IsZero() {
		startsAt = dc.StartsAt.UTC().Format("2006-01-02 15:04 UTC")
	}
	endsAt := "—"
	if !dc.EndsAt.IsZero() {
		endsAt = dc.EndsAt.UTC().Format("2006-01-02 15:04 UTC")
	}

	benefits := make([]dashCampaignBenefit, 0, len(dc.Benefits))
	for _, b := range dc.Benefits {
		benefits = append(benefits, dashCampaignBenefit{
			ID:              b.ID,
			Name:            b.Name,
			RequiredMinutes: b.RequiredMinutes,
			ImageURL:        b.ImageURL,
		})
	}

	rawJSON, _ := json.MarshalIndent(dc.Campaign, "", "  ")

	detail := dashCampaignDetail{
		ID:               dc.ID,
		Name:             dc.Name,
		Platform:         dc.Platform,
		Game:             dc.Game,
		Status:           dc.Status,
		Kind:             dc.Kind,
		StartsAt:         startsAt,
		EndsAt:           endsAt,
		EndsIn:           endsIn,
		EndsUrgent:       urgent,
		Benefits:         benefits,
		EligibleAccounts: relabel(dc.EligibleAccounts),
		SourceAccounts:   relabel(dc.SourceAccounts),
		AccountLinked:    dc.AccountLinked,
		AccountLinkURL:   dc.AccountLinkURL,
		RawJSON:          string(rawJSON),
	}
	renderPartial(w, d.t, "dashboard_campaign_modal", detail)
}

// dashAccountDetail powers the per-account modal opened from
// Currently mining rows. Shows what the watcher is doing right now,
// the per-account whitelist + game priority, what campaigns the
// account is eligible for, and the latest log lines tagged with this
// account ID.
type dashAccountDetail struct {
	ID          string
	Platform    string
	Login       string
	DisplayName string
	Enabled     bool
	State       string // raw watcher state
	StateLabel  string // human label

	// Current activity (watching/claiming)
	CurrentCampaign  string
	CurrentGame      string
	CurrentBenefit   string
	CurrentChannel   string
	MinutesWatched  int
	RequiredMinutes int
	ProgressPct     int
	WatchETA        string
	Uptime          string

	// Whitelist / priority
	Games []dashAccountGameRow

	// What this account can mine right now from discovery
	EligibleCampaigns []dashAccountCampaignRow
	UpcomingCampaigns []dashAccountCampaignRow

	// Recent events tagged with this account
	RecentEvents []dashEvent
}

type dashAccountGameRow struct {
	Rank int
	Name string
}

type dashAccountCampaignRow struct {
	ID       string
	Name     string
	Game     string
	EndsIn   string
	StartsIn string // for upcoming only
}

func (d dashboardDeps) accountDetail(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		http.Error(w, "missing account id", http.StatusBadRequest)
		return
	}
	acc, err := d.q.GetAccount(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	// Pull watcher snapshot if present.
	var snap watcher.Snapshot
	for _, s := range d.sch.WatcherSnapshots() {
		if s.AccountID == id {
			snap = s
			break
		}
	}
	if snap.State == "" {
		snap.State = "stopped"
	}

	detail := dashAccountDetail{
		ID:              id,
		Platform:        acc.Platform,
		Login:           acc.Login,
		DisplayName:     acc.DisplayName,
		Enabled:         acc.Enabled == 1,
		State:           snap.State,
		StateLabel:      stateLabel(snap.State),
		CurrentCampaign: snap.CampaignName,
		CurrentGame:     snap.CampaignGame,
		CurrentBenefit:  snap.BenefitName,
		CurrentChannel:  snap.Channel,
		MinutesWatched:  snap.MinutesWatched,
		RequiredMinutes: snap.RequiredMinutes,
		ProgressPct:     pct(snap.MinutesWatched, snap.RequiredMinutes),
		WatchETA:        etaFrom(snap.MinutesWatched, snap.RequiredMinutes),
	}
	if !snap.StartedAt.IsZero() && snap.State == "watching" {
		detail.Uptime = formatShort(time.Since(snap.StartedAt))
	}

	// Whitelist / priority
	if games, err := d.q.ListAccountGames(r.Context(), id); err == nil {
		for _, g := range games {
			detail.Games = append(detail.Games, dashAccountGameRow{Rank: int(g.Rank) + 1, Name: g.Name})
		}
	}

	// Per-account campaign eligibility from discovery cache.
	now := time.Now()
	for _, dc := range d.sch.DiscoverySnapshot() {
		// Match by source/eligible account ID.
		var matches bool
		for _, aid := range dc.EligibleAccounts {
			if aid == id {
				matches = true
				break
			}
		}
		if !matches {
			continue
		}
		row := dashAccountCampaignRow{ID: dc.ID, Name: dc.Name, Game: dc.Game}
		if !dc.EndsAt.IsZero() {
			row.EndsIn = formatEndsIn(dc.EndsAt.Sub(now))
		}
		if !dc.StartsAt.IsZero() && dc.StartsAt.After(now) {
			row.StartsIn = formatEndsIn(dc.StartsAt.Sub(now))
			detail.UpcomingCampaigns = append(detail.UpcomingCampaigns, row)
		} else {
			detail.EligibleCampaigns = append(detail.EligibleCampaigns, row)
		}
	}

	// Filter events ring for this account.
	if d.ring != nil {
		all := eventsFromRing(d.ring, "", id, []gen.Account{acc})
		if len(all) > 20 {
			all = all[:20]
		}
		detail.RecentEvents = all
	}

	renderPartial(w, d.t, "dashboard_account_modal", detail)
}

func stateLabel(s string) string {
	switch s {
	case "watching":
		return "watching"
	case "claiming":
		return "claiming"
	case "pick_stream":
		return "scanning channels"
	case "pick_campaign", "idle":
		return "looking for work"
	case "sleeping":
		return "no eligible campaign"
	case "needs_auth":
		return "needs login"
	case "stopped":
		return "stopped"
	}
	return s
}

// nowPoll formats how long ago t was, for the "last poll" display.
func nowPoll(t time.Time) string {
	return fmt.Sprintf("%.1fs ago", time.Since(t).Seconds())
}
