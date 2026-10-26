package main

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"strings"
	"sync"
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
	ctx       context.Context
	q         *gen.Queries
	sessions  *store.SessionStore
	chosen    *store.ChosenGames
	skips     *store.Skips
	collector *collect.Collector
	hub       *api.Hub
	pool      *runner.Pool
	kick      kickRegistry // nil interface when Kick is not configured; see newRoster
	logger    *slog.Logger
	names     *accountNames // set by newRoster

	backendFor func(gen.Account) (platform.Backend, error)
	notifier   runner.Notifier

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
		m[a.ID] = a.DisplayName
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
		name = a.DisplayName
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

		chosen, err := ro.chosen.List(ctx)
		if err != nil {
			ro.log().Error("load chosen games failed", "kind", "error", "account", a.ID, "err", err)
		}
		games := make([]plan.Game, 0, len(chosen))
		for _, g := range chosen {
			games = append(games, plan.Game{Name: g.Name, Slug: g.Slug})
		}

		r := runner.New(ro.runnerConfig(ctx, a, b, sess, games))
		return runner.Entry{ID: a.ID, Unit: r}
	}
}

// progressNotifyStepPct fires progress notifications at start, 50% and 100%,
// the 1.x default. The notify_progress toggle in Settings gates them.
const progressNotifyStepPct = 50

// runnerConfig is the runner.Config for one account build.
func (ro *roster) runnerConfig(ctx context.Context, a gen.Account, b platform.Backend, sess platform.Session, games []plan.Game) runner.Config {
	order := ro.pool.OrderFunc(a.ID)
	return runner.Config{
		AccountID: a.ID, AccountLabel: a.DisplayName, Platform: a.Platform,
		Backend: b, Session: sess,
		Games:       games,
		ForceLinked: linkOverrideSet(ctx, ro.q),
		Notifier:    ro.notifier,
		Claims:      &claimLog{ro: ro},
		Skips:       ro.skips,
		Order:       order,
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
	ro.names.set(a.ID, a.DisplayName)
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

// gamesChanged rebuilds every runner so each picks up the new chosen games.
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
		ro.reloadAccount(a.ID)
	}
	ro.hub.Publish(api.Event{Name: api.EventGames, Data: struct{}{}})
}

// linkOverrideSet reads the manual "I've linked it" campaign overrides from
// kv (keys prefixed store.LinkOverridePrefix). Errors degrade to no overrides.
func linkOverrideSet(ctx context.Context, q *gen.Queries) map[string]bool {
	set := map[string]bool{}
	rows, err := q.ListKVByPrefix(ctx, sql.NullString{String: store.LinkOverridePrefix, Valid: true})
	if err == nil {
		for _, kv := range rows {
			if string(kv.Value) != "1" {
				continue
			}
			set[strings.TrimPrefix(kv.Key, store.LinkOverridePrefix)] = true
		}
	}
	return set
}
