package runner

import (
	"context"
	"log/slog"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// registerPubSub installs the push hooks. New calls it before any backend
// call so a lazy PubSub bootstrap picks them up. Hooks fire from the
// backend's goroutine, so they take r.mu briefly and never hold it across
// backend, store or notifier calls.
func (r *Runner) registerPubSub() {
	pa, ok := r.cfg.Backend.(platform.PubSubAware)
	if !ok {
		return
	}
	pa.SetAccountPubSubHooks(r.cfg.AccountID, platform.PubSubHooks{
		OnDropProgress: r.onDropProgress,
		OnDropClaim:    r.onDropClaim,
		OnStreamDown:   r.onStreamDown,
		OnStreamUp:     func(string) {},
		OnRewardCode:   r.onRewardCode,
	})
}

// onDropProgress records pushed minutes for the current benefit so the
// snapshot moves without waiting for the next inventory poll.
func (r *Runner) onDropProgress(dropID string, curMin, _ int64) {
	r.mu.Lock()
	if r.current == nil || r.current.Benefit.ID != dropID {
		r.mu.Unlock()
		return
	}
	if int(curMin) > r.lastMin {
		r.noAdvance = 0
	}
	r.lastMin = int(curMin)
	r.minutes = int(curMin)
	r.noProgress = 0
	r.mu.Unlock()
	r.changed()
}

// onDropClaim captures the instance ID and fast-paths to claiming.
func (r *Runner) onDropClaim(dropID, instanceID string) {
	r.mu.Lock()
	if r.current == nil || r.current.Benefit.ID != dropID {
		r.mu.Unlock()
		return
	}
	if instanceID != "" {
		r.current.Benefit.InstanceID = instanceID
	}
	r.mu.Unlock()
	r.setState(context.Background(), StateClaiming)
}

// onStreamDown swaps channel at once when the watched stream ends.
func (r *Runner) onStreamDown(channelID string) {
	r.mu.Lock()
	if r.stream == nil || r.stream.ChannelID != channelID || r.handle == nil {
		r.mu.Unlock()
		return
	}
	r.mu.Unlock()
	r.stopWatch(context.Background())
	r.setState(context.Background(), StateStarting)
}

// onRewardCode logs a redemption code and stores it against the current
// benefit. Without a current benefit it is logged only.
func (r *Runner) onRewardCode(notificationID, code, _ string) {
	r.mu.Lock()
	var benefit platform.DropBenefit
	if r.current != nil {
		benefit = r.current.Benefit
	}
	r.mu.Unlock()
	slog.Info("runner reward code captured", "kind", "claim", "account", r.cfg.AccountID,
		"notification_id", notificationID, "code", code,
		"benefit_id", benefit.ID, "benefit_name", benefit.Name)
	if r.cfg.Claims == nil || benefit.ID == "" {
		return
	}
	// Detached: the hook has no run ctx.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := r.cfg.Claims.RecordClaimWithCode(ctx, r.cfg.AccountID, benefit, code); err != nil {
		slog.Warn("runner record claim with code failed", "kind", "error",
			"account", r.cfg.AccountID, "benefit", benefit.ID, "err", err)
	}
}
