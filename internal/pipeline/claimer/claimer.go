// Package claimer turns a claimable drop into claimed or blocked.
package claimer

import (
	"context"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/pipeline/dropstate"
	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// Attempt claims one drop and returns its next state. The caller must
// reconcile afterwards: a lost response shows up as ClaimFailed here and is
// corrected when the platform reports isClaimed.
func Attempt(ctx context.Context, c platform.DropClaimer, s platform.Session, row dropstate.Row, dp platform.DropProgress, now time.Time) (dropstate.Row, platform.ClaimResult) {
	res := c.ClaimDrop(ctx, s, dp)
	switch res.Outcome {
	case platform.ClaimOK, platform.ClaimAlready:
		return dropstate.ClaimOK(row, now), res
	case platform.ClaimNeedsLink:
		return dropstate.ClaimNeedsLink(row, now), res
	default:
		return dropstate.ClaimFailedAttempt(row, now), res
	}
}
