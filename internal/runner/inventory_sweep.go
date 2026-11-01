package runner

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/plan"
	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// InventorySweepEvery is how often a runner sweeps its account's inventory
// (Config.InventorySweep): it claims every finished, unclaimed drop there.
// The first sweep runs when the runner starts.
const InventorySweepEvery = 24 * time.Hour

var (
	// sweepClaimGap spaces the claims of one sweep. A var so tests can
	// shrink it.
	sweepClaimGap = 2 * time.Second
	// staleClaimGrace is how old a claim-log row must be before the sweep
	// trusts an inventory that still shows the drop unclaimed: Twitch's
	// inventory can lag a claim it granted.
	staleClaimGrace = time.Hour
)

// ClaimCorrector is an optional ClaimLog capability. The inventory sweep uses
// it to undo a logged claim the platform's inventory says never happened.
type ClaimCorrector interface {
	// LoggedClaim returns when benefitID was logged and whether the user
	// marked it by hand. ok is false when the log has no row for it.
	LoggedClaim(ctx context.Context, accountID, benefitID string) (at time.Time, manual, ok bool, err error)
	// ForgetClaim deletes the log row, so the drop counts as unclaimed again.
	ForgetClaim(ctx context.Context, accountID, benefitID string) error
}

// sweepCounts is one inventory sweep's tally.
type sweepCounts struct{ claimed, failed, corrected int }

// inventorySweepLoop sweeps the inventory now and then every
// InventorySweepEvery until ctx ends. A sweep that falls in the sleep window
// waits for the window to end. It runs beside Run, so the watch beacon never
// waits on it.
func (r *Runner) inventorySweepLoop(ctx context.Context) {
	for {
		for r.InSleepWindow() {
			if !sleepCtx(ctx, sleepPoll) {
				return
			}
		}
		r.sweepInventory(ctx)
		if !sleepCtx(ctx, InventorySweepEvery) {
			return
		}
	}
}

// sleepCtx waits d and reports false when ctx ended first.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// sweepInventory runs one inventory sweep and logs its summary. Twitch
// (platform.ReadyDropLister) lists the finished, unclaimed drops and claims
// each. Kick (platform.CompletedSweeper) claims them in the backend.
func (r *Runner) sweepInventory(ctx context.Context) {
	var (
		n   sweepCounts
		err error
	)
	switch b := r.cfg.Backend.(type) {
	case platform.ReadyDropLister:
		n, err = r.sweepReady(ctx, b)
	case platform.CompletedSweeper:
		var swept []platform.ClaimedReward
		swept, err = b.SweepCompletedClaims(ctx, r.cfg.Session)
		for _, cr := range swept {
			r.notifySwept(ctx, cr)
		}
		n.claimed = len(swept)
	default:
		return
	}
	if err != nil {
		if ctx.Err() == nil {
			slog.Warn("runner inventory sweep failed", "kind", "discovery", "account", r.cfg.AccountID, "err", err)
		}
		return
	}
	slog.Info("runner inventory sweep done", "kind", "discovery", "account", r.cfg.AccountID,
		"claimed", n.claimed, "failed", n.failed, "corrected", n.corrected)
	if n.claimed > 0 || n.corrected > 0 {
		r.Rescan() // rebuild the plan and drop states from the corrected log
	}
}

// sweepReady claims every finished, unclaimed drop the inventory lists, with
// the account's own drop instance ID, sweepClaimGap apart. The inventory is
// the truth: a drop the claim log holds is claimed again once its row is
// older than staleClaimGrace, and the row is dropped first, so a claim that
// fails leaves the drop claimable. A drop marked collected by hand, the drop
// the runner is mining, and a drop whose campaign needs an account link are
// left alone. A drop Twitch refuses is skipped until a later sweep claims it.
func (r *Runner) sweepReady(ctx context.Context, rl platform.ReadyDropLister) (sweepCounts, error) {
	var n sweepCounts
	drops, err := rl.ReadyDrops(ctx, r.cfg.Session)
	if err != nil {
		return n, err
	}
	var logged map[string]bool
	if r.cfg.Claims != nil && len(drops) > 0 {
		if logged, err = r.cfg.Claims.ClaimedBenefitIDs(ctx, r.cfg.AccountID); err != nil {
			return n, err
		}
	}
	corrector, _ := r.cfg.Claims.(ClaimCorrector)
	attempts := 0
	for _, rd := range drops {
		d, camp := rd.Benefit, rd.Campaign
		camp.Benefits = nil
		if camp.ID == "" {
			camp.ID = d.CampaignID
		}
		if camp.Platform == "" {
			camp.Platform = r.cfg.Platform
		}
		if plan.Unlinked(camp) {
			continue
		}
		r.mu.Lock()
		mining := r.current != nil && r.current.Benefit.ID == d.ID
		r.mu.Unlock()
		if mining {
			continue // the runner's own claim takes it
		}
		if logged[d.ID] {
			if corrector == nil || !r.forgetStale(ctx, corrector, d) {
				continue
			}
			n.corrected++
		}
		if attempts > 0 && !sleepCtx(ctx, sweepClaimGap) {
			return n, ctx.Err()
		}
		attempts++
		if err := r.cfg.Backend.Claim(ctx, r.cfg.Session, d); err != nil {
			if ctx.Err() != nil {
				return n, ctx.Err()
			}
			n.failed++
			slog.Warn("runner inventory sweep claim failed", "kind", "claim", "account", r.cfg.AccountID,
				"benefit", d.ID, "benefit_name", d.Name, "campaign", camp.Name, "err", err)
			if errors.Is(err, platform.ErrClaimRefused) {
				r.skipRefused(d)
			}
			continue
		}
		n.claimed++
		r.mu.Lock()
		skipped := r.skipped[d.ID]
		delete(r.claimFailSkipped, d.ID)
		r.mu.Unlock()
		if skipped {
			r.clearSkip(d.ID)
		}
		t := plan.Target{Campaign: camp, Benefit: d, Minutes: d.RequiredMinutes}
		extra := map[string]any{}
		if d.ImageURL != "" {
			extra["image"] = d.ImageURL
		}
		if camp.Game != "" {
			extra["game"] = camp.Game
		}
		if camp.Name != "" {
			extra["campaign"] = camp.Name
		}
		r.recordClaimed(ctx, &t, d, "", extra)
	}
	return n, nil
}

// forgetStale drops the claim-log row of drop d, which the inventory shows
// finished and unclaimed. It keeps a row the user marked by hand and a row
// younger than staleClaimGrace, and reports whether the row is gone.
func (r *Runner) forgetStale(ctx context.Context, c ClaimCorrector, d platform.DropBenefit) bool {
	at, manual, ok, err := c.LoggedClaim(ctx, r.cfg.AccountID, d.ID)
	if err != nil {
		slog.Warn("runner inventory sweep: claim log read failed", "kind", "error",
			"account", r.cfg.AccountID, "benefit", d.ID, "err", err)
		return false
	}
	if !ok || manual || time.Since(at) < staleClaimGrace {
		return false
	}
	if err := c.ForgetClaim(ctx, r.cfg.AccountID, d.ID); err != nil {
		slog.Warn("runner inventory sweep: forget stale claim failed", "kind", "error",
			"account", r.cfg.AccountID, "benefit", d.ID, "err", err)
		return false
	}
	slog.Info("runner inventory sweep: logged claim never landed; claiming again", "kind", "claim",
		"account", r.cfg.AccountID, "benefit", d.ID, "benefit_name", d.Name, "logged_at", at)
	return true
}

// skipRefused skips drop d after the platform refused its claim, as the
// runner does after repeated claim failures, so the plan does not send the
// runner to watch a finished drop. The skip holds while the drop stays
// finished; the next sweep retries the claim.
func (r *Runner) skipRefused(d platform.DropBenefit) {
	r.mu.Lock()
	r.skipped[d.ID] = true
	if r.claimFailSkipped == nil {
		r.claimFailSkipped = map[string]int{}
	}
	r.claimFailSkipped[d.ID] = d.RequiredMinutes
	r.mu.Unlock()
	if r.cfg.Skips != nil {
		if err := r.cfg.Skips.RecordSkip(r.cfg.AccountID, d.ID); err != nil {
			slog.Warn("runner persist skip failed; in-memory skip still applies this run",
				"kind", "error", "account", r.cfg.AccountID, "benefit", d.ID, "err", err)
		}
	}
}
