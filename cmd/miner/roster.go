package main

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/api"
	"github.com/aalejandrofer/grubdrops/internal/collect"
	"github.com/aalejandrofer/grubdrops/internal/plan"
	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/platform/kick"
	"github.com/aalejandrofer/grubdrops/internal/runner"
	"github.com/aalejandrofer/grubdrops/internal/store"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

// kickRegistry is the part of the Kick backend a roster build registers an
// account with.
type kickRegistry interface {
	RegisterSidecar(accountID, username string)
	RegisterChannels(accountID string, channels []string)
}

// roster builds the v2 per-account runners and wires them to the collector
// and the SSE hub. ctx is the root context: every build and reload uses it,
// never a request context.
type roster struct {
	ctx         context.Context
	q           *gen.Queries
	sessions    *store.SessionStore
	chosen      *store.ChosenGames
	own         *store.AccountGames // nil = every account mines the chosen list
	skips       *store.Skips
	linkNotices *store.LinkNotices
	collector   *collect.Collector
	hub         *api.Hub
	pool        *runner.Pool
	kick        kickRegistry // nil interface when Kick is not configured; see newRoster
	logger      *slog.Logger
	names       *accountNames // set by newRoster

	backendFor func(gen.Account) (platform.Backend, error)
	notifier   runner.Notifier
	settings   *store.Settings              // nil = default priority
	runCfg     *atomic.Pointer[runSettings] // set by newRoster; holds nil until read

	// gamesDirty queues one games reload; see requestGamesReload.
	gamesDirty chan struct{} // set by newRoster
}

// accountNames maps account ID to display name in memory, so the log hook can
// name an account without a DB read. The roster keeps it current.
type accountNames struct {
	mu sync.RWMutex
	m  map[string]string
}

func (n *accountNames) get(id string) string {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.m[id]
}

func (n *accountNames) set(id, name string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.m == nil {
		n.m = map[string]string{}
	}
	n.m[id] = name
}

func (n *accountNames) remove(id string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	delete(n.m, id)
}

func (n *accountNames) replace(accts []gen.Account) {
	m := make(map[string]string, len(accts))
	for _, a := range accts {
		m[a.ID] = store.AccountName(a)
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	n.m = m
}

// accountName returns the cached display name for id, or "".
func (ro *roster) accountName(id string) string { return ro.names.get(id) }

// newRoster returns r wired for claim events. k may be nil. A nil
// *kick.Backend stored straight in the kickRegistry interface would be a
// non-nil interface and panic on use, so the field is set only when k is
// non-nil.
func newRoster(r roster, k *kick.Backend) *roster {
	ro := &r
	ro.names = &accountNames{}
	ro.gamesDirty = make(chan struct{}, 1)
	ro.runCfg = &atomic.Pointer[runSettings]{}
	if k != nil {
		ro.kick = k
	}
	// Claims found during reconcile reach the hub the same way runner claims do.
	ro.collector.OnFound = func(ctx context.Context, e store.ClaimEntry) {
		ro.publishClaim(ctx, e.AccountID, e.BenefitID)
	}
	return ro
}

// publishClaim reads the claim log row back and publishes it as a claim event.
func (ro *roster) publishClaim(ctx context.Context, accountID, benefitID string) {
	e, err := ro.collector.Claims.Get(ctx, accountID, benefitID)
	if err != nil {
		ro.log().Error("read back claim failed", "kind", "error", "account", accountID, "benefit", benefitID, "err", err)
		return
	}
	var name string
	if a, err := ro.q.GetAccount(ctx, accountID); err == nil {
		name = store.AccountName(a)
	}
	ro.hub.Publish(api.Event{Name: api.EventClaim, Data: api.ClaimJSON(e, name)})
}

func (ro *roster) log() *slog.Logger {
	if ro.logger != nil {
		return ro.logger
	}
	return slog.Default()
}

func (ro *roster) idle(a gen.Account, st runner.State) runner.Entry {
	return runner.Entry{ID: a.ID, Unit: &runner.Idle{AccountID: a.ID, Platform: a.Platform, State: st}}
}

// builder returns the Builder for one account row.
func (ro *roster) builder(a gen.Account) runner.Builder {
	return func() runner.Entry {
		ctx := ro.ctx
		// Reread the row so a panic restart picks up a rename or login
		// change; the captured row is the fallback.
		a := a
		if fresh, err := ro.q.GetAccount(ctx, a.ID); err == nil {
			a = fresh
		}
		if a.Enabled == 0 {
			return ro.idle(a, runner.StateIdle)
		}
		b, err := ro.backendFor(a)
		if err != nil {
			ro.log().Error("no backend for account", "kind", "error", "account", a.ID, "platform", a.Platform, "err", err)
			return ro.idle(a, runner.StateNeedsLogin)
		}
		var verify func(ctx context.Context, s platform.Session) error
		if ac, ok := b.(platform.AuthChecker); ok {
			verify = ac.VerifyAuth
		}
		deps := sessionDeps{
			get:     ro.sessions.Get,
			refresh: b.RefreshSession,
			put:     ro.sessions.Put,
			logger:  ro.log(),
			backoff: sessionRetryBackoff,
			verify:  verify,
		}
		sess, reason, err := acquireSession(ctx, deps, a, time.Now())
		if err != nil {
			ro.log().Error("load session failed", "kind", "error", "account", a.ID, "err", err)
			return ro.idle(a, runner.StateNeedsLogin)
		}
		if reason != "" {
			return ro.idle(a, runner.StateNeedsLogin)
		}

		// Re-register Kick state each build so the in-memory maps survive
		// restarts and a freshly added account works without one.
		if a.Platform == "kick" && ro.kick != nil {
			ro.kick.RegisterSidecar(a.ID, a.DisplayName)
			if chs := decodeKickChannels(sess); len(chs) > 0 {
				ro.kick.RegisterChannels(a.ID, chs)
				ro.log().Info("kick channels restored from session", "account", a.ID, "channels", chs)
			}
		}

		r := runner.New(ro.runnerConfig(ctx, a, b, sess, ro.gamesFor(ctx, a)))
		return runner.Entry{ID: a.ID, Unit: r}
	}
}

// gamesFor is the ranked game list the account mines: its own list while
// own_games is on, else the global chosen list. A failed read logs and gives
// an empty list, which idles the runner as no_games.
func (ro *roster) gamesFor(ctx context.Context, a gen.Account) []plan.Game {
	var (
		list []store.ChosenGame
		err  error
	)
	if a.OwnGames != 0 && ro.own != nil {
		list, err = ro.own.List(ctx, a.ID)
	} else {
		list, err = ro.chosen.List(ctx)
	}
	if err != nil {
		ro.log().Error("load games failed", "kind", "error", "account", a.ID, "own", a.OwnGames != 0, "err", err)
	}
	games := make([]plan.Game, 0, len(list))
	for _, g := range list {
		games = append(games, plan.Game{Name: g.Name, Slug: g.Slug})
	}
	return games
}

// progressNotifyStepPct fires progress notifications at start, 50% and 100%,
// the 1.x default. The notify_progress toggle in Settings gates them.
const progressNotifyStepPct = 50

// runnerConfig is the runner.Config for one account build.
func (ro *roster) runnerConfig(ctx context.Context, a gen.Account, b platform.Backend, sess platform.Session, games []plan.Game) runner.Config {
	order := ro.pool.OrderFunc(a.ID)
	return runner.Config{
		AccountID: a.ID, AccountLabel: store.AccountName(a), Platform: a.Platform,
		Backend: b, Session: sess,
		Games:       games,
		Notifier:    ro.notifier,
		Claims:      &claimLog{ro: ro},
		Skips:       ro.skips,
		LinkNotices: ro.linkNotices,
		Order:       order,
		Priority:    ro.priorityMode,
		Lang:        ro.uiLanguage,
		ScanEvery:   ro.scanEvery,
		Preempt:     ro.preempt,
		OnScan: func(ctx context.Context, s runner.Scan) {
			ro.collector.OnScan(ctx, s)
			ro.hub.Publish(api.Event{Name: api.EventGames, Data: struct{}{}})
		},
		OnProgress: ro.collector.OnProgress,
		OnBeat:     ro.collector.OnBeat,
		OnChange: func(s runner.Snapshot) {
			// Read the row fresh so a rename shows; the captured row is
			// the fallback.
			row := a
			if fresh, err := ro.q.GetAccount(ro.ctx, a.ID); err == nil {
				row = fresh
			}
			ro.hub.Publish(api.Event{
				Name: api.EventRunner, Key: a.ID,
				Data: api.RunnerJSON(s, row, len(order()) > 0),
			})
		},
		ProgressNotifyStepPct: progressNotifyStepPct,
		TickInterval:          0,
	}
}

// claimLog adapts the Collector to runner.ClaimLog and announces each claim.
type claimLog struct{ ro *roster }

var _ runner.ClaimLog = (*claimLog)(nil)

func (l *claimLog) RecordClaim(ctx context.Context, accountID string, t plan.Target, code string) error {
	if err := l.ro.collector.RecordClaim(ctx, accountID, t, code); err != nil {
		return err
	}
	l.ro.publishClaim(ctx, accountID, t.Benefit.ID)
	return nil
}

func (l *claimLog) ClaimedBenefitIDs(ctx context.Context, accountID string) (map[string]bool, error) {
	return l.ro.collector.ClaimedBenefitIDs(ctx, accountID)
}

func (l *claimLog) MarkedBenefitIDs(ctx context.Context, accountID string) (map[string]bool, error) {
	return l.ro.collector.MarkedBenefitIDs(ctx, accountID)
}

// builders returns one Builder per account row, in ListAllAccounts order.
func (ro *roster) builders(_ context.Context) ([]runner.Builder, error) {
	accts, err := ro.q.ListAllAccounts(ro.ctx)
	if err != nil {
		return nil, err
	}
	ro.names.replace(accts)
	out := make([]runner.Builder, 0, len(accts))
	for _, a := range accts {
		out = append(out, ro.builder(a))
	}
	return out, nil
}

// reloadAccount rebuilds one account's runner from its row, or removes the
// runner when the row is gone.
func (ro *roster) reloadAccount(id string) {
	a, err := ro.q.GetAccount(ro.ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		ro.names.remove(id)
		ro.pool.ReloadAccount(id, nil)
		return
	}
	if err != nil {
		ro.log().Error("reload account: read row failed", "kind", "error", "account", id, "err", err)
		return
	}
	ro.names.set(a.ID, store.AccountName(a))
	ro.pool.ReloadAccount(id, ro.builder(a))
}

// accountRemoved drops a deleted account's runner and collected state.
func (ro *roster) accountRemoved(id string) {
	ro.names.remove(id)
	ro.pool.ReloadAccount(id, nil)
	ro.collector.Forget(id)
	ro.hub.Publish(api.Event{Name: api.EventAccounts, Data: struct{}{}})
}

// requestGamesReload queues a games reload and returns at once. A trigger
// that arrives while one is already queued folds into it.
func (ro *roster) requestGamesReload() {
	select {
	case ro.gamesDirty <- struct{}{}:
	default:
	}
}

// runGamesReloader runs queued games reloads one at a time until ro.ctx ends.
func (ro *roster) runGamesReloader() {
	for {
		select {
		case <-ro.ctx.Done():
			return
		case <-ro.gamesDirty:
			if ro.ctx.Err() != nil {
				return
			}
			ro.safeGamesChanged()
		}
	}
}

// safeGamesChanged keeps a builder panic from killing the reloader goroutine.
func (ro *roster) safeGamesChanged() {
	defer func() {
		if r := recover(); r != nil {
			ro.log().Error("games changed: reload panicked", "kind", "error", "panic", r)
		}
	}()
	ro.gamesChanged()
}

// gamesChanged rebuilds every runner on the global list so each picks up the
// new chosen games. Accounts on their own list keep running.
func (ro *roster) gamesChanged() {
	accts, err := ro.q.ListAllAccounts(ro.ctx)
	if err != nil {
		ro.log().Error("games changed: list accounts failed", "kind", "error", "err", err)
		return
	}
	for _, a := range accts {
		if ro.ctx.Err() != nil {
			return
		}
		if a.OwnGames != 0 && ro.own != nil {
			continue
		}
		ro.reloadAccount(a.ID)
	}
	ro.hub.Publish(api.Event{Name: api.EventGames, Data: struct{}{}})
}

// runSettings is the settings every runner reads on each tick and snapshot.
type runSettings struct {
	lang     string
	priority plan.Priority
	scan     time.Duration
	preempt  bool
}

// runSettings returns the cached runner settings, reading them once after
// start or after settingsChanged. A failed read gives the defaults for that
// call: en, ordered, the runner's own scan pace, preempt on.
func (ro *roster) runSettings() runSettings {
	if c := ro.runCfg.Load(); c != nil {
		return *c
	}
	c := runSettings{lang: "en", priority: plan.PriorityOrdered, preempt: true}
	if ro.settings != nil {
		ctx, cancel := context.WithTimeout(ro.ctx, 5*time.Second)
		defer cancel()
		lang, e1 := ro.settings.UILanguage(ctx)
		p, e2 := ro.settings.PriorityMode(ctx)
		n, e3 := ro.settings.ScanIntervalMin(ctx)
		on, e4 := ro.settings.Preempt(ctx)
		if err := errors.Join(e1, e2, e3, e4); err != nil {
			// Not cached: the next read tries again.
			ro.log().Warn("read runner settings failed", "kind", "error", "err", err)
			return c
		}
		c = runSettings{lang: lang, priority: plan.Priority(p), scan: time.Duration(n) * time.Minute, preempt: on}
	}
	ro.runCfg.Store(&c)
	return c
}

// settingsChanged drops the cached runner settings so the next read sees a
// saved change.
func (ro *roster) settingsChanged() { ro.runCfg.Store(nil) }

// uiLanguage is the UI language for notification text.
func (ro *roster) uiLanguage() string { return ro.runSettings().lang }

// priorityMode is the queue priority, applied on the next plan without
// rebuilding runners.
func (ro *roster) priorityMode() plan.Priority { return ro.runSettings().priority }

// scanEvery is the scan pace; 0 keeps the runner defaults.
func (ro *roster) scanEvery() time.Duration { return ro.runSettings().scan }

// preempt is the switch-to-higher-rank setting.
func (ro *roster) preempt() bool { return ro.runSettings().preempt }
