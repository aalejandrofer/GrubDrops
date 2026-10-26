package runner

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/plan"
	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// blockNotLinked handles a claim the platform refused until the user links an
// external account. It is not a claim failure: the drop is blocked as not
// linked, never skipped, and the runner picks the next target.
func (r *Runner) blockNotLinked(ctx context.Context, benefit platform.DropBenefit, err error) {
	slog.Warn("runner claim needs account link; drop blocked until linked",
		"kind", "claim", "account", r.cfg.AccountID,
		"benefit", benefit.ID, "benefit_name", benefit.Name, "err", err)
	r.stopWatch(ctx)
	r.mu.Lock()
	if r.linkRefused == nil {
		r.linkRefused = map[string]time.Time{}
	}
	r.linkRefused[benefit.ID] = time.Now()
	delete(r.claimFailures, benefit.ID)
	r.current = nil
	r.unpinLocked(benefit.ID)
	r.mu.Unlock()
	r.setState(ctx, StatePicking)
}

// retryLinkRefused retries the claim of each completed, unclaimed benefit the
// platform refused until linked: the ones the runner saw refused, and the
// completed ones of a campaign the backend flags ClaimNeedsLink (that flag
// survives a restart). Each is retried at most once per recheckInterval. It
// returns claimed with any benefit claimed now added, and the benefits still
// refused for the plan to block.
func (r *Runner) retryLinkRefused(ctx context.Context, camps []platform.Campaign, progress []platform.Progress, claimed map[string]bool) (map[string]bool, map[string]bool) {
	minutes := map[string]int{}
	done := map[string]bool{}
	for _, p := range progress {
		minutes[p.BenefitID] = max(minutes[p.BenefitID], p.MinutesWatched)
		if p.Claimed {
			done[p.BenefitID] = true
		}
	}
	type cand struct {
		t       plan.Target
		lastTry time.Time
	}
	var cands []cand
	seen := map[string]bool{}
	r.mu.Lock()
	for _, c := range camps {
		if _, ok := plan.Match(r.cfg.Games, c.Game); !ok || (c.Status != "" && c.Status != "active") {
			continue
		}
		for _, b := range c.Benefits {
			if b.RequiredMinutes <= 0 || seen[b.ID] || done[b.ID] || claimed[b.ID] {
				continue
			}
			last, refused := r.linkRefused[b.ID]
			if !refused && !(c.ClaimNeedsLink && minutes[b.ID] >= b.RequiredMinutes) {
				continue
			}
			seen[b.ID] = true
			cands = append(cands, cand{plan.Target{Campaign: c, Benefit: b, Minutes: minutes[b.ID]}, last})
		}
	}
	// Refused benefits no longer listed or now claimed are forgotten.
	for id := range r.linkRefused {
		if !seen[id] {
			delete(r.linkRefused, id)
		}
	}
	r.mu.Unlock()

	var notLinked map[string]bool
	for _, cd := range cands {
		id := cd.t.Benefit.ID
		if !cd.lastTry.IsZero() && time.Since(cd.lastTry) < recheckInterval {
			notLinked = setTrue(notLinked, id)
			continue
		}
		err := r.cfg.Backend.Claim(ctx, r.cfg.Session, cd.t.Benefit)
		r.mu.Lock()
		switch {
		case err == nil:
			delete(r.linkRefused, id)
		case errors.Is(err, platform.ErrClaimNeedsLink):
			if r.linkRefused == nil {
				r.linkRefused = map[string]time.Time{}
			}
			r.linkRefused[id] = time.Now()
		default:
			// Not a link refusal: back to the normal claim flow.
			delete(r.linkRefused, id)
		}
		r.mu.Unlock()
		switch {
		case err == nil:
			slog.Info("runner claimed drop after account link", "kind", "claim",
				"account", r.cfg.AccountID, "benefit", id, "benefit_name", cd.t.Benefit.Name)
			claimed = setTrue(claimed, id)
			r.recordLinkedClaim(ctx, cd.t)
		case errors.Is(err, platform.ErrClaimNeedsLink):
			slog.Debug("runner claim still needs account link", "kind", "claim",
				"account", r.cfg.AccountID, "benefit", id)
			notLinked = setTrue(notLinked, id)
		default:
			slog.Warn("runner link retry claim failed", "kind", "error",
				"account", r.cfg.AccountID, "benefit", id, "err", err)
		}
	}
	return claimed, notLinked
}

// recordLinkedClaim logs and notifies a claim made by a link retry.
func (r *Runner) recordLinkedClaim(ctx context.Context, t plan.Target) {
	if r.cfg.Claims != nil {
		rctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		err := r.cfg.Claims.RecordClaim(rctx, r.cfg.AccountID, t, "")
		cancel()
		if err != nil {
			slog.Warn("runner record claim failed", "kind", "error", "account", r.cfg.AccountID,
				"benefit", t.Benefit.ID, "err", err)
		}
	}
	if r.cfg.Notifier != nil {
		// The claimed drop is not the mined one: none of current's fields apply.
		f := r.notifyFields(nil)
		for _, k := range []string{"game", "campaign", "drop", "image", "channel"} {
			delete(f, k)
		}
		f["game"], f["campaign"], f["drop"] = t.Campaign.Game, t.Campaign.Name, t.Benefit.Name
		if u := t.Benefit.ImageURL; u != "" {
			f["image"] = u
		}
		f["cur_min"], f["req_min"] = t.Benefit.RequiredMinutes, t.Benefit.RequiredMinutes
		_ = r.cfg.Notifier.Notify(ctx, "claim", f)
	}
}

func setTrue(m map[string]bool, id string) map[string]bool {
	if m == nil {
		m = map[string]bool{}
	}
	m[id] = true
	return m
}

// LinkRecheck is the result of RecheckLinks.
type LinkRecheck struct {
	Claimed        int
	StillNeedsLink int
}

// RecheckLinks is the user's "I linked it" check: it forgets the account's
// link refusals, asks a LinkRechecker backend to retry them now, and rescans
// so the plan picks the drops up again.
func (r *Runner) RecheckLinks(ctx context.Context) (LinkRecheck, error) {
	r.mu.Lock()
	r.linkRefused = nil
	r.mu.Unlock()
	var out LinkRecheck
	var err error
	if lr, ok := r.cfg.Backend.(platform.LinkRechecker); ok {
		var claimed []platform.ClaimedReward
		claimed, out.StillNeedsLink, err = lr.RecheckLinks(ctx, r.cfg.Session)
		out.Claimed = len(claimed)
		for _, cr := range claimed {
			r.notifySwept(ctx, cr)
		}
	} else {
		// No retry hook: the rescan rechecks the platform's link state.
		r.mu.Lock()
		for _, b := range r.blocked {
			if b.Reason == plan.ReasonNotLinked {
				out.StillNeedsLink++
			}
		}
		r.mu.Unlock()
	}
	r.Rescan()
	return out, err
}
