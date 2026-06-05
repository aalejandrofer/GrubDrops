package twitch

import (
	"context"
	"fmt"
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
			Self           struct {
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
	if err := d.c.gql(ctx, sess.AccessToken, OpCampaigns, nil, &page); err != nil {
		return nil, fmt.Errorf("list campaigns: %w", err)
	}
	out := make([]platform.Campaign, 0, len(page.CurrentUser.DropCampaigns))
	for _, c := range page.CurrentUser.DropCampaigns {
		// Honor the per-account game whitelist BEFORE doing any work —
		// non-whitelisted campaigns must not appear ANYWHERE (mining,
		// /drops page, or persisted in the campaigns table). The
		// whitelist is the canonical filter.
		if sess.GameFilter != nil && !sess.GameFilter(c.Game.DisplayName) {
			continue
		}
		// Skip scrape-fallback noise: campaigns with no game name AND
		// dom-prefixed IDs are paragraphs the heading-anchored walk
		// grabbed instead of real cards. Don't poison the discovery
		// cache with them.
		if c.Game.DisplayName == "" && strings.HasPrefix(c.ID, "dom-") {
			continue
		}
		camp := platform.Campaign{
			ID:             c.ID,
			Platform:       "twitch",
			Game:           c.Game.DisplayName,
			Name:           c.Name,
			StartsAt:       parseISO(c.StartAtRaw),
			EndsAt:         parseISO(c.EndAtRaw),
			AccountLinked:  c.Self.IsAccountConnected,
			AccountLinkURL: c.AccountLinkURL,
		}
		switch c.Status {
		case "ACTIVE":
			camp.Status = "active"
			benefits, allowedLogins, err := d.fetchDetails(ctx, sess, c.ID)
			if err != nil {
				return nil, err
			}
			d.captureAllowed(c.ID, allowedLogins)
			camp.Benefits = benefits
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
	return out, nil
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
	if err := d.c.gql(ctx, sess.AccessToken, OpDropCampaignDetails,
		map[string]any{"dropID": campaignID, "channelLogin": ""}, &det); err != nil {
		return nil, nil, fmt.Errorf("campaign details %s: %w", campaignID, err)
	}
	benefits = make([]platform.DropBenefit, 0, len(det.User.DropCampaign.TimeBasedDrops))
	for _, td := range det.User.DropCampaign.TimeBasedDrops {
		for _, be := range td.BenefitEdges {
			benefits = append(benefits, platform.DropBenefit{
				ID:              td.ID, // drop id used for claiming, not benefit reward id
				CampaignID:      campaignID,
				Name:            be.Benefit.Name,
				RequiredMinutes: td.RequiredMinutesWatched,
				ImageURL:        be.Benefit.ImageAssetURL,
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
