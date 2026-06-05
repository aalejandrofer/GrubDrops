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
	var out claimResult
	err := cl.c.gql(ctx, sess.AccessToken, OpClaimDrop,
		map[string]any{"input": map[string]any{"dropInstanceID": b.ID}}, &out)
	if err != nil {
		return fmt.Errorf("claim %s: %w", b.ID, err)
	}
	switch out.ClaimDropRewards.Status {
	case "ELIGIBLE_FOR_ALL", "DROP_INSTANCE_ALREADY_CLAIMED", "":
		return nil
	default:
		return fmt.Errorf("claim status: %s", out.ClaimDropRewards.Status)
	}
}
