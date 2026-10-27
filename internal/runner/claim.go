package runner

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/plan"
	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// claimFailSkipThreshold: consecutive Claim failures before the benefit is
// skipped so one broken claim cannot wedge the queue.
const claimFailSkipThreshold = 3

// linkRefusal reports whether a Claim error means the game account must be
// linked first. Twitch has no link-refusal error, so a refused claim on a
// campaign it reports unlinked counts as one; transport errors never do.
func linkRefusal(err error, c platform.Campaign) bool {
	return errors.Is(err, platform.ErrClaimNeedsLink) ||
		(plan.Unlinked(c) && errors.Is(err, platform.ErrClaimRefused))
}

// claim claims the completed current benefit.
func (r *Runner) claim(ctx context.Context) error {
	r.mu.Lock()
	if r.current == nil {
		r.mu.Unlock()
		return errors.New("cannot claim: no current target")
	}
	benefit := r.current.Benefit
	campaign := r.current.Campaign
	channel := r.channel
	if r.handle != nil && r.handle.Channel != "" {
		channel = r.handle.Channel
	}
	r.mu.Unlock()

	if err := r.cfg.Backend.Claim(ctx, r.cfg.Session, benefit); err != nil {
		if errors.Is(err, context.Canceled) || ctx.Err() != nil {
			return fmt.Errorf("claim: %w", err)
		}
		if linkRefusal(err, campaign) {
			r.blockNotLinked(ctx, benefit, err)
			return nil
		}
		slog.Error("runner claim failed", "kind", "error", "account", r.cfg.AccountID,
			"benefit", benefit.ID, "err", err)
		r.mu.Lock()
		if r.claimFailures == nil {
			r.claimFailures = map[string]int{}
		}
		r.claimFailures[benefit.ID]++
		n := r.claimFailures[benefit.ID]
		giveUp := n >= claimFailSkipThreshold
		if giveUp {
			delete(r.claimFailures, benefit.ID)
			r.skipped[benefit.ID] = true
			if r.claimFailSkipped == nil {
				r.claimFailSkipped = map[string]int{}
			}
			r.claimFailSkipped[benefit.ID] = benefit.RequiredMinutes
		}
		r.mu.Unlock()
		if giveUp {
			slog.Warn("runner claim failed repeatedly for completed drop; skipping it",
				"kind", "claim", "account", r.cfg.AccountID,
				"benefit", benefit.ID, "benefit_name", benefit.Name, "failures", n)
			if r.cfg.Skips != nil {
				if err := r.cfg.Skips.RecordSkip(r.cfg.AccountID, benefit.ID); err != nil {
					slog.Warn("runner persist skip failed; in-memory skip still applies this run",
						"kind", "error", "account", r.cfg.AccountID, "benefit", benefit.ID, "err", err)
				}
			}
		}
		return fmt.Errorf("claim: %w", err)
	}

	r.mu.Lock()
	delete(r.claimFailures, benefit.ID)
	r.mu.Unlock()

	r.finishClaim(ctx, benefit, channel)

	// Post-claim consistency probe: log only, never rolls back the claim.
	if checker, ok := r.cfg.Backend.(platform.CurrentSessionChecker); ok {
		if cs, err := checker.CurrentSession(ctx, r.cfg.Session); err == nil {
			if cs.DropID == benefit.ID {
				slog.Info("runner post-claim: drop still in current session, server lag expected",
					"kind", "claim", "account", r.cfg.AccountID, "benefit", benefit.ID,
					"current_min", cs.CurrentMinute, "required_min", cs.RequiredMinute)
			} else if cs.DropID != "" {
				slog.Debug("runner post-claim: current session is a different drop",
					"kind", "claim", "account", r.cfg.AccountID, "benefit", benefit.ID,
					"current_session_drop", cs.DropID,
					"current_min", cs.CurrentMinute, "required_min", cs.RequiredMinute)
			}
		}
	}
	return nil
}

// finishClaim settles a claimed benefit: stops the watch, records the claim,
// notifies it, clears the current target and returns to picking.
func (r *Runner) finishClaim(ctx context.Context, benefit platform.DropBenefit, channel string) {
	r.stopWatch(ctx)

	r.mu.Lock()
	var target *plan.Target
	if r.current != nil {
		t := *r.current
		target = &t
	}
	r.mu.Unlock()
	r.recordClaimed(ctx, target, benefit, channel, nil)

	r.mu.Lock()
	r.current = nil
	r.unpinLocked(benefit.ID)
	r.mu.Unlock()
	r.setState(ctx, StatePicking)
}

// recordClaimed settles the bookkeeping of a successful claim: it marks target
// as last claimed for reward codes, records it in the claim log, logs it and
// sends the "claim" notification with extra merged in. A nil target is only
// logged and notified.
func (r *Runner) recordClaimed(ctx context.Context, target *plan.Target, benefit platform.DropBenefit, channel string, extra map[string]any) {
	if target != nil {
		t := *target
		r.mu.Lock()
		r.lastClaimed, r.lastClaimedAt = &t, time.Now()
		r.mu.Unlock()
	}
	// A failed record is logged, never retried: a DB hiccup must not re-claim.
	if r.cfg.Claims != nil && target != nil {
		// Detached: a cancelled run ctx must not drop the claim log row.
		rctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		err := r.cfg.Claims.RecordClaim(rctx, r.cfg.AccountID, *target, "")
		cancel()
		if err != nil {
			slog.Warn("runner record claim failed", "kind", "error", "account", r.cfg.AccountID,
				"benefit", benefit.ID, "err", err)
		}
	}

	slog.Info("runner claim recorded", "kind", "claim", "account", r.cfg.AccountID,
		"benefit", benefit.ID, "benefit_name", benefit.Name, "channel", channel)
	if r.cfg.Notifier != nil {
		// A claim implies the requirement was met, so the bar reads full.
		f := map[string]any{
			"drop": benefit.Name, "channel": channel,
			"cur_min": benefit.RequiredMinutes, "req_min": benefit.RequiredMinutes,
		}
		for k, v := range extra {
			f[k] = v
		}
		_ = r.cfg.Notifier.Notify(ctx, "claim", r.notifyFields(f))
	}
}

// notifySwept fires the "claim" notification for a reward the multi-reward
// sweep picked up. It skips the actively mined benefit (claim notifies it) and
// dedupes by title (ClaimedReward has no ID) so a resurfacing reward fires once.
func (r *Runner) notifySwept(ctx context.Context, cr platform.ClaimedReward) {
	if cr.Title == "" {
		return
	}
	r.mu.Lock()
	if r.current != nil && r.current.Benefit.Name == cr.Title {
		r.mu.Unlock()
		return
	}
	if _, done := r.notifiedSwept[cr.Title]; done {
		r.mu.Unlock()
		return
	}
	if r.notifiedSwept == nil {
		r.notifiedSwept = map[string]struct{}{}
	}
	r.notifiedSwept[cr.Title] = struct{}{}
	// The backend's game is not trusted: resolve the title to a known target.
	target, ambiguous := r.resolveSweptLocked(cr.Title)
	r.mu.Unlock()
	if ambiguous {
		slog.Info("runner swept reward title matches several drops; left to the next scan",
			"kind", "claim", "account", r.cfg.AccountID, "title", cr.Title)
	}

	extra := map[string]any{"drop": cr.Title}
	if target != nil {
		if g := target.Campaign.Game; g != "" {
			extra["game"] = g
		}
		if r.cfg.Claims != nil {
			// Detached: a cancelled run ctx must not drop the claim log row.
			rctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			err := r.cfg.Claims.RecordClaim(rctx, r.cfg.AccountID, *target, "")
			cancel()
			if err != nil {
				slog.Warn("runner record swept claim failed", "kind", "error", "account", r.cfg.AccountID,
					"benefit", target.Benefit.ID, "err", err)
			}
		}
	}
	if r.cfg.Notifier != nil {
		f := r.notifyFields(extra)
		// The sibling is not the mined drop: drop its campaign and image.
		delete(f, "campaign")
		delete(f, "image")
		if _, ok := extra["game"]; !ok {
			delete(f, "game") // unresolved title: the current game does not apply
		}
		_ = r.cfg.Notifier.Notify(ctx, "claim", f)
	}
}

// resolveSweptLocked maps a swept reward title to one planned target. It looks
// in the current campaign, then the current game, then every queued and
// blocked target, and stops at the first level with a match. Several distinct
// matches at that level are ambiguous: no target. Caller holds r.mu.
func (r *Runner) resolveSweptLocked(title string) (target *plan.Target, ambiguous bool) {
	var all []plan.Target
	for _, t := range r.queue {
		if t.Benefit.Name == title {
			all = append(all, t)
		}
	}
	for _, b := range r.blocked {
		if b.Benefit.Name == title {
			all = append(all, b.Target)
		}
	}
	levels := []func(plan.Target) bool{func(plan.Target) bool { return true }}
	if cur := r.current; cur != nil {
		levels = []func(plan.Target) bool{
			func(t plan.Target) bool { return t.Campaign.ID == cur.Campaign.ID },
			func(t plan.Target) bool { return t.Campaign.Game == cur.Campaign.Game },
			levels[0],
		}
	}
	for _, in := range levels {
		var hit *plan.Target
		for i := range all {
			if !in(all[i]) {
				continue
			}
			if hit != nil && hit.Benefit.ID != all[i].Benefit.ID {
				return nil, true
			}
			hit = &all[i]
		}
		if hit != nil {
			t := *hit
			return &t, false
		}
	}
	return nil, false
}

// reapRewards claims one-click reward campaigns (RewardClaimer backends) for
// the chosen games. The backend only clicks visible Claim buttons, so
// over-firing is harmless.
func (r *Runner) reapRewards(ctx context.Context, camps []platform.Campaign) {
	rc, ok := r.cfg.Backend.(platform.RewardClaimer)
	if !ok {
		return
	}
	var games []string
	seen := map[string]bool{}
	for _, c := range camps {
		if _, chosen := plan.Match(r.cfg.Games, c.Game); !chosen {
			continue
		}
		if c.Kind != "reward" && len(c.Benefits) != 0 {
			continue
		}
		if !seen[c.Game] {
			seen[c.Game] = true
			games = append(games, c.Game)
		}
	}
	if len(games) == 0 {
		return
	}
	claimed, err := rc.ClaimRewards(ctx, r.cfg.Session, games)
	if err != nil {
		slog.Warn("runner reward reaper failed", "kind", "error", "account", r.cfg.AccountID, "err", err)
		return
	}
	if len(claimed) == 0 {
		slog.Info("runner reward reaper: nothing to claim", "kind", "discovery",
			"account", r.cfg.AccountID, "games", games)
		return
	}
	for _, cr := range claimed {
		slog.Info("runner reward claimed", "kind", "claim", "account", r.cfg.AccountID,
			"game", cr.Game, "title", cr.Title)
	}
}

// claimReady claims inventory drops that are fully watched (ReadyDropLister
// backends, Twitch) while force watching, whatever their game. Each is
// recorded and notified like a normal claim. A drop the claim log already
// holds, or this runner already claimed, is left alone: the inventory can lag
// a claim. A failed claim waits recheckInterval and is given up after
// claimFailSkipThreshold failures. A link refusal sends the link notice once
// and is given up at once. A drop whose campaign the platform reports as
// needing an account link is not attempted. The claim row and notice carry
// the campaign's name and game.
func (r *Runner) claimReady(ctx context.Context, channel string) {
	rl, ok := r.cfg.Backend.(platform.ReadyDropLister)
	if !ok {
		return
	}
	drops, err := rl.ReadyDrops(ctx, r.cfg.Session)
	if err != nil {
		slog.Debug("runner ready drops read failed", "kind", "error", "account", r.cfg.AccountID, "err", err)
		return
	}
	if len(drops) == 0 {
		return
	}
	var logged map[string]bool
	if r.cfg.Claims != nil {
		logged, err = r.cfg.Claims.ClaimedBenefitIDs(ctx, r.cfg.AccountID)
		if err != nil {
			// Claiming without the log could claim a drop twice.
			slog.Warn("runner claim log read failed", "kind", "error", "account", r.cfg.AccountID, "err", err)
			return
		}
	}
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
			// The claim would be refused until the account is linked.
			slog.Debug("runner ready drop skipped: campaign needs account link", "kind", "claim",
				"account", r.cfg.AccountID, "benefit", d.ID, "campaign", camp.ID)
			continue
		}
		now := time.Now()
		r.mu.Lock()
		skip := logged[d.ID] || r.forceClaimed[d.ID] ||
			r.forceClaimFails[d.ID] >= claimFailSkipThreshold || now.Before(r.forceClaimRetry[d.ID])
		r.mu.Unlock()
		if skip {
			continue
		}
		if err := r.cfg.Backend.Claim(ctx, r.cfg.Session, d); err != nil {
			if errors.Is(err, context.Canceled) || ctx.Err() != nil {
				return
			}
			r.readyClaimFailed(ctx, d, err, now)
			continue
		}
		t := plan.Target{Campaign: camp, Benefit: d, Minutes: d.RequiredMinutes}
		r.mu.Lock()
		delete(r.forceClaimRetry, d.ID)
		delete(r.forceClaimFails, d.ID)
		if r.forceClaimed == nil {
			r.forceClaimed = map[string]bool{}
		}
		r.forceClaimed[d.ID] = true
		r.mu.Unlock()
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
		r.recordClaimed(ctx, &t, d, channel, extra)
	}
}

// readyClaimFailed counts a failed ready-drop claim and sets when it may be
// retried. A link refusal notifies the user and gives the drop up.
func (r *Runner) readyClaimFailed(ctx context.Context, d platform.DropBenefit, err error, now time.Time) {
	needsLink := errors.Is(err, platform.ErrClaimNeedsLink)
	r.mu.Lock()
	if r.forceClaimRetry == nil {
		r.forceClaimRetry = map[string]time.Time{}
	}
	if r.forceClaimFails == nil {
		r.forceClaimFails = map[string]int{}
	}
	r.forceClaimRetry[d.ID] = now.Add(recheckInterval)
	r.forceClaimFails[d.ID]++
	if needsLink {
		r.forceClaimFails[d.ID] = claimFailSkipThreshold
	}
	n := r.forceClaimFails[d.ID]
	r.mu.Unlock()
	if needsLink {
		slog.Warn("runner ready drop claim needs account link; not retried", "kind", "claim",
			"account", r.cfg.AccountID, "benefit", d.ID, "benefit_name", d.Name, "err", err)
		r.notifyNeedsLink(ctx, d)
		return
	}
	if n >= claimFailSkipThreshold {
		slog.Warn("runner ready drop claim failed repeatedly; giving up", "kind", "claim",
			"account", r.cfg.AccountID, "benefit", d.ID, "benefit_name", d.Name, "failures", n, "err", err)
		return
	}
	slog.Warn("runner ready drop claim failed", "kind", "claim", "account", r.cfg.AccountID,
		"benefit", d.ID, "benefit_name", d.Name, "err", err)
}
