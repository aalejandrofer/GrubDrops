// Package points keeps the cached Twitch channel-points balances fresh: an
// hourly read of every account and Force watch channel pair, sooner retries
// after a failed read, and balances the runner's claim loop learns for free.
package points

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/store"
)

// Defaults for the Refresher fields left zero.
const (
	DefaultEvery        = time.Hour
	DefaultRetry        = 5 * time.Minute
	DefaultRetryMax     = 30 * time.Minute
	DefaultSpacing      = 2 * time.Second
	DefaultTick         = time.Minute
	DefaultFetchTimeout = 10 * time.Second
	DefaultCooldown     = time.Minute
)

// Store is the balance cache; *store.PointsBalances satisfies it.
type Store interface {
	List(ctx context.Context) ([]store.PointsBalance, error)
	Get(ctx context.Context, accountID, channel string) (store.PointsBalance, bool, error)
	RecordOK(ctx context.Context, accountID, channel string, balance int64, at time.Time) error
	RecordErr(ctx context.Context, accountID, channel, code string, failures int, at time.Time) error
	Prune(ctx context.Context) (int64, error)
}

var _ Store = (*store.PointsBalances)(nil)

// Target is one account and Twitch channel login whose balance to keep.
type Target struct {
	AccountID, Channel string
}

// Refresher reads due balances one at a time, Spacing apart, so a long list
// never bursts gql.
type Refresher struct {
	Store Store
	// Targets lists the pairs to keep fresh now. Pairs left out are not read;
	// the store's Prune drops rows no longer assigned.
	Targets func(ctx context.Context) ([]Target, error)
	// Fetch reads one balance. platform.ErrNoPointsBalance marks the channel
	// unavailable and waits the full Every.
	Fetch func(ctx context.Context, accountID, channel string) (int, error)
	// OnChange is nil-safe; called after a pass or Record changed a cached
	// balance or error state.
	OnChange func()

	Every        time.Duration // refresh after a good read
	Retry        time.Duration // first retry after a failed read, doubled per failure
	RetryMax     time.Duration // retry backoff cap, below Every
	Spacing      time.Duration // pause between two reads in a pass
	Tick         time.Duration // how often Run looks for due pairs
	FetchTimeout time.Duration
	Cooldown     time.Duration // least time between two RefreshNow calls

	Now func() time.Time // nil = time.Now
	// Sleep waits d or until ctx ends, reporting whether the wait finished.
	// nil = a timer.
	Sleep func(ctx context.Context, d time.Duration) bool

	mu sync.Mutex
	// dueBefore makes every pair last checked before it due: RefreshNow's
	// time, cut to the store's whole seconds.
	dueBefore time.Time
	// lastManual is when RefreshNow last started a refresh.
	lastManual time.Time
	wake       chan struct{}
}

func or(d, def time.Duration) time.Duration {
	if d > 0 {
		return d
	}
	return def
}

func (r *Refresher) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r *Refresher) sleep(ctx context.Context, d time.Duration) bool {
	if r.Sleep != nil {
		return r.Sleep(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// Backoff is the wait after failures failed reads in a row: retry doubled
// per failure past the first, capped at max.
func Backoff(failures int, retry, max time.Duration) time.Duration {
	d := retry
	for i := 1; i < failures && d < max; i++ {
		d *= 2
	}
	return min(d, max)
}

// wakeC is the channel RefreshNow uses to wake Run.
func (r *Refresher) wakeC() chan struct{} {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.wake == nil {
		r.wake = make(chan struct{}, 1)
	}
	return r.wake
}

func (r *Refresher) forcedBefore() time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.dueBefore
}

// RefreshNow makes every current pair due and wakes Run without blocking.
// Pairs a pass in flight reads after the call count as read, so a running
// pass and the woken one never read a pair twice; repeat wakes coalesce.
// Within Cooldown of the last refresh it does nothing and reports false
// with the time left.
func (r *Refresher) RefreshNow() (bool, time.Duration) {
	wake := r.wakeC()
	now := r.now()
	r.mu.Lock()
	if !r.lastManual.IsZero() {
		if left := r.lastManual.Add(or(r.Cooldown, DefaultCooldown)).Sub(now); left > 0 {
			r.mu.Unlock()
			return false, left
		}
	}
	r.lastManual = now
	r.dueBefore = now.Truncate(time.Second)
	r.mu.Unlock()
	select {
	case wake <- struct{}{}:
	default: // a wake is already pending
	}
	return true, 0
}

// NextAt is when the cached balance b is due for another read.
func (r *Refresher) NextAt(b store.PointsBalance) time.Time {
	if b.Error == store.PointsErrFailed {
		return b.CheckedAt.Add(Backoff(b.Failures, or(r.Retry, DefaultRetry), or(r.RetryMax, DefaultRetryMax)))
	}
	return b.CheckedAt.Add(or(r.Every, DefaultEvery))
}

func key(accountID, channel string) [2]string { return [2]string{accountID, channel} }

// Pass prunes unassigned rows, then reads every due target, one account at a
// time. It returns how many reads it made.
func (r *Refresher) Pass(ctx context.Context) int {
	changed := false
	defer func() {
		if changed && r.OnChange != nil {
			r.OnChange()
		}
	}()
	if n, err := r.Store.Prune(ctx); err != nil {
		slog.Warn("points balances: prune failed", "kind", "error", "err", err)
	} else if n > 0 {
		changed = true
	}
	targets, err := r.Targets(ctx)
	if err != nil {
		slog.Warn("points balances: list targets failed", "kind", "error", "err", err)
		return 0
	}
	rows, err := r.Store.List(ctx)
	if err != nil {
		slog.Warn("points balances: read cache failed", "kind", "error", "err", err)
		return 0
	}
	cached := make(map[[2]string]store.PointsBalance, len(rows))
	for _, b := range rows {
		cached[key(b.AccountID, b.Channel)] = b
	}
	// One account at a time; each account keeps its channel order.
	sort.SliceStable(targets, func(i, j int) bool { return targets[i].AccountID < targets[j].AccountID })
	reads := 0
	for _, t := range targets {
		if ctx.Err() != nil {
			return reads
		}
		b, ok := cached[key(t.AccountID, t.Channel)]
		// Read per pair, so a RefreshNow during this pass covers the rest.
		forced := ok && b.CheckedAt.Before(r.forcedBefore())
		if ok && !forced && r.now().Before(r.NextAt(b)) {
			continue
		}
		if reads > 0 && !r.sleep(ctx, or(r.Spacing, DefaultSpacing)) {
			return reads
		}
		reads++
		// A forced read is a change even with the same balance, so the
		// UI that asked hears back.
		if r.fetch(ctx, t, b, ok) || forced {
			changed = true
		}
	}
	return reads
}

// fetch reads one balance and stores the outcome. It reports whether the
// cached balance or error state changed.
func (r *Refresher) fetch(ctx context.Context, t Target, prev store.PointsBalance, had bool) bool {
	fctx, cancel := context.WithTimeout(ctx, or(r.FetchTimeout, DefaultFetchTimeout))
	bal, err := r.Fetch(fctx, t.AccountID, t.Channel)
	cancel()
	if ctx.Err() != nil {
		return false // shutting down: the read says nothing about the channel
	}
	at := r.now()
	switch {
	case err == nil:
		if werr := r.Store.RecordOK(ctx, t.AccountID, t.Channel, int64(bal), at); werr != nil {
			slog.Warn("points balances: save failed", "kind", "error", "account", t.AccountID, "channel", t.Channel, "err", werr)
			return false
		}
		return !had || prev.Balance == nil || *prev.Balance != int64(bal) || prev.Error != ""
	case errors.Is(err, platform.ErrNoPointsBalance):
		if werr := r.Store.RecordErr(ctx, t.AccountID, t.Channel, store.PointsErrUnavailable, 0, at); werr != nil {
			slog.Warn("points balances: save failed", "kind", "error", "account", t.AccountID, "channel", t.Channel, "err", werr)
			return false
		}
		return !had || prev.Error != store.PointsErrUnavailable
	default:
		failures := 1
		if had && prev.Error == store.PointsErrFailed {
			failures = prev.Failures + 1
		}
		if failures == 1 {
			slog.Warn("points balances: read failed; retrying sooner", "kind", "error",
				"account", t.AccountID, "channel", t.Channel, "err", err)
		} else {
			slog.Debug("points balances: read failed again", "account", t.AccountID,
				"channel", t.Channel, "failures", failures, "err", err)
		}
		if werr := r.Store.RecordErr(ctx, t.AccountID, t.Channel, store.PointsErrFailed, failures, at); werr != nil {
			slog.Warn("points balances: save failed", "kind", "error", "account", t.AccountID, "channel", t.Channel, "err", werr)
			return false
		}
		return !had || prev.Error != store.PointsErrFailed
	}
}

// Record stores a balance learned elsewhere, e.g. the runner's claim loop,
// so the hourly read is not needed. It reports whether the cached balance
// or error state changed.
func Record(ctx context.Context, st Store, accountID, channel string, balance int, at time.Time) (bool, error) {
	prev, had, err := st.Get(ctx, accountID, channel)
	if err != nil {
		return false, err
	}
	if err := st.RecordOK(ctx, accountID, channel, int64(balance), at); err != nil {
		return false, err
	}
	return !had || prev.Balance == nil || *prev.Balance != int64(balance) || prev.Error != "", nil
}

// Run passes at once, then every Tick or on RefreshNow, until ctx ends.
func (r *Refresher) Run(ctx context.Context) {
	wake := r.wakeC()
	r.Pass(ctx)
	t := time.NewTicker(or(r.Tick, DefaultTick))
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			r.Pass(ctx)
		case <-wake:
			r.Pass(ctx)
		}
	}
}
