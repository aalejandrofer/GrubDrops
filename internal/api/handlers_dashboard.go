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

	mlog "github.com/aalejandrofer/grubdrops/internal/log"
	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/scheduler"
	"github.com/aalejandrofer/grubdrops/internal/store"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
	"github.com/aalejandrofer/grubdrops/internal/watcher"
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
	s     *store.Settings
	start time.Time
	// channelCounters is keyed by platform name ("twitch", "kick"). Nil
	// or missing entries make the dashboard fall back to zero for that
	// platform — safer than panicking when a backend isn't wired up.
	channelCounters map[string]ChannelCounter
	// kickPath reports an account's live Kick watch path ("ws"|"chrome").
	// Nil when no Kick backend is wired.
	kickPath func(accountID string) string
}

type dashTelemetry struct {
	WatchTimeTotal string // lifetime watch time, "h:m" (sum of progress minutes)
	ClaimsTotal    int    // lifetime drops claimed (claims table count)
	ClaimsToday    int    // drops claimed since start of today (local midnight)
	ActiveCamps    int    // whitelisted campaigns broadcasting right now
	Completed      int    // discovered drops already claimed/completed (X in "X / Y")
	TotalDrops     int    // total discovered drops across active campaigns (Y in "X / Y")
	NextClaimETA   string // "00:13 h:m" or "—"
	NextClaimName  string // "Wolf Helmet" or ""
}

type dashMineCard struct {
	ID             string
	Name           string
	Login          string
	AccountInitial string // first letter of display name, "?" fallback
	AvatarURL      string // resolved <img> src (direct for Twitch, proxied for Kick); "" -> letter circle
	Platform       string // "twitch" | "kick"
	WatchPath      string // kick only: live accrual path "ws" | "chrome" (per-account, reflects auto-mode fallback)
	State          string // "watching" | "claiming" | "pick_stream" | "sleeping" | "idle" | "stopped"
	StateSub       string // free-text aside
	Uptime         string // "17m on stream" or "—"
	LastPoll       string // "12s ago" — time since last inventory/progress poll
	Enabled        bool

	// Now-playing strip
	Channel        string
	ChannelInitial string
	ChannelGame    string
	ChannelViews   string // formatted, e.g. "62.4k" or "—"
	ChannelURL     string // direct watch link (platform-aware) or ""

	// Current drop
	DropName     string
	DropCampaign string
	DropImage    string // benefit icon URL (shown in the expanded row)
	DropMins     int
	DropReq      int
	DropPercent  int
	DropETA      string // "~01:13" or "—"

	// Queue
	Queue []dashQueueItem

	// Footer
	WatchToday  string // "watch 4h17m / 24h"
	ClaimsToday int

	// CSRFToken backs the inline per-account reload form on each row. The
	// row partial is rendered with just the card (.) as its data, so it has
	// no access to the page-level $.CSRFToken — the token has to ride on the
	// card itself.
	CSRFToken string
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
	// KickWatchMode is the configured Kick accrual path ("browser" | "ws"),
	// shown as a bubble next to the KICK column header so the operator can
	// see which path is active at a glance.
	KickWatchMode string
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

// dashEventAccount is fed into the per-account filter dropdown. The
// handler matches incoming `?account=` against ID; Platform drives the
// option's colour (twitch purple / kick green).
type dashEventAccount struct {
	ID       string
	Label    string
	Platform string
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
	NodeAddr      string             // e.g. "10.0.0.5"
	Uptime        string             // "17h 42m"
	Alerts        []dashAlert        // top-of-page CTA banner items
}

type dashAlert struct {
	Kind    string // "needs_auth" | "no_drops"
	Account string // display @login
	URL     string // direct CTA link
	Action  string // button label
}

func (d dashboardDeps) collectPage(r *http.Request) dashPage {
	accs, _ := d.q.ListAllAccounts(r.Context())
	snapshots := d.sch.WatcherSnapshots()
	snapByID := map[string]watcher.Snapshot{}
	for _, s := range snapshots {
		snapByID[s.AccountID] = s
	}

	// Persist watch progress so the lifetime "Watch time" tile
	// (SumWatchMinutes over the progress table) has a durable source.
	// The scheduler holds no store handle, so the dashboard poll is the
	// seam that has both live snapshots and the queries. Minutes only
	// grow, so overwriting with the current value is correct. Best-effort:
	// benefits that were never persisted (synth/scrape drops) fail the FK
	// and are skipped silently.
	persistedAt := time.Now().Unix()
	for _, s := range snapshots {
		if s.BenefitID == "" || s.MinutesWatched <= 0 {
			continue
		}
		_ = d.q.UpsertProgress(r.Context(), gen.UpsertProgressParams{
			AccountID:      s.AccountID,
			BenefitID:      s.BenefitID,
			MinutesWatched: int64(s.MinutesWatched),
			UpdatedAt:      persistedAt,
		})
	}

	// CSRF token for the inline per-account reload form on each row. Stamped
	// onto every card because the row partial only sees the card (.), not the
	// page-level $.CSRFToken.
	csrf := csrfToken(r)
	cards := make([]dashMineCard, 0, len(accs))
	for _, a := range accs {
		snap, ok := snapByID[a.ID]
		if !ok {
			snap = watcher.Snapshot{AccountID: a.ID, State: "stopped"}
		}
		c := mineCardFromSnap(a, snap)
		c.CSRFToken = csrf
		if c.Platform == "kick" && d.kickPath != nil {
			c.WatchPath = d.kickPath(a.ID)
		}
		cards = append(cards, c)
	}

	allowed := allowedLoginsFor(r, d.q, accs)
	// Build alerts: any account in needs_auth state or sleeping with 0
	// eligible drops gets a top banner pointing at the right CTA.
	var alerts []dashAlert
	for _, c := range cards {
		switch c.State {
		case "needs_auth":
			alerts = append(alerts, dashAlert{
				Kind: "needs_auth", Account: c.Name,
				URL: "/accounts/" + c.ID + "/login", Action: "Re-authenticate →",
			})
		case "sleeping":
			if c.Platform == "twitch" {
				alerts = append(alerts, dashAlert{
					Kind: "no_drops", Account: c.Name,
					URL: "/accounts/" + c.ID + "/login", Action: "Switch to device-code login →",
				})
			}
		case "awaiting_connect":
			alerts = append(alerts, dashAlert{
				Kind: "awaiting_connect", Account: c.Name,
				URL: "/drops", Action: "Connect account →",
			})
		case "no_games":
			alerts = append(alerts, dashAlert{
				Kind: "no_games", Account: c.Name,
				URL: "/accounts/" + c.ID, Action: "Add games →",
			})
		}
	}

	camps := activeCampsFromDiscovery(r.Context(), d.sch, d.channelCounters, d.q)

	mining := bucketMiningByPlatform(cards)
	if d.s != nil {
		mining.KickWatchMode, _ = d.s.KickWatchMode(r.Context())
	}

	page := dashPage{
		Tele:          telemetryWithClaims(telemetryFrom(snapshots), d.q, r.Context()),
		Alerts:        alerts,
		Mining:        mining,
		NextClaims:    nextClaimsFrom(cards),
		ActiveCamps:   camps,
		LiveChannels:  liveChannelsFor(accs, snapshots, allowed),
		Events:        eventsFromRing(d.ring, "", "", accs),
		EventAccounts: eventAccountsFrom(accs),
		EventFilter:   "all",
		UpdatedAt:     nowPoll(time.Now()),
		Uptime:        formatUptime(time.Since(d.start)),
	}
	// "X / Y collected": Y = total discovered drops across active
	// campaigns (one per benefit). X = drops COMPLETED, i.e. every account
	// connected/linked to the drop's campaign has claimed that benefit. A
	// drop only *some* accounts collected is not completed. Computed from
	// the discovery snapshot (benefit + eligible-account sets) joined with
	// the persistent link + claim tables.
	page.Tele.Completed, page.Tele.TotalDrops = completedByAllConnected(r.Context(), d.sch.DiscoverySnapshot(), d.q)
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

// completedDataSource is the subset of *gen.Queries that
// completedByAllConnected needs: the per-campaign account-link state
// (who is connected/linked) and the per-campaign claim rows (who
// collected which benefit). Declared as an interface so tests can inject
// a stub without a real sqlite database.
type completedDataSource interface {
	ListAccountLinksForCampaign(ctx context.Context, campaignID string) ([]gen.ListAccountLinksForCampaignRow, error)
	ListClaimsForCampaign(ctx context.Context, campaignID string) ([]gen.ListClaimsForCampaignRow, error)
}

// completedByAllConnected computes the "X / Y collected" tile.
//
//   - total (Y) = every discovered drop: the sum of len(Benefits) across
//     all discovered campaigns. Unchanged from the old behaviour.
//   - done (X)  = drops that are COMPLETED, meaning EVERY account
//     connected/linked to the drop's campaign has a claim for that
//     benefit. A drop only some connected accounts collected does NOT
//     count.
//
// The "connected accounts" set for a campaign is the accounts with
// linked=1 in account_campaign_links (the per-account link state the
// campaign persister maintains). When a campaign has no link rows
// recorded — common for platforms/campaigns that don't require an
// external-account link probe (e.g. Kick), or campaigns discovered before
// the link table was populated — we fall back to the campaign's
// EligibleAccounts (the accounts whose whitelist matched the game, i.e.
// the accounts that would mine + claim it). See the LIMITATION note below.
//
// One ListAccountLinksForCampaign + one ListClaimsForCampaign call per
// discovered campaign. Discovery snapshots are small (tens of campaigns),
// so this stays well within the per-tick budget; no per-benefit fan-out.
//
// LIMITATION: the link-table fallback to EligibleAccounts is an
// approximation — EligibleAccounts is "accounts allowed to mine" not
// "accounts that must be linked". For platforms with no link concept it's
// the best available signal (an account that mines + claims is, in effect,
// the connected account). If/when every campaign reliably records link
// rows, drop the fallback and use the link set alone.
func completedByAllConnected(ctx context.Context, snap []scheduler.DiscoveredCampaign, q completedDataSource) (done, total int) {
	for _, dc := range snap {
		total += len(dc.Benefits)
		if q == nil || len(dc.Benefits) == 0 {
			continue
		}

		// Connected = accounts with linked=1 for this campaign.
		connected := map[string]struct{}{}
		if links, err := q.ListAccountLinksForCampaign(ctx, dc.ID); err == nil {
			for _, l := range links {
				if l.Linked == 1 {
					connected[l.AccountID] = struct{}{}
				}
			}
		}
		// Fallback: no recorded link rows -> treat the eligible accounts as
		// the connected set (see LIMITATION above).
		if len(connected) == 0 {
			for _, aid := range dc.EligibleAccounts {
				connected[aid] = struct{}{}
			}
		}
		// No connected accounts at all -> none of this campaign's drops can
		// be "collected by all connected accounts". Skip (contributes 0).
		if len(connected) == 0 {
			continue
		}

		// claimers[benefitID] = set of accounts that claimed that benefit.
		claimers := map[string]map[string]struct{}{}
		if claims, err := q.ListClaimsForCampaign(ctx, dc.ID); err == nil {
			for _, c := range claims {
				if claimers[c.BenefitID] == nil {
					claimers[c.BenefitID] = map[string]struct{}{}
				}
				claimers[c.BenefitID][c.AccountID] = struct{}{}
			}
		}

		// A benefit is completed iff every connected account is in its
		// claimer set.
		for _, b := range dc.Benefits {
			got := claimers[b.ID]
			if got == nil {
				continue
			}
			all := true
			for aid := range connected {
				if _, ok := got[aid]; !ok {
					all = false
					break
				}
			}
			if all {
				done++
			}
		}
	}
	return done, total
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
		out = append(out, dashEventAccount{ID: a.ID, Label: a.DisplayName, Platform: a.Platform})
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
		Login:          a.DisplayName,
		AccountInitial: initial(a.DisplayName),
		AvatarURL:      avatarSrc(a.Platform, a.AvatarUrl),
		Platform:       a.Platform,
		State:          s.State,
		Enabled:        a.Enabled == 1,
	}
	switch s.State {
	case "watching":
		c.StateSub = "live"
		c.Uptime = formatShort(time.Since(s.StartedAt))
		if !s.LastPollAt.IsZero() {
			c.LastPoll = formatShort(time.Since(s.LastPollAt)) + " ago"
		}
		c.Channel = s.Channel
		c.ChannelInitial = initial(s.Channel)
		c.ChannelGame = s.CampaignGame
		c.ChannelViews = formatViews(s.ViewerCount)
		c.ChannelURL = channelURL(a.Platform, s.Channel)
		c.DropName = s.BenefitName
		c.DropImage = s.BenefitImage
		c.DropCampaign = s.CampaignName
		c.DropMins, c.DropReq = s.MinutesWatched, max1(s.RequiredMinutes)
		c.DropPercent = pct(s.MinutesWatched, s.RequiredMinutes)
		c.DropETA = etaFrom(s.MinutesWatched, s.RequiredMinutes)
	case "claiming":
		c.StateSub = "claim in flight"
		c.DropName = s.BenefitName
		c.DropImage = s.BenefitImage
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
	case "awaiting_connect":
		c.StateSub = "connect account to mine"
	case "needs_auth":
		c.StateSub = "login required"
	case "no_games":
		c.StateSub = "no games whitelisted"
	}
	c.WatchToday = "—"
	c.ClaimsToday = 0
	return c
}

// telemetryWithClaims layers the persistent-store counts (lifetime
// drops claimed, active-campaign count, lifetime watch time) onto a
// base telemetry struct already populated from the live watcher
// snapshots by telemetryFrom.
func telemetryWithClaims(base dashTelemetry, q *gen.Queries, ctx context.Context) dashTelemetry {
	// Lifetime drops claimed = every row in the persistent claims table.
	// (Reward-reaper claims that never reach the claims table aren't counted —
	// they only live in the ephemeral log ring.)
	if q != nil {
		if n, err := q.CountClaims(ctx); err == nil {
			base.ClaimsTotal = int(n)
		}
		// Drops claimed today = claims since local midnight.
		now := time.Now()
		midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		if n, err := q.CountClaimsSince(ctx, midnight.Unix()); err == nil {
			base.ClaimsToday = int(n)
		}
	}

	// ACTIVE CAMPAIGNS = whitelisted campaigns currently live (same set
	// /drops shows as "N · CURRENT"), not just the one the watcher is
	// actively mining. telemetryFrom only saw watcher snapshots (=1).
	if q != nil {
		now := time.Now().Unix()
		if rows, err := q.ListCurrentCampaigns(ctx, gen.ListCurrentCampaignsParams{
			StartsAt: now, EndsAt: now, Limit: 500,
		}); err == nil {
			allow, hasWhitelist := allowedGamesUnion(ctx, q)
			active := 0
			for _, c := range rows {
				if passesWhitelist(allow, hasWhitelist, c.Game) {
					active++
				}
			}
			base.ActiveCamps = active
		}
	}

	// NextPoll is derived in telemetryFrom from the watcher snapshots (it
	// has the per-account LastPollAt); the log ring no longer feeds
	// telemetry now that the poll cadence is locked to 60s and a
	// heartbeats/hr count is low-signal.

	// Lifetime watch time = sum of persistent per-benefit progress minutes.
	// Survives restarts (unlike the heartbeat ring) and doesn't zero out
	// when drops are claimed.
	if q != nil {
		if mins, err := q.SumWatchMinutes(ctx); err == nil && mins > 0 {
			base.WatchTimeTotal = formatHM(time.Duration(mins) * time.Minute)
		}
	}
	return base
}

func telemetryFrom(snaps []watcher.Snapshot) dashTelemetry {
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
		ActiveCamps:   distinctCampaigns(snaps),
		NextClaimName: nextName,
	}
	if nextETA >= 0 {
		tele.NextClaimETA = formatHM(nextETA)
	} else {
		tele.NextClaimETA = "—"
	}
	// WatchTimeTotal is filled in telemetryWithClaims from the persistent
	// progress table (lifetime sum). Default to "—" until that runs.
	tele.WatchTimeTotal = "—"
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

func (d dashboardDeps) telemetry(w http.ResponseWriter, r *http.Request) {
	// HTMX partial — refreshes the telemetry tiles so NEXT CLAIM (and the
	// live counts) track the watchers instead of freezing at page-load state.
	renderPartial(w, d.t, "dashboard_telemetry", d.collectPage(r).Tele)
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
		labelByID[a.ID] = a.DisplayName
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

	AccountInitial string // letter-circle fallback
	AvatarURL      string // resolved <img> src; "" -> letter circle

	// Current activity (watching/claiming)
	CurrentCampaign string
	CurrentGame     string
	CurrentBenefit  string
	CurrentChannel  string
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
		Login:           acc.DisplayName,
		DisplayName:     acc.DisplayName,
		Enabled:         acc.Enabled == 1,
		State:           snap.State,
		StateLabel:      stateLabel(snap.State),
		AccountInitial:  initial(acc.DisplayName),
		AvatarURL:       avatarSrc(acc.Platform, acc.AvatarUrl),
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
	case "awaiting_connect":
		return "awaiting connect"
	case "needs_auth":
		return "needs login"
	case "no_games":
		return "no games yet"
	case "stopped":
		return "stopped"
	}
	return s
}

// nowPoll formats how long ago t was, for the "last poll" display.
func nowPoll(t time.Time) string {
	return fmt.Sprintf("%.1fs ago", time.Since(t).Seconds())
}
