package twitch

import (
	"context"
	"fmt"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

type claimer struct {
	c *client
}

type claimResult struct {
	ClaimDropRewards *struct {
		Status string `json:"status"`
	} `json:"claimDropRewards"`
}

func (cl *claimer) claim(ctx context.Context, sess platform.Session, b platform.DropBenefit, userID int64) error {
	status, err := cl.claimStatus(ctx, sess, b, userID)
	if err != nil {
		return err
	}
	switch status {
	case "ELIGIBLE_FOR_ALL", "DROP_INSTANCE_ALREADY_CLAIMED", "":
		return nil
	default:
		return fmt.Errorf("claim status: %s", status)
	}
}

// claimStatus sends the claim mutation and returns Twitch's raw status.
func (cl *claimer) claimStatus(ctx context.Context, sess platform.Session, b platform.DropBenefit, userID int64) (string, error) {
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
	if err := cl.c.gql(ctx, sess.AccessToken, OpClaimDrop,
		map[string]any{"input": map[string]any{"dropInstanceID": id}}, &out); err != nil {
		return "", fmt.Errorf("claim %s: %w", id, err)
	}
	if out.ClaimDropRewards == nil {
		return "", fmt.Errorf("claim %s: twitch returned no claim result (unknown or invalid drop instance)", id)
	}
	return out.ClaimDropRewards.Status, nil
}
