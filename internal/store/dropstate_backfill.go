package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/pipeline/dropstate"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

// DropStateBackfilledKey marks the one-shot v1 to v2 state backfill as done.
const DropStateBackfilledKey = "drop_state_backfilled"

// BackfillDropState seeds drop_state from v1 state: ghost-skips become
// not_enrolled with an immediate retry, claim history becomes platform
// claims, and manual marks become user claims. Later sources win: manual
// marks go last because the mark-collected handler also writes a claims row,
// and a user assertion must not become a final platform claim. Every row
// carries the benefit's required minutes. Runs once; the v1 kv keys are left
// in place so rolling back to v1 is safe.
func BackfillDropState(ctx context.Context, q *gen.Queries, now time.Time) (int, error) {
	if v, err := q.GetSettingString(ctx, DropStateBackfilledKey); err == nil && string(v) == "1" {
		return 0, nil
	}
	rows := map[string]dropstate.Row{}
	put := func(r dropstate.Row) { rows[r.AccountID+"|"+r.DropID] = r }

	fromKV := func(prefix string, mk func(acct, drop, campaign, plat string, required int) dropstate.Row) error {
		kvs, err := q.ListKVByPrefix(ctx, sql.NullString{String: prefix, Valid: true})
		if err != nil {
			return fmt.Errorf("list %s: %w", prefix, err)
		}
		for _, kv := range kvs {
			drop, acct, ok := splitOverrideKey(kv.Key, prefix)
			if !ok {
				continue
			}
			if _, err := q.GetAccount(ctx, acct); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					continue // account deleted, kv leftover: nothing to seed
				}
				return fmt.Errorf("get account %s: %w", acct, err)
			}
			bc, err := q.GetBenefitCampaign(ctx, drop)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					continue // benefit row gone, nothing to seed
				}
				return fmt.Errorf("get benefit campaign %s: %w", drop, err)
			}
			put(mk(acct, drop, bc.CampaignID, bc.Platform, int(bc.RequiredMinutes)))
		}
		return nil
	}
	if err := fromKV(SkipOverridePrefix, func(acct, drop, campaign, plat string, required int) dropstate.Row {
		return dropstate.Row{AccountID: acct, DropID: drop, CampaignID: campaign, Platform: plat,
			Status: dropstate.Blocked, Reason: dropstate.NotEnrolled, Source: dropstate.FromPlatform,
			Required: required, RetryAfter: now, UpdatedAt: now}
	}); err != nil {
		return 0, err
	}
	claims, err := q.ListClaimsForBackfill(ctx)
	if err != nil {
		return 0, fmt.Errorf("list claims: %w", err)
	}
	for _, c := range claims {
		put(dropstate.Row{AccountID: c.AccountID, DropID: c.BenefitID, CampaignID: c.CampaignID, Platform: c.Platform,
			Status: dropstate.Claimed, Source: dropstate.FromPlatform, Required: int(c.RequiredMinutes), UpdatedAt: now})
	}
	if err := fromKV(CollectOverridePrefix, func(acct, drop, campaign, plat string, required int) dropstate.Row {
		return dropstate.Row{AccountID: acct, DropID: drop, CampaignID: campaign, Platform: plat,
			Status: dropstate.Claimed, Source: dropstate.FromUser, Required: required, UpdatedAt: now}
	}); err != nil {
		return 0, err
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
