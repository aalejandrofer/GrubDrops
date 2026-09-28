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

// requiredMinutes maps a drop's watch requirement to the app-wide marker:
// sub-gated drops (requiredSubs > 0) are not watch-earnable whatever minutes
// Twitch lists, so they report 0 (issue #47, same rule as fetchDetails).
func requiredMinutes(minutes, subs int) int {
	if subs > 0 {
		return 0
	}
	return minutes
}

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
		// self:null means Twitch said nothing about this viewer, which does
		// not prove "not enrolled"; only a self object is a definite answer.
		dp := platform.DropProgress{DropID: td.ID, CampaignID: campaignID, Required: requiredMinutes(td.RequiredMinutesWatched, td.RequiredSubs), Known: td.Self != nil}
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
// inventory); the in-progress inventory fills fresher minutes and the
// instance id, and a claimed=true from either source wins — isClaimed is
// a positive statement from the same self object and never reverts on
// Twitch. TV-client sessions read Inventory only (see tvDropProgress).
func (b *Backend) DropProgress(ctx context.Context, s platform.Session, camps []platform.Campaign) ([]platform.DropProgress, error) {
	b.c.bind(s)
	if s.ClientID == ClientTV {
		return b.tvDropProgress(ctx, s, camps)
	}
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
			dps[i].Known = true // present in the in-progress inventory
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

// tvDropProgress serves TV-client sessions from Inventory alone: Twitch
// returns dropCampaign:null for DropCampaignDetails on TV tokens (#48), so
// the details call is never made. Drops missing from Inventory get no entry,
// which the reconciler treats as unknown.
func (b *Backend) tvDropProgress(ctx context.Context, s platform.Session, camps []platform.Campaign) ([]platform.DropProgress, error) {
	byCamp, err := b.disc.inventoryDropProgress(ctx, s)
	if err != nil {
		return nil, err
	}
	var out []platform.DropProgress
	for _, c := range camps {
		if isSyntheticID(c.ID) {
			for _, bf := range c.Benefits {
				out = append(out, platform.DropProgress{DropID: bf.ID, CampaignID: c.ID, Known: true, Unmineable: true})
			}
			continue
		}
		out = append(out, byCamp[c.ID]...)
	}
	return out, nil
}

// classifyClaimStatus maps Twitch's claimDropRewards.status to an outcome.
// An empty status is a failure: v2 sends blind stall claim-probes, and
// reading "" as OK would permanently mark an unenrolled drop claimed. A
// real claim that returned "" is corrected by the post-claim reconcile.
// (v1's claim() in claim.go keeps its own mapping.)
func classifyClaimStatus(status string) platform.ClaimResult {
	switch status {
	case "":
		return platform.ClaimResult{Outcome: platform.ClaimFailed, Detail: "empty claim status"}
	case "ELIGIBLE_FOR_ALL":
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
	b.c.bind(s)
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
	b.c.bind(s)
	return b.chans.listEligible(ctx, s, c, logins)
}

var (
	_ platform.DropProgressSource = (*Backend)(nil)
	_ platform.DropClaimer        = (*Backend)(nil)
	_ platform.ChannelProber      = (*Backend)(nil)
)
