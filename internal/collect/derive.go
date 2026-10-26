// Package collect turns runner scans into persisted per-drop state, the
// claim log and the minutes counter.
package collect

import (
	"time"

	"github.com/aalejandrofer/grubdrops/internal/plan"
	"github.com/aalejandrofer/grubdrops/internal/runner"
	"github.com/aalejandrofer/grubdrops/internal/store"
)

// Drop states stored in store.DropState.State.
const (
	StateEligible  = "eligible"
	StateAccruing  = "accruing"
	StateClaimable = "claimable"
	StateClaimed   = "claimed"
	StateBlocked   = "blocked"
)

// StateFor classifies a queued benefit by its watched minutes.
func StateFor(minutes, required int) string {
	switch {
	case minutes >= required:
		return StateClaimable
	case minutes > 0:
		return StateAccruing
	default:
		return StateEligible
	}
}

// Derive builds one row per queued, blocked and claimed target. A failed scan
// yields nil so stale rows are not replaced by an empty set.
func Derive(s runner.Scan, now time.Time) []store.DropState {
	if s.Err != nil {
		return nil
	}
	var rows []store.DropState
	for _, t := range s.Result.Queue {
		rows = append(rows, row(s, t, StateFor(t.Minutes, t.Benefit.RequiredMinutes), "", t.Minutes, now))
	}
	for _, b := range s.Result.Blocked {
		rows = append(rows, row(s, b.Target, StateBlocked, string(b.Reason), b.Minutes, now))
	}
	for _, t := range s.Result.Claimed {
		rows = append(rows, row(s, t, StateClaimed, "", t.Benefit.RequiredMinutes, now))
	}
	return rows
}

func row(s runner.Scan, t plan.Target, state, reason string, minutes int, now time.Time) store.DropState {
	return store.DropState{
		AccountID: s.AccountID, Platform: s.Platform, Game: t.Campaign.Game,
		CampaignID: t.Campaign.ID, CampaignName: t.Campaign.Name,
		BenefitID: t.Benefit.ID, BenefitName: t.Benefit.Name, ImageURL: t.Benefit.ImageURL,
		State: state, Reason: reason, Minutes: minutes, RequiredMinutes: t.Benefit.RequiredMinutes,
		EndsAt: t.Campaign.EndsAt, UpdatedAt: now,
	}
}

// Found returns claim-log entries for drops the inventory shows as claimed
// that the claim log does not know yet (claimed outside GrubDrops).
func Found(s runner.Scan, now time.Time) []store.ClaimEntry {
	if s.Err != nil {
		return nil
	}
	inv := map[string]bool{}
	for _, p := range s.Progress {
		if p.Claimed {
			inv[p.BenefitID] = true
		}
	}
	var out []store.ClaimEntry
	for _, t := range s.Result.Claimed {
		id := t.Benefit.ID
		if !inv[id] || s.Logged[id] {
			continue
		}
		out = append(out, store.ClaimEntry{
			AccountID: s.AccountID, Platform: s.Platform, Game: t.Campaign.Game,
			CampaignID: t.Campaign.ID, CampaignName: t.Campaign.Name,
			BenefitID: id, BenefitName: t.Benefit.Name, ImageURL: t.Benefit.ImageURL,
			Source: store.ClaimSourceInventory, ClaimedAt: now,
		})
	}
	return out
}
