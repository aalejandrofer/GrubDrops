package store

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/gameslug"
	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

// LinkOverridePrefix namespaces the manual "I've linked it" overrides in
// the kv table. A key `link_override:<campaignID>` with value "1" means the
// user asserted the campaign's external account is connected, so the
// watcher should attempt to mine it despite the backend reporting unlinked.
const LinkOverridePrefix = "link_override:"

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
				ID:       gameslug.ID(c.Game),
				Name:     c.Game,
				Slug:     gameslug.Slug(c.Game),
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
		kind := c.Kind
		if kind == "" {
			kind = "drop"
		}
		// account_linked: 1 unless the link status was checked and came back
		// false (whitelisted but the required external account isn't linked).
		linked := int64(1)
		if c.AccountLinkChecked && !c.AccountLinked {
			linked = 0
		}
		rawJSON := "{}"
		if len(c.AllowedChannels) > 0 {
			chans := make([]string, 0, len(c.AllowedChannels))
			for _, ch := range c.AllowedChannels {
				ch = strings.ToLower(strings.TrimSpace(ch))
				if ch != "" {
					chans = append(chans, ch)
				}
			}
			if len(chans) > 0 {
				if b, err := json.Marshal(struct {
					AllowedChannels []string `json:"allowed_channels"`
				}{AllowedChannels: chans}); err == nil {
					rawJSON = string(b)
				}
			}
		}
		if err := p.Q.UpsertCampaign(ctx, gen.UpsertCampaignParams{
			ID:             c.ID,
			Platform:       c.Platform,
			Game:           c.Game,
			Name:           c.Name,
			StartsAt:       startsAt,
			EndsAt:         endsAt,
			Status:         status,
			RawJson:        rawJSON,
			DiscoveredAt:   now,
			Kind:           kind,
			AccountLinked:  linked,
			AccountLinkUrl: c.AccountLinkURL,
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

// PersistAccountLinks records this account's per-campaign link state so the
// not-linked table can show WHICH accounts must connect (the shared
// campaigns.account_linked is last-writer-wins across accounts). Only writes
// rows whose link status was actually checked. Best-effort per row.
func (p *CampaignPersister) PersistAccountLinks(ctx context.Context, accountID string, camps []platform.Campaign) error {
	if p == nil || p.Q == nil || accountID == "" {
		return nil
	}
	now := time.Now().Unix()
	for _, c := range camps {
		if c.ID == "" || !c.AccountLinkChecked {
			continue
		}
		linked := int64(1)
		if !c.AccountLinked {
			linked = 0
		}
		_ = p.Q.UpsertAccountCampaignLink(ctx, gen.UpsertAccountCampaignLinkParams{
			AccountID:  accountID,
			CampaignID: c.ID,
			Linked:     linked,
			Checked:    1,
			LinkUrl:    c.AccountLinkURL,
			UpdatedAt:  now,
		})
	}
	return nil
}
