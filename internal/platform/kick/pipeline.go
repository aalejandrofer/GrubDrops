package kick

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// DropProgress satisfies platform.DropProgressSource. Required minutes come
// from /drops/progress required_units whenever Kick lists the reward. Kick
// lists a reward only after it accrues, so an unlisted reward is reported
// with Known=false instead of guessing.
func (b *Backend) DropProgress(ctx context.Context, s platform.Session, camps []platform.Campaign) ([]platform.DropProgress, error) {
	rewards, err := b.api.progressDetail(ctx, s)
	if err != nil {
		return nil, fmt.Errorf("kick drop progress: %w", err)
	}
	byID := make(map[string]progressReward, len(rewards))
	for _, r := range rewards {
		byID[r.RewardID] = r
	}
	var out []platform.DropProgress
	for _, c := range camps {
		for _, bf := range c.Benefits {
			r, ok := byID[bf.ID]
			if !ok {
				out = append(out, platform.DropProgress{DropID: bf.ID, CampaignID: c.ID, Required: bf.RequiredMinutes})
				continue
			}
			req := r.Required
			// Kick's live payload always carries required_units; this fallback
			// only covers a malformed row. Reporting 0 would wrongly block the
			// drop as sub_only, so we use the campaign's RequiredMinutes instead.
			if req <= 0 {
				req = bf.RequiredMinutes
			}
			minutes := 0
			if req > 0 && r.Fraction > 0 {
				minutes = int(math.Round(r.Fraction * float64(req)))
			}
			out = append(out, platform.DropProgress{
				DropID: bf.ID, CampaignID: c.ID, Minutes: minutes, Required: req,
				Claimed: r.Claimed, Known: true,
			})
		}
	}
	return out, nil
}

// ClaimDrop satisfies platform.DropClaimer. A claim error on an already
// auto-granted reward shows up as ClaimFailed; the post-claim reconcile
// then sees claimed=true and corrects the state.
func (b *Backend) ClaimDrop(ctx context.Context, s platform.Session, d platform.DropProgress) platform.ClaimResult {
	err := b.api.Claim(ctx, s, d.DropID, d.CampaignID)
	if err == nil {
		return platform.ClaimResult{Outcome: platform.ClaimOK}
	}
	var linkErr *ClaimNeedsLinkError
	if errors.As(err, &linkErr) {
		return platform.ClaimResult{Outcome: platform.ClaimNeedsLink, LinkURL: linkErr.ConnectURL, Detail: err.Error()}
	}
	return platform.ClaimResult{Outcome: platform.ClaimFailed, Detail: err.Error()}
}

// ProbeChannels satisfies platform.ChannelProber on top of probeLive.
func (b *Backend) ProbeChannels(ctx context.Context, s platform.Session, c platform.Campaign, logins []string) ([]platform.Stream, error) {
	pool := make([]kickChannel, 0, len(logins))
	for _, l := range logins {
		if l = strings.ToLower(strings.TrimSpace(l)); l != "" {
			pool = append(pool, kickChannel{Slug: l})
		}
	}
	return b.probeLive(ctx, s, c, pool), nil
}

var (
	_ platform.DropProgressSource = (*Backend)(nil)
	_ platform.DropClaimer        = (*Backend)(nil)
	_ platform.ChannelProber      = (*Backend)(nil)
)
