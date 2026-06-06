package twitch

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/aalejandrofer/dropsminer/internal/platform"
)

// discovery wraps the low-level client and implements campaign + inventory queries.
type discovery struct {
	c  *client
	mu sync.Mutex
	// pendingAllowed accumulates per-campaign allow-lists as fetchDetails
	// runs. Backend drains this via drainAllowed() after listActive returns.
	pendingAllowed map[string][]string
	// userLogin caches the authenticated user's twitch login. The
	// DropCampaignDetails persisted query needs a non-empty
	// channelLogin variable — passing "" makes the resolver return
	// data.user:null, which silently kills Benefits population and
	// makes the watcher fall back to synth-only campaigns. Cached
	// once per session lifetime.
	userLogin string
}

// campaignsData decodes the ViewerDropsDashboard (OpCampaigns) response.
// DevilXD path: response["data"]["currentUser"]["dropCampaigns"]
// Source: twitch.py GQL_QUERIES["Campaigns"] + DropsCampaign.__init__
type campaignsData struct {
	CurrentUser struct {
		DropCampaigns []struct {
			ID     string `json:"id"`
			Name   string `json:"name"`
			Status string `json:"status"` // "ACTIVE" | "UPCOMING" | "EXPIRED"
			// startAt/endAt come as ISO-8601 strings from gql but the
			// scrape fallback may emit empty strings. Use string +
			// best-effort parse so empties don't crash unmarshal.
			StartAtRaw     string `json:"startAt"`
			EndAtRaw       string `json:"endAt"`
			AccountLinkURL string `json:"accountLinkURL"`
			// Kind is a sidecar-only synthetic field ("__kind") added
			// by buildViewerDropsDashboardEnvelope when scrape detects
			// REWARD-only campaigns (no watch-time). Real Twitch gql
			// never sets this. Empty -> treated as "drop".
			Kind string `json:"__kind"`
			Self struct {
				IsAccountConnected bool `json:"isAccountConnected"`
			} `json:"self"`
			Game struct {
				ID          string `json:"id"`
				DisplayName string `json:"displayName"`
			} `json:"game"`
		} `json:"dropCampaigns"`
	} `json:"currentUser"`
}

// parseISO is the lenient timestamp parser used by listActive. Returns
// the zero Time for empty/garbage input — the watcher tolerates it.
func parseISO(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	if t, err := time.Parse("2006-01-02T15:04:05Z", s); err == nil {
		return t
	}
	return time.Time{}
}

// campaignDetailsData decodes the DropCampaignDetails (OpDropCampaignDetails) response.
// DevilXD path: response["data"]["user"]["dropCampaign"]["timeBasedDrops"]
// Benefit image field is "imageAssetURL" (flat string, NOT a nested image object).
// Source: inventory.py TimedDrop.__init__ + Benefit.__init__
//
// Allow-list path (verified from DevilXD channel.py Channel.from_acl):
//   response["data"]["user"]["dropCampaign"]["allow"]["isEnabled"] bool
//   response["data"]["user"]["dropCampaign"]["allow"]["channels"][]["id"]   string
//   response["data"]["user"]["dropCampaign"]["allow"]["channels"][]["name"] string  ← used as login
// If isEnabled==false (or allow.channels is empty), all channels streaming the
// game qualify (no restriction). We conservatively return an empty allow-list
// in that case — a future revision can fan out to "top channels for game".
type campaignDetailsData struct {
	User struct {
		DropCampaign struct {
			ID    string `json:"id"`
			Name  string `json:"name"`
			Allow struct {
				IsEnabled bool `json:"isEnabled"`
				Channels  []struct {
					ID   string `json:"id"`
					Name string `json:"name"` // DevilXD reads this as login (Channel.from_acl: login=data["name"])
				} `json:"channels"`
			} `json:"allow"`
			TimeBasedDrops []struct {
				ID                     string `json:"id"`
				Name                   string `json:"name"`
				RequiredMinutesWatched int    `json:"requiredMinutesWatched"`
				BenefitEdges           []struct {
					Benefit struct {
						ID            string `json:"id"`
						Name          string `json:"name"`
						ImageAssetURL string `json:"imageAssetURL"` // flat URL, not nested image.url1x
					} `json:"benefit"`
				} `json:"benefitEdges"`
				// preconditionDrops gate this drop behind earlier drops in
				// a chain (DevilXD TimedDrop preconditions). Usually empty.
				PreconditionDrops []struct {
					ID string `json:"id"`
				} `json:"preconditionDrops"`
			} `json:"timeBasedDrops"`
		} `json:"dropCampaign"`
	} `json:"user"`
}

// inventoryData decodes the Inventory (OpInventory) response.
// DevilXD path: response["data"]["currentUser"]["inventory"]["dropCampaignsInProgress"]
// Progress lives under timeBasedDrops[].self.{currentMinutesWatched, isClaimed, dropInstanceID}.
// Source: twitch.py GQL_QUERIES["Inventory"] + inventory.py TimedDrop.__init__ self-path
type inventoryData struct {
	CurrentUser struct {
		Inventory struct {
			DropCampaignsInProgress []struct {
				ID             string `json:"id"`
				TimeBasedDrops []struct {
					ID   string `json:"id"`
					Self struct {
						CurrentMinutesWatched int    `json:"currentMinutesWatched"`
						IsClaimed             bool   `json:"isClaimed"`
						DropInstanceID        string `json:"dropInstanceID"`
					} `json:"self"`
				} `json:"timeBasedDrops"`
			} `json:"dropCampaignsInProgress"`
		} `json:"inventory"`
	} `json:"currentUser"`
}

// listActive returns ALL campaigns the user can see — ACTIVE, EXPIRED, and
// UPCOMING — with their drop benefits (benefits are only fetched for ACTIVE
// campaigns to save bandwidth; EXPIRED / UPCOMING are emitted with empty
// benefit lists so the /drops page can show past + upcoming tabs filtered
// by the whitelist). The Status field is lower-cased — "active", "expired",
// or "upcoming" — matching the values persisted in the campaigns table.
// The whitelist is ALWAYS applied: non-whitelisted campaigns are dropped
// regardless of status.
//
// As a side effect listActive calls captureAllowed for ACTIVE campaigns so
// that the caller (Backend) can drain the allow-lists via drainAllowed()
// and store them in its own cache. The function name is kept for backward
// compatibility with the watcher/mining flow; mining-side callers must
// filter Status == "active" before attempting to mine.
func (d *discovery) listActive(ctx context.Context, sess platform.Session) ([]platform.Campaign, error) {
	var page campaignsData
	// fetchRewardCampaigns: false matches DevilXD/TwitchDropsMiner's
	// invocation of the ViewerDropsDashboard persisted query. Passing
	// nil variables works too but may take a different server-side
	// branch; matching DevilXD exactly removes one unknown.
	vars := map[string]any{"fetchRewardCampaigns": false}
	if err := d.c.gql(ctx, sess.AccessToken, OpCampaigns, vars, &page); err != nil {
		return nil, fmt.Errorf("list campaigns: %w", err)
	}
	// Resolve + cache the authenticated user's login. fetchDetails
	// passes this as channelLogin — empty value makes Twitch's
	// resolver return data.user:null, which strips TimeBasedDrops and
	// leaves real campaigns with Benefits=[] (so the watcher falls
	// back to scrape-synth entries). One CurrentUser call per
	// session lifetime.
	if d.userLogin == "" {
		if login, err := d.resolveCurrentLogin(ctx, sess); err == nil {
			d.userLogin = login
		}
	}
	out := make([]platform.Campaign, 0, len(page.CurrentUser.DropCampaigns))
	for _, c := range page.CurrentUser.DropCampaigns {
		// GameFilter is now a "should-fetch-details" gate, not a
		// drop-the-campaign gate. Non-whitelisted campaigns are still
		// emitted (status + game + name) so the /drops Discoverable
		// tab can surface them — but we skip the per-campaign detail
		// + allow-list fetches to keep bandwidth bounded by whitelist
		// size. The watcher applies the whitelist again before mining,
		// so emitting a non-whitelisted campaign here is safe.
		shouldFetchDetails := sess.GameFilter == nil || sess.GameFilter(c.Game.DisplayName)
		// Skip scrape-fallback noise: campaigns with no game name AND
		// dom-prefixed IDs are paragraphs the heading-anchored walk
		// grabbed instead of real cards. Don't poison the discovery
		// cache with them.
		if c.Game.DisplayName == "" && strings.HasPrefix(c.ID, "dom-") {
			continue
		}
		kind := c.Kind
		if kind == "" {
			kind = "drop"
		}
		// Real gql calls return self.isAccountConnected; scrape-only
		// entries (id contains "|" or " ") have it set optimistically.
		linkChecked := !strings.ContainsAny(c.ID, "| ")
		camp := platform.Campaign{
			ID:                 c.ID,
			Platform:           "twitch",
			Game:               c.Game.DisplayName,
			Name:               c.Name,
			StartsAt:           parseISO(c.StartAtRaw),
			EndsAt:             parseISO(c.EndAtRaw),
			AccountLinked:      c.Self.IsAccountConnected,
			AccountLinkChecked: linkChecked,
			AccountLinkURL:     c.AccountLinkURL,
			Kind:               kind,
		}
		switch c.Status {
		case "ACTIVE":
			camp.Status = "active"
			// Non-whitelisted: emit shell row only (no detail fetch,
			// no synth benefit). The /drops Discoverable tab uses this
			// to advertise opt-in candidates. Watcher won't mine it
			// because Benefits is empty.
			if !shouldFetchDetails {
				break
			}
			// Skip detail fetch for scrape-synthesised IDs — those
			// aren't real Twitch UUIDs and OpDropCampaignDetails will
			// fail. Synth a single default benefit per scrape-only
			// campaign so the watcher's pickCampaign loop advances
			// (it skips campaigns with len(Benefits)==0). Twitch
			// credits minutes against the enrolled real drop in the
			// user's account; our benefit row is a local placeholder
			// for state tracking.
			if strings.ContainsAny(c.ID, "| ") {
				// e.g. "Minecraft|Builder Cape..." — scraped
				if kind != "reward" {
					camp.Benefits = []platform.DropBenefit{{
						ID:              c.ID + "_default",
						CampaignID:      c.ID,
						Name:            c.Name,
						RequiredMinutes: 5,
					}}
				}
			} else {
				benefits, allowedLogins, err := d.fetchDetails(ctx, sess, c.ID)
				if err != nil {
					return nil, err
				}
				d.captureAllowed(c.ID, allowedLogins)
				camp.Benefits = benefits
				camp.AllowedChannelCount = len(allowedLogins)
			}
		case "UPCOMING":
			camp.Status = "upcoming"
		case "EXPIRED":
			camp.Status = "expired"
		default:
			// Unknown status — record it lower-cased and continue.
			camp.Status = strings.ToLower(c.Status)
		}
		out = append(out, camp)
	}
	return dedupeSynthVsReal(out), nil
}

// dedupeSynthVsReal drops scrape-synthesised campaign entries (IDs that
// contain "|" or " " — fabricated by the sidecar's scrape-fallback merge)
// when a real gql-sourced entry exists for the same game. Real UUID
// campaigns are authoritative: they have valid drop UUIDs that the
// inventory query can match, allowing the watcher to track progress and
// claim. Synth entries have fabricated IDs that never appear in
// dropCampaignsInProgress, so picking them strands the watcher at 0/N
// minutes forever (B2).
//
// When NO real entry exists for a game (e.g. integrity-wall blocked gql
// and only scrape data survived), synth entries are kept so the
// dashboard can still surface them — but the watcher's pick loop will
// still skip them via the AccountLinked=false / ID-shape heuristics.
func dedupeSynthVsReal(camps []platform.Campaign) []platform.Campaign {
	realGames := map[string]bool{}
	for _, c := range camps {
		if c.Platform != "twitch" {
			continue
		}
		if !strings.ContainsAny(c.ID, "| ") {
			realGames[c.Game] = true
		}
	}
	out := make([]platform.Campaign, 0, len(camps))
	for _, c := range camps {
		if c.Platform == "twitch" && strings.ContainsAny(c.ID, "| ") && realGames[c.Game] {
			continue
		}
		out = append(out, c)
	}
	return out
}

// fetchDetails calls OpDropCampaignDetails and converts the response into
// a slice of platform.DropBenefit values and the per-campaign allow-list.
//
// Note on BenefitID: claims are issued against the drop's id (TimedDrop.id),
// not the underlying reward benefit id — matching DevilXD's claim flow.
//
// allowedLogins is nil when allow.isEnabled==false (meaning any channel
// streaming the game qualifies). A future revision should fan out to the
// "top channels for game" query in that case. For now we conservatively
// return nil, which causes ListEligibleChannels to return no streams.
func (d *discovery) fetchDetails(ctx context.Context, sess platform.Session, campaignID string) (benefits []platform.DropBenefit, allowedLogins []string, err error) {
	var det campaignDetailsData
	// channelLogin must be non-empty for the resolver to return the
	// user object. Use the authenticated user's login (cached from
	// CurrentUser); fall back to a generic value if resolution
	// hasn't happened yet — anything is better than "".
	channelLogin := d.userLogin
	if channelLogin == "" {
		channelLogin = "twitch"
	}
	if err := d.c.gql(ctx, sess.AccessToken, OpDropCampaignDetails,
		map[string]any{"dropID": campaignID, "channelLogin": channelLogin}, &det); err != nil {
		return nil, nil, fmt.Errorf("campaign details %s: %w", campaignID, err)
	}
	benefits = make([]platform.DropBenefit, 0, len(det.User.DropCampaign.TimeBasedDrops))
	for _, td := range det.User.DropCampaign.TimeBasedDrops {
		// Watch-time drops only. DevilXD mines a drop only when
		// requiredMinutesWatched > 0 (inventory.py _base_earn_conditions).
		// Drops with 0 required minutes are sub/gift/purchase/event-gated
		// rewards we can't earn by watching (e.g. the LoL "1 Sub or Gift
		// Sub" drop) — skip them so the watcher never picks one.
		if td.RequiredMinutesWatched <= 0 {
			slog.Info("twitch skipping non-watch drop (0 required minutes)",
				"campaign", campaignID, "drop", td.ID)
			continue
		}
		var preconds []string
		for _, pc := range td.PreconditionDrops {
			if pc.ID != "" {
				preconds = append(preconds, pc.ID)
			}
		}
		for _, be := range td.BenefitEdges {
			benefits = append(benefits, platform.DropBenefit{
				ID:              td.ID, // drop id used for claiming, not benefit reward id
				CampaignID:      campaignID,
				Name:            be.Benefit.Name,
				RequiredMinutes: td.RequiredMinutesWatched,
				ImageURL:        be.Benefit.ImageAssetURL,
				Preconditions:   preconds,
			})
		}
	}
	if det.User.DropCampaign.Allow.IsEnabled {
		for _, ch := range det.User.DropCampaign.Allow.Channels {
			allowedLogins = append(allowedLogins, ch.Name)
		}
	}
	return benefits, allowedLogins, nil
}

// resolveCurrentLogin fetches the authenticated user's twitch login
// via a single CurrentUser ad-hoc gql query. Only used to populate
// the channelLogin arg of DropCampaignDetails — passing "" makes
// Twitch's resolver return data.user:null which silently strips
// benefits from real campaigns.
func (d *discovery) resolveCurrentLogin(ctx context.Context, sess platform.Session) (string, error) {
	const q = `query CurrentUser { currentUser { login } }`
	var resp struct {
		CurrentUser struct {
			Login string `json:"login"`
		} `json:"currentUser"`
	}
	if err := d.c.gqlQuery(ctx, sess.AccessToken, "CurrentUser", q, nil, &resp); err != nil {
		return "", err
	}
	if resp.CurrentUser.Login == "" {
		return "", fmt.Errorf("currentUser.login empty")
	}
	return resp.CurrentUser.Login, nil
}

// captureAllowed stores per-campaign allow-lists as a side-effect of
// fetchDetails so Backend can drain them after listActive completes.
func (d *discovery) captureAllowed(campaignID string, logins []string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.pendingAllowed == nil {
		d.pendingAllowed = map[string][]string{}
	}
	d.pendingAllowed[campaignID] = logins
}

// drainAllowed returns and clears the pending allow-list map. Safe for
// concurrent use; intended to be called exactly once per listActive call.
func (d *discovery) drainAllowed() map[string][]string {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := d.pendingAllowed
	d.pendingAllowed = map[string][]string{}
	return out
}

// inventory returns the current watch progress for all in-progress drop campaigns.
func (d *discovery) inventory(ctx context.Context, sess platform.Session) ([]platform.Progress, error) {
	var inv inventoryData
	if err := d.c.gql(ctx, sess.AccessToken, OpInventory, nil, &inv); err != nil {
		return nil, fmt.Errorf("inventory: %w", err)
	}
	out := []platform.Progress{}
	for _, camp := range inv.CurrentUser.Inventory.DropCampaignsInProgress {
		for _, td := range camp.TimeBasedDrops {
			out = append(out, platform.Progress{
				BenefitID:      td.ID,
				MinutesWatched: td.Self.CurrentMinutesWatched,
				Claimed:        td.Self.IsClaimed,
				InstanceID:     td.Self.DropInstanceID,
			})
		}
	}
	return out, nil
}
