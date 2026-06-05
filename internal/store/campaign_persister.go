package store

import (
	"context"
	"time"

	"github.com/aalejandrofer/rust-drops-miner/internal/platform"
	"github.com/aalejandrofer/rust-drops-miner/internal/store/gen"
)

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
		status := c.Status
		if status == "" {
			status = "active"
		}
		if err := p.Q.UpsertCampaign(ctx, gen.UpsertCampaignParams{
			ID:           c.ID,
			Platform:     c.Platform,
			Game:         c.Game,
			Name:         c.Name,
			StartsAt:     c.StartsAt.Unix(),
			EndsAt:       c.EndsAt.Unix(),
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
