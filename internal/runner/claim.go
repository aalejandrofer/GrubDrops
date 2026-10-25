package runner

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/aalejandrofer/grubdrops/internal/plan"
	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// claimFailSkipThreshold: consecutive Claim failures before the benefit is
// skipped so one broken claim cannot wedge the queue.
const claimFailSkipThreshold = 3

// claim claims the completed current benefit.
func (r *Runner) claim(ctx context.Context) error {
	r.mu.Lock()
	if r.current == nil {
		r.mu.Unlock()
		return errors.New("cannot claim: no current target")
	}
	benefit := r.current.Benefit
	channel := r.channel
	if r.handle != nil && r.handle.Channel != "" {
		channel = r.handle.Channel
	}
	r.mu.Unlock()

	if err := r.cfg.Backend.Claim(ctx, r.cfg.Session, benefit); err != nil {
		if errors.Is(err, context.Canceled) || ctx.Err() != nil {
			return fmt.Errorf("claim: %w", err)
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
	r.stopWatch(ctx)

	// A failed record is logged, never retried: a DB hiccup must not re-claim.
	if r.cfg.Claims != nil {
		if err := r.cfg.Claims.RecordClaim(ctx, r.cfg.AccountID, benefit); err != nil {
			slog.Warn("runner record claim failed", "kind", "error", "account", r.cfg.AccountID,
				"benefit", benefit.ID, "err", err)
		}
	}

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

	slog.Info("runner claim recorded", "kind", "claim", "account", r.cfg.AccountID,
		"benefit", benefit.ID, "benefit_name", benefit.Name, "channel", channel)
	if r.cfg.Notifier != nil {
		// A claim implies the requirement was met, so the bar reads full.
		_ = r.cfg.Notifier.Notify(ctx, "claim", r.notifyFields(map[string]any{
			"drop": benefit.Name, "channel": channel,
			"cur_min": benefit.RequiredMinutes, "req_min": benefit.RequiredMinutes,
		}))
	}

	r.mu.Lock()
	r.current = nil
	r.mu.Unlock()
	r.setState(ctx, StatePicking)
	return nil
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
	r.mu.Unlock()

	extra := map[string]any{"drop": cr.Title}
	if cr.Game != "" {
		extra["game"] = cr.Game
	}
	if r.cfg.Notifier != nil {
		_ = r.cfg.Notifier.Notify(ctx, "claim", r.notifyFields(extra))
	}
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
