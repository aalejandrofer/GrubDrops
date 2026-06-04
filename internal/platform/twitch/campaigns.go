package twitch

import (
	"context"
	"fmt"
	"time"

	"github.com/chano-fernandez/rust-drops-miner/internal/platform"
)

// discovery wraps the low-level client and implements campaign + inventory queries.
type discovery struct {
	c *client
}

// campaignsData decodes the ViewerDropsDashboard (OpCampaigns) response.
// DevilXD path: response["data"]["currentUser"]["dropCampaigns"]
// Source: twitch.py GQL_QUERIES["Campaigns"] + DropsCampaign.__init__
type campaignsData struct {
	CurrentUser struct {
		DropCampaigns []struct {
			ID      string    `json:"id"`
			Name    string    `json:"name"`
			Status  string    `json:"status"` // "ACTIVE" | "UPCOMING" | "EXPIRED"
			StartAt time.Time `json:"startAt"`
			EndAt   time.Time `json:"endAt"`
			Game    struct {
				ID          string `json:"id"`
				DisplayName string `json:"displayName"`
			} `json:"game"`
		} `json:"dropCampaigns"`
	} `json:"currentUser"`
}

// campaignDetailsData decodes the DropCampaignDetails (OpDropCampaignDetails) response.
// DevilXD path: response["data"]["user"]["dropCampaign"]["timeBasedDrops"]
// Benefit image field is "imageAssetURL" (flat string, NOT a nested image object).
// Source: inventory.py TimedDrop.__init__ + Benefit.__init__
type campaignDetailsData struct {
	User struct {
		DropCampaign struct {
			ID             string `json:"id"`
			Name           string `json:"name"`
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

// listActive returns all ACTIVE campaigns with their drop benefits.
// EXPIRED and UPCOMING campaigns are discarded.
func (d *discovery) listActive(ctx context.Context, sess platform.Session) ([]platform.Campaign, error) {
	var page campaignsData
	if err := d.c.gql(ctx, sess.AccessToken, OpCampaigns, nil, &page); err != nil {
		return nil, fmt.Errorf("list campaigns: %w", err)
	}
	out := make([]platform.Campaign, 0, len(page.CurrentUser.DropCampaigns))
	for _, c := range page.CurrentUser.DropCampaigns {
		if c.Status != "ACTIVE" {
			continue
		}
		benefits, err := d.fetchDetails(ctx, sess, c.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, platform.Campaign{
			ID:       c.ID,
			Platform: "twitch",
			Game:     c.Game.DisplayName,
			Name:     c.Name,
			Status:   "active",
			StartsAt: c.StartAt,
			EndsAt:   c.EndAt,
			Benefits: benefits,
		})
	}
	return out, nil
}

// fetchDetails calls OpDropCampaignDetails and converts the response into
// a slice of platform.DropBenefit values.
//
// Note on BenefitID: claims are issued against the drop's id (TimedDrop.id),
// not the underlying reward benefit id — matching DevilXD's claim flow.
func (d *discovery) fetchDetails(ctx context.Context, sess platform.Session, campaignID string) ([]platform.DropBenefit, error) {
	var det campaignDetailsData
	if err := d.c.gql(ctx, sess.AccessToken, OpDropCampaignDetails,
		map[string]any{"dropID": campaignID, "channelLogin": ""}, &det); err != nil {
		return nil, fmt.Errorf("campaign details %s: %w", campaignID, err)
	}
	benefits := make([]platform.DropBenefit, 0, len(det.User.DropCampaign.TimeBasedDrops))
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
	return benefits, nil
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
			})
		}
	}
	return out, nil
}
