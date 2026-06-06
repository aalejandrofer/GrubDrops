package twitch

import (
	"context"
	"fmt"

	"github.com/aalejandrofer/dropsminer/internal/platform"
)

type claimer struct {
	c *client
}

type claimResult struct {
	ClaimDropRewards struct {
		Status string `json:"status"`
	} `json:"claimDropRewards"`
}

func (cl *claimer) claim(ctx context.Context, sess platform.Session, b platform.DropBenefit, userID int64) error {
	// Prefer the per-account instance id captured at progress time.
	// When it's missing, construct DevilXD's synthetic instance id
	// `userID#campaignID#dropID` (inventory.py generate_claim) — Twitch
	// accepts it and rejects the bare drop-template id with
	// INVALID_DROP_INSTANCE. Only fall back to the template id as a last
	// resort when we couldn't resolve the user id.
	id := b.InstanceID
	if id == "" && userID > 0 && b.CampaignID != "" && b.ID != "" {
		id = fmt.Sprintf("%d#%s#%s", userID, b.CampaignID, b.ID)
	}
	if id == "" {
		id = b.ID
	}
	var out claimResult
	err := cl.c.gql(ctx, sess.AccessToken, OpClaimDrop,
		map[string]any{"input": map[string]any{"dropInstanceID": id}}, &out)
	if err != nil {
		return fmt.Errorf("claim %s: %w", id, err)
	}
	switch out.ClaimDropRewards.Status {
	case "ELIGIBLE_FOR_ALL", "DROP_INSTANCE_ALREADY_CLAIMED", "":
		return nil
	default:
		return fmt.Errorf("claim status: %s", out.ClaimDropRewards.Status)
	}
}
