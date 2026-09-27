package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/pipeline/dropstate"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

// DropStateBackfilledKey marks the one-shot v1 to v2 state backfill as done.
const DropStateBackfilledKey = "drop_state_backfilled"

// BackfillDropState seeds drop_state from v1 state: ghost-skips become
// not_enrolled with an immediate retry, manual marks become user claims, and
// claim history becomes platform claims. Later sources win. Runs once; the v1
// kv keys are left in place so rolling back to v1 is safe.
func BackfillDropState(ctx context.Context, q *gen.Queries, now time.Time) (int, error) {
	if v, err := q.GetSettingString(ctx, DropStateBackfilledKey); err == nil && string(v) == "1" {
		return 0, nil
	}
	rows := map[string]dropstate.Row{}
	put := func(r dropstate.Row) { rows[r.AccountID+"|"+r.DropID] = r }

	fromKV := func(prefix string, mk func(acct, drop, campaign, plat string) dropstate.Row) error {
		kvs, err := q.ListKVByPrefix(ctx, sql.NullString{String: prefix, Valid: true})
		if err != nil {
			return fmt.Errorf("list %s: %w", prefix, err)
		}
		for _, kv := range kvs {
			drop, acct, ok := splitOverrideKey(kv.Key, prefix)
			if !ok {
				continue
			}
			bc, err := q.GetBenefitCampaign(ctx, drop)
			if err != nil {
				continue // benefit row gone, nothing to seed
			}
			put(mk(acct, drop, bc.CampaignID, bc.Platform))
		}
		return nil
	}
	if err := fromKV(SkipOverridePrefix, func(acct, drop, campaign, plat string) dropstate.Row {
		return dropstate.Row{AccountID: acct, DropID: drop, CampaignID: campaign, Platform: plat,
			Status: dropstate.Blocked, Reason: dropstate.NotEnrolled, Source: dropstate.FromPlatform,
			RetryAfter: now, UpdatedAt: now}
	}); err != nil {
		return 0, err
	}
	if err := fromKV(CollectOverridePrefix, func(acct, drop, campaign, plat string) dropstate.Row {
		return dropstate.Row{AccountID: acct, DropID: drop, CampaignID: campaign, Platform: plat,
			Status: dropstate.Claimed, Source: dropstate.FromUser, UpdatedAt: now}
	}); err != nil {
		return 0, err
	}
	claims, err := q.ListClaimsForBackfill(ctx)
	if err != nil {
		return 0, fmt.Errorf("list claims: %w", err)
	}
	for _, c := range claims {
		put(dropstate.Row{AccountID: c.AccountID, DropID: c.BenefitID, CampaignID: c.CampaignID, Platform: c.Platform,
			Status: dropstate.Claimed, Source: dropstate.FromPlatform, UpdatedAt: now})
	}

	st := NewDropStateStore(q)
	for _, r := range rows {
		if err := st.Upsert(ctx, r); err != nil {
			return 0, fmt.Errorf("upsert %s/%s: %w", r.AccountID, r.DropID, err)
		}
	}
	if err := q.UpsertSettingString(ctx, gen.UpsertSettingStringParams{Key: DropStateBackfilledKey, Value: []byte("1")}); err != nil {
		return 0, fmt.Errorf("mark backfilled: %w", err)
	}
	return len(rows), nil
}

// splitOverrideKey parses prefix + dropID + ":" + accountID.
func splitOverrideKey(key, prefix string) (drop, acct string, ok bool) {
	rest := strings.TrimPrefix(key, prefix)
	i := strings.LastIndex(rest, ":")
	if i <= 0 || i == len(rest)-1 {
		return "", "", false
	}
	return rest[:i], rest[i+1:], true
}
