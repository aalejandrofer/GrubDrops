package store

import (
	"context"
	"strings"
	"time"

	"github.com/aalejandrofer/dropsminer/internal/platform"
	"github.com/aalejandrofer/dropsminer/internal/store/gen"
)

// gameIDFromName turns a Twitch/Kick display name into a stable
// game_id used as the games.id primary key. "Minecraft" -> "g_minecraft",
// "Apex Legends" -> "g_apex_legends". Mirrors slugify() in twitch
// backend, prefixed with g_ so it can't collide with user-entered IDs.
func gameIDFromName(name string) string {
	out := make([]byte, 0, len(name)+2)
	out = append(out, 'g', '_')
	prev := byte(0)
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'A' && c <= 'Z':
			c += 32
		case c == ' ', c == '-', c == '_':
			c = '_'
		case (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9'):
			// keep
		default:
			continue
		}
		if c == '_' && prev == '_' {
			continue
		}
		out = append(out, c)
		prev = c
	}
	for len(out) > 2 && out[len(out)-1] == '_' {
		out = out[:len(out)-1]
	}
	return string(out)
}

// slugFromName is the Twitch directory slug — same as gameIDFromName
// minus the g_ prefix, dashes instead of underscores.
func slugFromName(name string) string {
	id := gameIDFromName(name)
	if strings.HasPrefix(id, "g_") {
		id = id[2:]
	}
	return strings.ReplaceAll(id, "_", "-")
}

// CampaignPersister upserts every Campaign + Benefit the watcher discovers
// into the local DB so the /drops page can render past + current +
// upcoming tabs even before anything has been claimed.
//
// Non-whitelisted campaigns must NEVER reach this type — the watcher's
// whitelist filter runs first. We do not re-apply the whitelist here so
// the source of truth stays in one place (the account_games table, read
// at watcher construction time).
type CampaignPersister struct {
	Q *gen.Queries
}

// NewCampaignPersister returns a persister backed by q.
func NewCampaignPersister(q *gen.Queries) *CampaignPersister {
	return &CampaignPersister{Q: q}
}

// PersistCampaigns upserts campaigns and their benefits. Status strings
// are normalised: the upsert preserves whatever the backend returned
// (e.g. "active", "expired", "upcoming"). starts_at/ends_at are stored
// as Unix-epoch seconds, matching the campaigns table column type.
func (p *CampaignPersister) PersistCampaigns(ctx context.Context, camps []platform.Campaign) error {
	if p == nil || p.Q == nil || len(camps) == 0 {
		return nil
	}
	now := time.Now().Unix()
	for _, c := range camps {
		if c.ID == "" {
			continue
		}
		// Auto-upsert the game so it shows in the account whitelist
		// picker even if not in the migration seed. Idempotent —
		// existing rows keep their priority.
		if c.Game != "" {
			_ = p.Q.UpsertGame(ctx, gen.UpsertGameParams{
				ID:       gameIDFromName(c.Game),
				Name:     c.Game,
				Slug:     slugFromName(c.Game),
				Priority: 100,
			})
		}
		status := c.Status
		if status == "" {
			status = "active"
		}
		// Default zero timestamps to plausible bounds so /drops's
		// past/current/upcoming filter doesn't classify scraped
		// campaigns as expired. Scrape supplies neither start nor
		// end — best-effort: start=now, end=now+30d.
		startsAt := c.StartsAt.Unix()
		endsAt := c.EndsAt.Unix()
		if c.StartsAt.IsZero() {
			startsAt = now
		}
		if c.EndsAt.IsZero() {
			endsAt = now + 30*24*3600
		}
		if err := p.Q.UpsertCampaign(ctx, gen.UpsertCampaignParams{
			ID:           c.ID,
			Platform:     c.Platform,
			Game:         c.Game,
			Name:         c.Name,
			StartsAt:     startsAt,
			EndsAt:       endsAt,
			Status:       status,
			RawJson:      "{}",
			DiscoveredAt: now,
		}); err != nil {
			return err
		}
		for _, b := range c.Benefits {
			if b.ID == "" {
				continue
			}
			if err := p.Q.UpsertBenefit(ctx, gen.UpsertBenefitParams{
				ID:              b.ID,
				CampaignID:      c.ID,
				Name:            b.Name,
				RequiredMinutes: int64(b.RequiredMinutes),
				ImageUrl:        b.ImageURL,
			}); err != nil {
				return err
			}
		}
	}
	return nil
}
