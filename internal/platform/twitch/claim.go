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

func (cl *claimer) claim(ctx context.Context, sess platform.Session, b platform.DropBenefit) error {
	// Prefer the per-account instance id captured at progress time;
	// fall back to the drop template id for backends that don't yet
	// surface InstanceID. Twitch typically rejects template-id claims
	// with INVALID_DROP_INSTANCE so the InstanceID path is the
	// load-bearing one.
	id := b.InstanceID
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
