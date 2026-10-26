package kick

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// Catalog lists every active Kick drop campaign with its rewards, regardless
// of any game whitelist. It rides the utls transport, so no browser is
// needed; s may be an empty session. It has no side effects on mining state.
// RequiredMinutes is Kick's value as sent: 0 when the payload carries none.
func (b *Backend) Catalog(ctx context.Context, s platform.Session) ([]platform.Campaign, error) {
	if ks, err := decodeSession(s); err == nil && len(ks.Cookies) == 0 {
		// The campaign list is public: send it without cookies.
		ctx = withAnonymous(ctx)
	}
	camps, err := b.api.Campaigns(ctx, s)
	if err != nil {
		return nil, fmt.Errorf("kick campaigns: %w", err)
	}
	now := time.Now()
	out := make([]platform.Campaign, 0, len(camps))
	for _, c := range camps {
		camp := platform.Campaign{
			ID:             c.ID,
			Platform:       "kick",
			Game:           c.Game,
			BoxArtURL:      c.GameImage,
			Name:           c.Name,
			AccountLinkURL: c.ConnectURL,
		}
		if t, err := time.Parse(time.RFC3339, c.StartsAt); err == nil {
			camp.StartsAt = t.UTC()
		}
		if t, err := time.Parse(time.RFC3339, c.EndsAt); err == nil {
			camp.EndsAt = t.UTC()
		}
		switch {
		case strings.EqualFold(c.Status, "expired"), !camp.EndsAt.IsZero() && camp.EndsAt.Before(now):
			continue
		case !camp.StartsAt.IsZero() && camp.StartsAt.After(now):
			continue
		}
		camp.Status = "active"
		for _, r := range c.Rewards {
			camp.Benefits = append(camp.Benefits, platform.DropBenefit{
				ID:              r.ID,
				CampaignID:      c.ID,
				Name:            r.Name,
				RequiredMinutes: r.RequiredMinutes,
				ImageURL:        r.ImageURL,
			})
		}
		out = append(out, camp)
	}
	return out, nil
}

type anonKey struct{}

// withAnonymous marks ctx so the doer sends a request with no session
// cookies, for public endpoints only.
func withAnonymous(ctx context.Context) context.Context {
	return context.WithValue(ctx, anonKey{}, true)
}

func isAnonymous(ctx context.Context) bool {
	v, _ := ctx.Value(anonKey{}).(bool)
	return v
}
