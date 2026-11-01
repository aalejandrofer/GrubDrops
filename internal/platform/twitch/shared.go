package twitch

import (
	"context"
	"log/slog"
	"slices"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// SharedCatalog returns the campaigns an Android login's drops dashboard
// lists, for a TV session's scan (experimental). nil campaigns mean nothing
// to share: the feature is off or there is no Android login. The slice may
// be shared with other readers and must not be changed.
type SharedCatalog func(ctx context.Context) ([]platform.Campaign, error)

// SetSharedCatalog sets the source a TV session's scan merges from. nil
// turns sharing off.
func (b *Backend) SetSharedCatalog(f SharedCatalog) {
	b.mu.Lock()
	b.shared = f
	b.mu.Unlock()
}

// withShared appends the shared campaigns the TV scan did not find, and puts
// their allow-lists in allowed. Off, empty or failing, it returns camps and
// leaves allowed untouched: the TV scan never fails because of it.
func (b *Backend) withShared(ctx context.Context, s platform.Session, camps []platform.Campaign, allowed map[string][]string) []platform.Campaign {
	b.mu.Lock()
	f := b.shared
	b.mu.Unlock()
	if f == nil {
		return camps
	}
	shared, err := f(ctx)
	if err != nil {
		slog.Debug("tv discovery: shared catalog unavailable", "account", s.AccountID, "err", err)
		return camps
	}
	add := mergeShared(camps, shared, s.GameFilter, time.Now())
	if len(add) == 0 {
		return camps
	}
	for _, c := range add {
		// Catalog's allow-list is Twitch's own (DropCampaignDetails); none
		// means any live channel of the game, so drop any stale entry.
		allowed[c.ID] = c.AllowedChannels
	}
	slog.Debug("tv discovery: added shared campaigns", "account", s.AccountID, "count", len(add))
	return append(camps, add...)
}

// mergeShared returns the shared campaigns to add to own: active now, with
// watch-time drops, on a game filter allows (nil allows all, as the plan
// does), and not already in own. Each is stripped of the Android account's
// state by shareable.
func mergeShared(own, shared []platform.Campaign, filter func(string) bool, now time.Time) []platform.Campaign {
	if len(shared) == 0 {
		return nil
	}
	have := make(map[string]struct{}, len(own))
	for _, c := range own {
		have[c.ID] = struct{}{}
	}
	var out []platform.Campaign
	for _, c := range shared {
		if c.ID == "" || (c.Platform != "" && c.Platform != "twitch") {
			continue
		}
		if _, dup := have[c.ID]; dup {
			continue // the TV account's own copy wins
		}
		if c.Status != "active" || windowStatus(c.StartsAt, c.EndsAt, now) != "active" {
			continue // the cached catalog may predate the campaign's end
		}
		if c.Kind != "" && c.Kind != "drop" {
			continue
		}
		if len(c.Benefits) == 0 {
			continue // details failed: nothing to mine, allow-list unknown
		}
		if filter != nil && !filter(c.Game) {
			continue
		}
		have[c.ID] = struct{}{}
		out = append(out, shareable(c))
	}
	return out
}

// shareable copies the campaign-level fields of an Android-sourced campaign
// and nothing tied to the Android account. Link state is unknown for the TV
// account: a campaign with a publisher link page is reported unlinked and
// unchecked (the safe side: the plan flags it and a refused claim blocks
// it), one without needs no link. Drop instance ids are cleared so a claim
// builds the TV account's own. Fields are listed one by one so a field added
// to Campaign later is not shared by accident.
func shareable(c platform.Campaign) platform.Campaign {
	out := platform.Campaign{
		ID:                  c.ID,
		Platform:            "twitch",
		Game:                c.Game,
		GameID:              c.GameID,
		BoxArtURL:           c.BoxArtURL,
		Name:                c.Name,
		StartsAt:            c.StartsAt,
		EndsAt:              c.EndsAt,
		Status:              "active",
		Kind:                "drop",
		AccountLinkURL:      c.AccountLinkURL,
		AccountLinked:       !platform.ExternalLinkURL(c.AccountLinkURL),
		AccountLinkChecked:  false,
		AllowedChannels:     slices.Clone(c.AllowedChannels),
		AllowedChannelCount: len(c.AllowedChannels),
	}
	out.Benefits = make([]platform.DropBenefit, 0, len(c.Benefits))
	for _, d := range c.Benefits {
		out.Benefits = append(out.Benefits, platform.DropBenefit{
			ID:              d.ID,
			CampaignID:      c.ID,
			Name:            d.Name,
			RequiredMinutes: d.RequiredMinutes,
			ImageURL:        d.ImageURL,
			RewardID:        d.RewardID,
			Preconditions:   slices.Clone(d.Preconditions),
		})
	}
	return out
}
