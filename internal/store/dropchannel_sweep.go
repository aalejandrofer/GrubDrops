package store

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

// SweepStaleDropChannels removes per-account channel-whitelist entries
// (account_channels) whose null-game campaign has ended — those channels are
// only useful while their drop is live, so they auto-clean when the campaign
// rotates out. Force-watch channels live in a separate table and are never
// touched.
//
// Safety: the keep-set is built from every CURRENTLY-ACTIVE campaign's
// allowed_channels (persisted in raw_json). If discovery hasn't populated the
// campaigns table yet (no current campaigns at all), the sweep is skipped so a
// transient empty discovery can't wipe the user's channels. Returns the number
// of channels removed.
func SweepStaleDropChannels(ctx context.Context, q *gen.Queries, now int64) (int, error) {
	cur, err := q.ListCurrentCampaigns(ctx, gen.ListCurrentCampaignsParams{
		StartsAt: now, EndsAt: now, Limit: 1000,
	})
	if err != nil {
		return 0, err
	}
	if len(cur) == 0 {
		// Discovery not populated — don't risk wiping channels.
		return 0, nil
	}

	keep := map[string]struct{}{}
	for _, c := range cur {
		for _, ch := range allowedChannelsFromRawJSON(c.RawJson) {
			keep[strings.ToLower(strings.TrimSpace(ch))] = struct{}{}
		}
	}

	rows, err := q.ListAllAccountChannels(ctx)
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, row := range rows {
		if _, ok := keep[strings.ToLower(strings.TrimSpace(row.Channel))]; ok {
			continue
		}
		if err := q.RemoveAccountChannel(ctx, gen.RemoveAccountChannelParams{
			AccountID: row.AccountID, Channel: row.Channel,
		}); err != nil {
			return removed, err
		}
		removed++
	}
	return removed, nil
}

// allowedChannelsFromRawJSON extracts the persisted allowed_channels list from
// a campaign row's raw_json (written by the campaign persister).
func allowedChannelsFromRawJSON(raw string) []string {
	if raw == "" || raw == "{}" {
		return nil
	}
	var meta struct {
		AllowedChannels []string `json:"allowed_channels"`
	}
	if err := json.Unmarshal([]byte(raw), &meta); err != nil {
		return nil
	}
	return meta.AllowedChannels
}
