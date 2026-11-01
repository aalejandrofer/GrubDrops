// Package rewards keeps each Twitch account's reward codes fresh: a read at
// start and every few hours, one account at a time, plus a notification for
// every new code and a reminder before it expires. Codes are secrets: this
// package never logs one.
package rewards

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/i18n"
	"github.com/aalejandrofer/grubdrops/internal/notify"
	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/store"
)

// Defaults for the Poller fields left zero.
const (
	DefaultEvery            = 3 * time.Hour
	DefaultRetry            = 15 * time.Minute
	DefaultUnavailableRetry = 24 * time.Hour
	DefaultSpacing          = 30 * time.Second
	DefaultTick             = time.Minute
	DefaultReadTimeout      = 2 * time.Minute
	// DefaultRemindBefore is how long before expiry the reminder goes out.
	DefaultRemindBefore = 48 * time.Hour
	// DefaultNotifyRetry is the wait after a failed send before the next try.
	DefaultNotifyRetry = 15 * time.Minute
)

// Store is the reward code store; *store.RewardCodes satisfies it.
type Store interface {
	Save(ctx context.Context, accountID string, rc platform.RewardCode, now time.Time) (store.RewardCode, bool, error)
	List(ctx context.Context, openOnly bool) ([]store.RewardCode, error)
	MarkNotified(ctx context.Context, id int64, at time.Time) error
	MarkNotifySkipped(ctx context.Context, id int64, at time.Time) error
	MarkReminded(ctx context.Context, id int64, at time.Time) error
}

var _ Store = (*store.RewardCodes)(nil)

// Reader reads one account's reward codes; see platform.RewardCodeReader.
type Reader func(ctx context.Context, known func(key string) (code, expiry bool)) ([]platform.RewardCode, error)

// Target is one account to read.
type Target struct {
	AccountID string
	Read      Reader
}

// Notifier sends one notification event.
type Notifier interface {
	Notify(ctx context.Context, event string, fields map[string]any) error
}

// Poller reads every target's reward codes Every apart and sends the
// notifications they call for.
type Poller struct {
	Store   Store
	Targets func(ctx context.Context) ([]Target, error)
	// Notifier gets the new-code and reminder events; nil sends nothing and
	// marks nothing.
	Notifier Notifier
	// AccountName labels an account in notifications; nil uses the id.
	AccountName func(ctx context.Context, accountID string) string
	// Lang is the notification language; nil = en.
	Lang func(ctx context.Context) string
	// Zone formats expiry times; nil = time.Local.
	Zone func() *time.Location
	// OnChange is nil-safe; called after a pass stored a new reward.
	OnChange func()

	Every            time.Duration // between two reads of one account
	Retry            time.Duration // after a failed read
	UnavailableRetry time.Duration // after the login was refused
	Spacing          time.Duration // between two reads in one pass
	Tick             time.Duration // how often Run looks for due accounts
	ReadTimeout      time.Duration
	RemindBefore     time.Duration
	NotifyRetry      time.Duration

	Now   func() time.Time
	Sleep func(ctx context.Context, d time.Duration) bool

	mu         sync.Mutex
	accounts   map[string]*accountState
	notifyWait time.Time // no send before this, after a failed one
}

// accountState is one account's read schedule. It lives in memory, so every
// start reads each account once.
type accountState struct {
	next        time.Time
	failures    int
	unavailable bool
}

func or(d, def time.Duration) time.Duration {
	if d > 0 {
		return d
	}
	return def
}

func (p *Poller) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

func (p *Poller) sleep(ctx context.Context, d time.Duration) bool {
	if p.Sleep != nil {
		return p.Sleep(ctx, d)
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

func (p *Poller) state(id string) *accountState {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.accounts == nil {
		p.accounts = map[string]*accountState{}
	}
	st, ok := p.accounts[id]
	if !ok {
		st = &accountState{}
		p.accounts[id] = st
	}
	return st
}

// Unavailable reports whether the account's login was refused at its last
// read.
func (p *Poller) Unavailable(accountID string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	st, ok := p.accounts[accountID]
	return ok && st.unavailable
}

// Pass reads every due target, one at a time, Spacing apart, then sends
// due notifications. It returns how many reads it made.
func (p *Poller) Pass(ctx context.Context) int {
	reads := p.readDue(ctx)
	if ctx.Err() == nil {
		p.Sweep(ctx)
	}
	return reads
}

func (p *Poller) readDue(ctx context.Context) int {
	targets, err := p.Targets(ctx)
	if err != nil {
		slog.Warn("reward codes: list accounts failed", "kind", "error", "err", err)
		return 0
	}
	reads := 0
	changed := false
	for _, t := range targets {
		if ctx.Err() != nil {
			break
		}
		st := p.state(t.AccountID)
		if p.now().Before(st.next) {
			continue
		}
		if reads > 0 && !p.sleep(ctx, or(p.Spacing, DefaultSpacing)) {
			break
		}
		reads++
		if p.read(ctx, t, st) {
			changed = true
		}
	}
	if changed && p.OnChange != nil {
		p.OnChange()
	}
	return reads
}

// read reads one account and stores what it found. It reports whether a new
// reward was stored.
func (p *Poller) read(ctx context.Context, t Target, st *accountState) bool {
	stored, err := p.Store.List(ctx, false)
	if err != nil {
		slog.Warn("reward codes: read store failed", "kind", "error", "err", err)
		st.next = p.now().Add(or(p.Retry, DefaultRetry))
		return false
	}
	type have struct{ code, expiry bool }
	known := map[string]have{}
	for _, r := range stored {
		if r.AccountID == t.AccountID {
			known[r.Key] = have{r.Code != "", !r.ExpiresAt.IsZero()}
		}
	}
	rctx, cancel := context.WithTimeout(ctx, or(p.ReadTimeout, DefaultReadTimeout))
	list, err := t.Read(rctx, func(key string) (bool, bool) {
		h := known[key]
		return h.code, h.expiry
	})
	cancel()
	if ctx.Err() != nil {
		return false
	}
	now := p.now()
	switch {
	case errors.Is(err, platform.ErrRewardCodesUnavailable):
		if !st.unavailable {
			slog.Info("reward codes: this login cannot read reward codes; trying again in a day",
				"account", t.AccountID)
		}
		st.unavailable = true
		st.failures = 0
		st.next = now.Add(or(p.UnavailableRetry, DefaultUnavailableRetry))
		return false
	case err != nil:
		st.failures++
		if st.failures == 1 {
			slog.Warn("reward codes: read failed; retrying sooner", "kind", "error", "account", t.AccountID, "err", err)
		} else {
			slog.Debug("reward codes: read failed again", "account", t.AccountID, "failures", st.failures, "err", err)
		}
		st.next = now.Add(min(or(p.Retry, DefaultRetry)<<min(st.failures-1, 8), or(p.Every, DefaultEvery)))
		return false
	}
	if st.unavailable {
		slog.Info("reward codes: this login reads reward codes again", "account", t.AccountID)
	}
	st.unavailable = false
	st.failures = 0
	st.next = now.Add(or(p.Every, DefaultEvery))
	added := 0
	for _, rc := range list {
		_, isNew, err := p.Store.Save(ctx, t.AccountID, rc, now)
		if err != nil {
			slog.Warn("reward codes: save failed", "kind", "error", "account", t.AccountID, "reward", rc.Key, "err", err)
			continue
		}
		if isNew {
			added++
		}
	}
	if added > 0 {
		slog.Info("reward codes: new rewards found", "account", t.AccountID, "count", added)
	}
	return added > 0
}

// Due says which notification a stored reward needs now.
type Due int

const (
	DueNone     Due = iota
	DueNew          // first notice of the code
	DueReminder     // the code expires within the reminder window
	DueSkip         // first seen already expired: mark it skipped, send nothing
)

// Needs says which notification r needs at now with reminders remind
// before expiry. A reward without a code needs none, and neither does one
// whose new-code notice was skipped.
func Needs(r store.RewardCode, now time.Time, remind time.Duration) Due {
	if !r.Open() || r.Code == "" || !r.NotifySkippedAt.IsZero() {
		return DueNone
	}
	expired := !r.ExpiresAt.IsZero() && !now.Before(r.ExpiresAt)
	if r.NotifiedAt.IsZero() {
		if expired {
			return DueSkip
		}
		return DueNew
	}
	if r.RemindedAt.IsZero() && !r.ExpiresAt.IsZero() && !expired && !now.Before(r.ExpiresAt.Add(-remind)) {
		return DueReminder
	}
	return DueNone
}

// Sweep sends every due new-code notification and reminder, each once. A
// sent notification is recorded in the store, so restarts never repeat it.
// A new code that no notification target takes, or that was first seen
// already expired, is recorded as skipped and never sent. After a failed
// send the next try waits NotifyRetry.
func (p *Poller) Sweep(ctx context.Context) {
	if p.Notifier == nil {
		return
	}
	now := p.now()
	p.mu.Lock()
	wait := now.Before(p.notifyWait)
	p.mu.Unlock()
	if wait {
		return
	}
	open, err := p.Store.List(ctx, true)
	if err != nil {
		slog.Warn("reward codes: read store failed", "kind", "error", "err", err)
		return
	}
	remind := or(p.RemindBefore, DefaultRemindBefore)
	for _, r := range open {
		due := Needs(r, now, remind)
		switch due {
		case DueNone:
			continue
		case DueSkip:
			p.skip(ctx, r.ID, now, "expired")
			continue
		case DueNew:
			if !notify.Enabled(ctx, p.Notifier, notify.EventRewardCode) {
				p.skip(ctx, r.ID, now, "notifications off")
				continue
			}
		}
		if err := p.Notifier.Notify(ctx, notify.EventRewardCode, p.fields(ctx, r, due, now)); err != nil {
			slog.Warn("reward codes: notification failed; trying again later", "kind", "error", "id", r.ID, "err", err)
			p.mu.Lock()
			p.notifyWait = now.Add(or(p.NotifyRetry, DefaultNotifyRetry))
			p.mu.Unlock()
			return
		}
		var werr error
		if due == DueNew {
			werr = p.Store.MarkNotified(ctx, r.ID, now)
			// A code first seen inside the window already shows its expiry.
			if werr == nil && !r.ExpiresAt.IsZero() && !now.Before(r.ExpiresAt.Add(-remind)) {
				werr = p.Store.MarkReminded(ctx, r.ID, now)
			}
		} else {
			werr = p.Store.MarkReminded(ctx, r.ID, now)
		}
		if werr != nil {
			slog.Warn("reward codes: save failed", "kind", "error", "id", r.ID, "err", werr)
		}
	}
}

// skip records that reward id's new-code notice was not sent, so it never is.
func (p *Poller) skip(ctx context.Context, id int64, now time.Time, reason string) {
	if err := p.Store.MarkNotifySkipped(ctx, id, now); err != nil {
		slog.Warn("reward codes: save failed", "kind", "error", "id", id, "err", err)
		return
	}
	slog.Debug("reward codes: new-code notification skipped", "kind", "info", "id", id, "reason", reason)
}

// fields builds the notification fields for r.
func (p *Poller) fields(ctx context.Context, r store.RewardCode, due Due, now time.Time) map[string]any {
	lang := "en"
	if p.Lang != nil {
		if l := p.Lang(ctx); l != "" {
			lang = l
		}
	}
	title := i18n.T(lang, "rewards.notify_new")
	if due == DueReminder {
		title = i18n.T(lang, "rewards.notify_reminder")
	}
	name := r.AccountID
	if p.AccountName != nil {
		if n := p.AccountName(ctx, r.AccountID); n != "" {
			name = n
		}
	}
	f := map[string]any{
		"title":         title,
		"reward":        r.Name,
		"reward_game":   r.Game,
		"account_label": name,
		"code":          r.Code,
		"redeem_url":    r.RedeemURL,
		"platform":      "twitch",
	}
	if !r.ExpiresAt.IsZero() {
		f["expires"] = ExpiryText(lang, r.ExpiresAt, now, p.zone())
	}
	return f
}

func (p *Poller) zone() *time.Location {
	if p.Zone != nil {
		if z := p.Zone(); z != nil {
			return z
		}
	}
	return time.Local
}

// ExpiryText is the expiry line of a notification: the local date and time
// and how long is left.
func ExpiryText(lang string, at, now time.Time, loc *time.Location) string {
	left := at.Sub(now)
	var rel string
	switch {
	case left >= 48*time.Hour:
		rel = withN(i18n.T(lang, "rewards.left_days"), int(left/(24*time.Hour)))
	case left >= time.Hour:
		rel = withN(i18n.T(lang, "rewards.left_hours"), int(left/time.Hour))
	default:
		rel = withN(i18n.T(lang, "rewards.left_minutes"), max(1, int(left/time.Minute)))
	}
	return at.In(loc).Format("2006-01-02 15:04 MST") + " · " + rel
}

// withN fills the {n} placeholder of a locale string.
func withN(s string, n int) string {
	return strings.ReplaceAll(s, "{n}", strconv.Itoa(n))
}

// Run passes at once, then every Tick, until ctx ends.
func (p *Poller) Run(ctx context.Context) {
	p.Pass(ctx)
	t := time.NewTicker(or(p.Tick, DefaultTick))
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			p.Pass(ctx)
		}
	}
}
