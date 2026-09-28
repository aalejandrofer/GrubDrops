package twitch

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/gameslug"
	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// maxChannelsPerGame bounds the AvailableDrops fan-out per whitelisted
// game. The directory is sorted by viewers; campaigns a game runs show
// up on its top channels, and in-progress ones come from Inventory.
const maxChannelsPerGame = 10

// tvBenefitEdge decodes one benefitEdges[] element, shared by
// availableDropsFull (tvDrop) and inventoryData's TimeBasedDrops so a
// benefit edge from either payload can be copied into a tvDrop without
// a Go anonymous-struct type mismatch.
type tvBenefitEdge struct {
	Benefit struct {
		ID            string `json:"id"`
		Name          string `json:"name"`
		ImageAssetURL string `json:"imageAssetURL"`
	} `json:"benefit"`
}

// availableDropsFull decodes the full AvailableDrops payload. Unlike the
// dashboard/details queries, Twitch serves it to TV-client tokens.
type availableDropsFull struct {
	Channel struct {
		ViewerDropCampaigns []struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			StartAt string `json:"startAt"`
			EndAt   string `json:"endAt"`
			Game    struct {
				Name string `json:"name"`
			} `json:"game"`
			TimeBasedDrops []tvDrop `json:"timeBasedDrops"`
		} `json:"viewerDropCampaigns"`
	} `json:"channel"`
}

type tvDrop struct {
	ID                     string          `json:"id"`
	Name                   string          `json:"name"`
	RequiredMinutesWatched int             `json:"requiredMinutesWatched"`
	RequiredSubs           int             `json:"requiredSubs"`
	BenefitEdges           []tvBenefitEdge `json:"benefitEdges"`
}

// windowStatus derives a campaign's Status from its start/end window, with
// the same vocabulary the dashboard path persists: before start ->
// "upcoming", after end -> "expired", else "active". A zero bound is
// treated as open (payloads that omit startAt/endAt stay "active").
func windowStatus(start, end, now time.Time) string {
	switch {
	case !start.IsZero() && now.Before(start):
		return "upcoming"
	case !end.IsZero() && now.After(end):
		return "expired"
	default:
		return "active"
	}
}

// campaignStatus derives an Inventory campaign's Status. Inventory's own
// status enum is authoritative when present and is mapped exactly like
// listActive maps the dashboard's status ("ACTIVE"->"active",
// "EXPIRED"->"expired", "UPCOMING"->"upcoming", anything else
// lower-cased). When Twitch omits status (empty string), fall back to
// the startAt/endAt window heuristic.
func campaignStatus(status string, start, end, now time.Time) string {
	switch status {
	case "ACTIVE":
		return "active"
	case "UPCOMING":
		return "upcoming"
	case "EXPIRED":
		return "expired"
	case "":
		return windowStatus(start, end, now)
	default:
		return strings.ToLower(status)
	}
}

// toBenefits flattens drops the same way fetchDetails does, including
// the #47 rule: sub-gated drops report 0 minutes (not watch-earnable).
func toBenefits(campaignID string, drops []tvDrop) []platform.DropBenefit {
	var out []platform.DropBenefit
	for _, td := range drops {
		req := td.RequiredMinutesWatched
		if td.RequiredSubs > 0 {
			req = 0
		}
		for _, be := range td.BenefitEdges {
			out = append(out, platform.DropBenefit{
				ID: td.ID, CampaignID: campaignID, Name: be.Benefit.Name,
				RequiredMinutes: req, ImageURL: be.Benefit.ImageAssetURL, RewardID: be.Benefit.ID,
			})
		}
	}
	return out
}

// listByChannels discovers campaigns for sessions that can't see the
// drops dashboard (TV client). Directory(DROPS_ENABLED) per whitelisted
// game → AvailableDrops per top channel, merged with Inventory's
// in-progress campaigns (which AvailableDrops may omit, and which carry
// their own allow-lists). Returns campaigns + campaignID→allowed logins.
func (d *discovery) listByChannels(ctx context.Context, sess platform.Session, ch *channels) ([]platform.Campaign, map[string][]string, error) {
	now := time.Now()
	camps := map[string]*platform.Campaign{}
	allowed := map[string][]string{}
	order := []string{}
	add := func(c platform.Campaign) *platform.Campaign {
		if ex, ok := camps[c.ID]; ok {
			return ex
		}
		cc := c
		camps[c.ID] = &cc
		order = append(order, c.ID)
		return &cc
	}

	// sess.Games may carry a game under more than one token — the discovery
	// scraper's whitelist union emits both a game's lowercased display name
	// and its lowercased slug (e.g. "grand theft auto v" AND
	// "grand-theft-auto-v"), and gameslug.Slug maps both to the same value.
	// Dedupe by slug (skipping empty ones) so a multi-word game doesn't
	// double the DirectoryPage_Game + AvailableDrops fan-out every tick.
	seenSlugs := make(map[string]struct{}, len(sess.Games))
	for _, game := range sess.Games {
		slug := gameslug.Slug(game)
		if slug == "" {
			continue
		}
		if _, dup := seenSlugs[slug]; dup {
			continue
		}
		seenSlugs[slug] = struct{}{}

		streams, err := ch.listForGameDirectory(ctx, sess, slug)
		if err != nil {
			slog.Warn("tv discovery: directory failed", "game", game, "err", err)
			continue // one bad game must not sink the rest
		}
		if len(streams) > maxChannelsPerGame {
			streams = streams[:maxChannelsPerGame]
		}
		for _, s := range streams {
			var resp availableDropsFull
			if err := d.c.gql(ctx, sess.AccessToken, OpAvailableDrops, map[string]any{"channelID": s.ChannelID}, &resp); err != nil {
				slog.Warn("tv discovery: available drops failed", "channel", s.Channel, "err", err)
				continue
			}
			for _, vc := range resp.Channel.ViewerDropCampaigns {
				start, end := parseISO(vc.StartAt), parseISO(vc.EndAt)
				add(platform.Campaign{
					ID: vc.ID, Platform: "twitch", Game: vc.Game.Name, Name: vc.Name,
					StartsAt: start, EndsAt: end, Status: windowStatus(start, end, now), Kind: "drop",
					// Link state is unknowable without DropCampaignDetails;
					// optimistic like scrape-sourced campaigns.
					AccountLinked: true, AccountLinkChecked: false,
					Benefits: toBenefits(vc.ID, vc.TimeBasedDrops),
				})
				allowed[vc.ID] = appendUnique(allowed[vc.ID], s.Channel)
			}
		}
	}

	// A failed Inventory call must not discard the directory results:
	// log it and continue with an empty inventory (in-progress campaigns
	// the directory didn't surface reappear on the next pass).
	var inv inventoryData
	if err := d.c.gql(ctx, sess.AccessToken, OpInventory, nil, &inv); err != nil {
		slog.Warn("tv discovery: inventory failed; returning directory campaigns only", "err", err)
		inv = inventoryData{}
	}
	for _, ic := range inv.CurrentUser.Inventory.DropCampaignsInProgress {
		drops := make([]tvDrop, 0, len(ic.TimeBasedDrops))
		for _, td := range ic.TimeBasedDrops {
			drops = append(drops, tvDrop{
				ID: td.ID, Name: td.Name,
				RequiredMinutesWatched: td.RequiredMinutesWatched,
				RequiredSubs:           td.RequiredSubs,
				BenefitEdges:           td.BenefitEdges,
			})
		}
		start, end := parseISO(ic.StartAt), parseISO(ic.EndAt)
		status := campaignStatus(ic.Status, start, end, now)
		benefits := toBenefits(ic.ID, drops)

		// Inventory is authoritative for link state + status. When the
		// same campaign ID was already added from AvailableDrops
		// (optimistic, unknown link state), override those fields on
		// the existing entry instead of discarding Inventory's data —
		// and merge benefits by ID so a drop present in both payloads
		// isn't duplicated.
		if ex, ok := camps[ic.ID]; ok {
			ex.AccountLinked = ic.Self.IsAccountConnected
			ex.AccountLinkChecked = true
			ex.AccountLinkURL = ic.AccountLinkURL
			ex.Status = status
			seen := make(map[string]struct{}, len(ex.Benefits))
			for _, b := range ex.Benefits {
				seen[b.ID] = struct{}{}
			}
			for _, b := range benefits {
				if _, dup := seen[b.ID]; dup {
					continue
				}
				ex.Benefits = append(ex.Benefits, b)
			}
		} else {
			add(platform.Campaign{
				ID: ic.ID, Platform: "twitch", Game: ic.Game.Name, Name: ic.Name,
				StartsAt: start, EndsAt: end, Status: status, Kind: "drop",
				AccountLinked: ic.Self.IsAccountConnected, AccountLinkChecked: true, AccountLinkURL: ic.AccountLinkURL,
				Benefits: benefits,
			})
		}
		if len(ic.Allow.Channels) > 0 {
			var logins []string
			for _, c := range ic.Allow.Channels {
				logins = appendUnique(logins, c.Name)
			}
			allowed[ic.ID] = logins // Twitch's own allow-list wins
		}
	}

	out := make([]platform.Campaign, 0, len(order))
	for _, id := range order {
		out = append(out, *camps[id])
	}
	return out, allowed, nil
}

func appendUnique(xs []string, x string) []string {
	for _, e := range xs {
		if e == x {
			return xs
		}
	}
	return append(xs, x)
}
