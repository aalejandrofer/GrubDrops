package runner

import (
	"context"
	"log/slog"
	"math/rand/v2"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/plan"
	"github.com/aalejandrofer/grubdrops/internal/platform"
)

var raidJoinDelay = func() time.Duration { return time.Duration(2+rand.IntN(7)) * time.Second }

// Real backends copy the hooks into their PubSub client once, at the
// account's first bootstrap, and ignore later SetAccountPubSubHooks calls.
// So each (backend, account) gets one trampoline, registered once, whose
// hooks forward to whichever Runner currently owns the account.
type hookKey struct {
	backend   platform.PubSubAware
	accountID string
}

type trampoline struct {
	cur atomic.Pointer[Runner] // nil = drop pushes
}

var (
	trampMu sync.Mutex
	tramps  = map[hookKey]*trampoline{}
)

func (t *trampoline) hooks() platform.PubSubHooks {
	return platform.PubSubHooks{
		OnDropProgress: func(dropID string, curMin, reqMin int64) {
			if r := t.cur.Load(); r != nil {
				r.onDropProgress(dropID, curMin, reqMin)
			}
		},
		OnDropClaim: func(dropID, instanceID string) {
			if r := t.cur.Load(); r != nil {
				r.onDropClaim(dropID, instanceID)
			}
		},
		OnStreamDown: func(channelID string) {
			if r := t.cur.Load(); r != nil {
				r.onStreamDown(channelID)
			}
		},
		OnStreamUp: func(string) {},
		OnRaid: func(channelID, raidID string) {
			if r := t.cur.Load(); r != nil {
				r.onRaid(channelID, raidID)
			}
		},
		OnRewardCode: func(notificationID, code, title string) {
			if r := t.cur.Load(); r != nil {
				r.onRewardCode(notificationID, code, title)
			}
		},
	}
}

// registerPubSub points the account's trampoline at r, registering it with
// the backend the first time. New calls it before any backend call so a lazy
// PubSub bootstrap captures the trampoline. Hooks fire from the backend's
// goroutine, so they take r.mu briefly and never hold it across backend,
// store or notifier calls.
func (r *Runner) registerPubSub() {
	pa, ok := r.cfg.Backend.(platform.PubSubAware)
	if !ok {
		return
	}
	if !reflect.TypeOf(pa).Comparable() {
		// Cannot key the registry; register directly (hooks freeze at bootstrap).
		pa.SetAccountPubSubHooks(r.cfg.AccountID, r.directHooks())
		return
	}
	key := hookKey{backend: pa, accountID: r.cfg.AccountID}
	trampMu.Lock()
	defer trampMu.Unlock()
	t := tramps[key]
	if t == nil {
		t = &trampoline{}
		tramps[key] = t
		pa.SetAccountPubSubHooks(r.cfg.AccountID, t.hooks())
	}
	t.cur.Store(r)
	r.tramp = t
}

func (r *Runner) directHooks() platform.PubSubHooks {
	return platform.PubSubHooks{
		OnDropProgress: r.onDropProgress,
		OnDropClaim:    r.onDropClaim,
		OnStreamDown:   r.onStreamDown,
		OnStreamUp:     func(string) {},
		OnRaid:         r.onRaid,
		OnRewardCode:   r.onRewardCode,
	}
}

// Release stops PubSub pushes reaching r. It only unhooks the account if r
// still owns it, so releasing a replaced Runner leaves its successor wired.
// Pool calls it when a Runner leaves the roster.
func (r *Runner) Release() {
	if r.tramp != nil {
		r.tramp.cur.CompareAndSwap(r, nil)
		return
	}
	if pa, ok := r.cfg.Backend.(platform.PubSubAware); ok {
		pa.SetAccountPubSubHooks(r.cfg.AccountID, platform.PubSubHooks{})
	}
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
	r.current.Minutes = int(curMin)
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

// onStreamDown swaps channel at once when the watched stream ends. A force
// watch has no target to restart, so it re-picks.
func (r *Runner) onStreamDown(channelID string) {
	r.mu.Lock()
	if r.stream != nil && r.handle != nil && r.stream.ChannelID == channelID {
		next := StateStarting
		if r.state == StateForceWatching {
			next = StatePicking
		}
		r.mu.Unlock()
		r.stopForceWatch(context.Background())
		r.setState(context.Background(), next)
		return
	}
	for i, extra := range r.extraWatches {
		if extra.stream.ChannelID == channelID {
			r.extraWatches = append(r.extraWatches[:i], r.extraWatches[i+1:]...)
			r.extraChannels = append(r.extraChannels[:i], r.extraChannels[i+1:]...)
			r.extraRetryAt = time.Now().Add(recheckInterval)
			r.mu.Unlock()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := r.cfg.Backend.StopWatch(ctx, extra.handle); err != nil {
				slog.Warn("runner extra force stop failed", "kind", "error", "account", r.cfg.AccountID, "err", err)
			}
			if cs, ok := r.cfg.Backend.(platform.ChannelSubscriber); ok {
				cs.UnsubscribeChannel(r.cfg.AccountID, extra.stream.ChannelID)
			}
			r.changed()
			return
		}
	}
	r.mu.Unlock()
}

func (r *Runner) onRaid(sourceID string, raidID string) {
	if raidID == "" || r.cfg.FollowRaids == nil || !r.cfg.FollowRaids() {
		return
	}
	r.mu.Lock()
	ctx := r.runCtx
	active := r.activeForceSourceLocked(sourceID)
	r.mu.Unlock()
	joiner, ok := r.cfg.Backend.(platform.RaidJoiner)
	if !ok || ctx == nil || !active {
		return
	}
	go func() {
		timer := time.NewTimer(raidJoinDelay())
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		if !r.cfg.FollowRaids() {
			return
		}
		r.mu.Lock()
		active := r.activeForceSourceLocked(sourceID)
		r.mu.Unlock()
		if !active {
			return
		}
		if err := joiner.JoinRaid(ctx, r.cfg.Session, raidID); err != nil && ctx.Err() == nil {
			slog.Warn("runner raid join failed", "kind", "error", "account", r.cfg.AccountID, "err", err)
		}
	}()
}

func (r *Runner) activeForceSourceLocked(sourceID string) bool {
	if sourceID == "" || r.state != StateForceWatching || r.handle == nil {
		return false
	}
	if r.stream != nil && r.stream.ChannelID == sourceID {
		return true
	}
	for _, extra := range r.extraWatches {
		if extra.stream.ChannelID == sourceID {
			return true
		}
	}
	return false
}

// onRewardCode logs a redemption code and stores it against the target the
// runner last claimed, if that claim is under rewardCodeWindow old. It never
// uses current: after a claim, current is already the next drop. Each claim
// takes at most one code, so a second code in the window is not recorded.
func (r *Runner) onRewardCode(notificationID, code, body string) {
	r.mu.Lock()
	var claimed *plan.Target
	if r.lastClaimed != nil && time.Since(r.lastClaimedAt) <= rewardCodeWindow {
		t := *r.lastClaimed
		claimed = &t
		r.lastClaimed = nil
	}
	r.mu.Unlock()
	if claimed == nil {
		slog.Info("runner reward code with no recent claim; not recorded", "kind", "claim",
			"account", r.cfg.AccountID, "notification_id", notificationID, "code", code, "body", body)
		return
	}
	benefit := claimed.Benefit
	slog.Info("runner reward code captured", "kind", "claim", "account", r.cfg.AccountID,
		"notification_id", notificationID, "code", code,
		"benefit_id", benefit.ID, "benefit_name", benefit.Name)
	if r.cfg.Claims == nil || benefit.ID == "" {
		return
	}
	// Detached: the hook has no run ctx.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := r.cfg.Claims.RecordClaim(ctx, r.cfg.AccountID, *claimed, code); err != nil {
		slog.Warn("runner record claim with code failed", "kind", "error",
			"account", r.cfg.AccountID, "benefit", benefit.ID, "err", err)
	}
}
