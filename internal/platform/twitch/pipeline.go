package twitch

import (
	"context"
	"fmt"
	"strings"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// isSyntheticID reports scrape-fallback placeholder ids ("Game|Name").
// Twitch can't report progress for them.
func isSyntheticID(id string) bool { return strings.ContainsAny(id, "| ") }

// dropProgress reads DropCampaignDetails uncached, because self changes as
// the viewer watches and claims. One entry per drop id.
func (d *discovery) dropProgress(ctx context.Context, sess platform.Session, campaignID string) ([]platform.DropProgress, error) {
	channelLogin := d.userLogin
	if channelLogin == "" {
		channelLogin = "twitch"
	}
	var det campaignDetailsData
	if err := d.c.gql(ctx, sess.AccessToken, OpDropCampaignDetails,
		map[string]any{"dropID": campaignID, "channelLogin": channelLogin}, &det); err != nil {
		return nil, fmt.Errorf("drop progress %s: %w", campaignID, err)
	}
	seen := map[string]bool{}
	var out []platform.DropProgress
	for _, td := range det.User.DropCampaign.TimeBasedDrops {
		if td.ID == "" || seen[td.ID] {
			continue
		}
		seen[td.ID] = true
		dp := platform.DropProgress{DropID: td.ID, CampaignID: campaignID, Required: td.RequiredMinutesWatched, Known: true}
		if td.Self != nil {
			dp.Minutes = td.Self.CurrentMinutesWatched
			dp.Claimed = td.Self.IsClaimed
			dp.InstanceID = td.Self.DropInstanceID
		}
		out = append(out, dp)
	}
	return out, nil
}

// DropProgress satisfies platform.DropProgressSource. Per-campaign details
// are the truth (they survive the campaign leaving the in-progress
// inventory); the in-progress inventory only fills fresher minutes and the
// instance id.
func (b *Backend) DropProgress(ctx context.Context, s platform.Session, camps []platform.Campaign) ([]platform.DropProgress, error) {
	inv, err := b.disc.inventory(ctx, s)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]platform.Progress, len(inv))
	for _, p := range inv {
		byID[p.BenefitID] = p
	}
	var out []platform.DropProgress
	for _, c := range camps {
		if isSyntheticID(c.ID) {
			for _, bf := range c.Benefits {
				out = append(out, platform.DropProgress{DropID: bf.ID, CampaignID: c.ID, Known: true, Unmineable: true})
			}
			continue
		}
		dps, err := b.disc.dropProgress(ctx, s, c.ID)
		if err != nil {
			return nil, err
		}
		for i := range dps {
			p, ok := byID[dps[i].DropID]
			if !ok {
				continue
			}
			if p.MinutesWatched > dps[i].Minutes {
				dps[i].Minutes = p.MinutesWatched
			}
			dps[i].Claimed = dps[i].Claimed || p.Claimed
			if dps[i].InstanceID == "" {
				dps[i].InstanceID = p.InstanceID
			}
		}
		out = append(out, dps...)
	}
	return out, nil
}

// classifyClaimStatus maps Twitch's claimDropRewards.status to an outcome.
func classifyClaimStatus(status string) platform.ClaimResult {
	switch status {
	case "ELIGIBLE_FOR_ALL", "":
		return platform.ClaimResult{Outcome: platform.ClaimOK}
	case "DROP_INSTANCE_ALREADY_CLAIMED":
		return platform.ClaimResult{Outcome: platform.ClaimAlready}
	default:
		return platform.ClaimResult{Outcome: platform.ClaimFailed, Detail: "claim status: " + status}
	}
}

// ClaimDrop satisfies platform.DropClaimer. Unlinked campaigns are blocked
// before claiming (reconciler reads self.isAccountConnected), so Twitch has
// no link outcome here.
func (b *Backend) ClaimDrop(ctx context.Context, s platform.Session, d platform.DropProgress) platform.ClaimResult {
	userID, _ := b.watch.resolveUserID(ctx, s)
	status, err := b.claim.claimStatus(ctx, s, platform.DropBenefit{ID: d.DropID, CampaignID: d.CampaignID, InstanceID: d.InstanceID}, userID)
	if err != nil {
		return platform.ClaimResult{Outcome: platform.ClaimFailed, Detail: err.Error()}
	}
	return classifyClaimStatus(status)
}

// ProbeChannels satisfies platform.ChannelProber: parallel live check of the
// given logins, filtered to c.Game when set.
func (b *Backend) ProbeChannels(ctx context.Context, s platform.Session, c platform.Campaign, logins []string) ([]platform.Stream, error) {
	return b.chans.listEligible(ctx, s, c, logins)
}

var (
	_ platform.DropProgressSource = (*Backend)(nil)
	_ platform.DropClaimer        = (*Backend)(nil)
	_ platform.ChannelProber      = (*Backend)(nil)
)
