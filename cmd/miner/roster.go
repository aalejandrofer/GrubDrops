package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sort"
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
	names       *accountNames       // set by newRoster
	fallbackLog *refreshFallbackLog // set by newRoster

	backendFor func(gen.Account) (platform.Backend, error)
	notifier   runner.Notifier
	settings   *store.Settings              // nil = default priority
	runCfg     *atomic.Pointer[runSettings] // set by newRoster; holds nil until read

	// gamesDirty queues one games reload; see requestGamesReload.
	gamesDirty chan struct{} // set by newRoster

	// Force watch. force nil = feature off. forceLookup is the shared
	// checker's Lookup; nil = runners never force watch.
	force       *store.ForceWatch
	forceLookup func(ctx context.Context, platform, login string) (platform.Stream, bool, error)
	forceCache  *atomic.Pointer[[]store.ForceChannel] // set by newRoster; nil until read
	forceMu     *sync.Mutex                           // set by newRoster; serializes list loads and change diffs
	forceQueue  *accountQueue                         // set by newRoster; see forceChanged
	loaded      *loadedSessions                       // set by newRoster
	noLooker    *sync.Map                             // set by newRoster; platforms already warned about
}

// loadedSession is the backend and session a runner build last loaded for
// one account.
type loadedSession struct {
	backend platform.Backend
	session platform.Session
}

// loadedSessions maps account ID to its loaded session, so the force-watch
// checker can borrow one without reloading it.
type loadedSessions struct {
	mu sync.Mutex
	m  map[string]loadedSession
}

func (l *loadedSessions) set(id string, s loadedSession) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.m[id] = s
}

func (l *loadedSessions) get(id string) (loadedSession, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	s, ok := l.m[id]
	return s, ok
}

func (l *loadedSessions) remove(id string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.m, id)
}

// accountQueue is a set of account IDs waiting for a reload, with a one-slot
// wake channel. Adds fold into one pending set.
type accountQueue struct {
	mu   sync.Mutex
	ids  map[string]bool
	wake chan struct{}
}

func newAccountQueue() *accountQueue {
	return &accountQueue{ids: map[string]bool{}, wake: make(chan struct{}, 1)}
}

func (q *accountQueue) add(ids map[string]bool) {
	if len(ids) == 0 {
		return
	}
	q.mu.Lock()
	for id := range ids {
		q.ids[id] = true
	}
	q.mu.Unlock()
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

// take empties the set and returns its IDs sorted.
func (q *accountQueue) take() []string {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]string, 0, len(q.ids))
	for id := range q.ids {
		out = append(out, id)
	}
	clear(q.ids)
	sort.Strings(out)
	return out
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
	ro.fallbackLog = &refreshFallbackLog{}
	ro.gamesDirty = make(chan struct{}, 1)
	ro.runCfg = &atomic.Pointer[runSettings]{}
	ro.forceCache = &atomic.Pointer[[]store.ForceChannel]{}
	ro.forceMu = &sync.Mutex{}
	ro.forceQueue = newAccountQueue()
	ro.loaded = &loadedSessions{m: map[string]loadedSession{}}
	ro.noLooker = &sync.Map{}
	if k != nil {
		ro.kick = k
	}
	// Claims found during reconcile reach the hub the same way runner claims do.
	ro.collector.OnFound = func(ctx context.Context, e store.ClaimEntry) {
		ro.publishClaim(ctx, e.AccountID, e.BenefitID)
	}
	ro.collector.LookupTwitchGameID = ro.lookupTwitchGameID
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
		// Only a build that gets to a runner leaves a session to borrow.
		ro.loaded.remove(a.ID)
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
			get:         ro.sessions.Get,
			refresh:     b.RefreshSession,
			put:         ro.sessions.Put,
			logger:      ro.log(),
			backoff:     sessionRetryBackoff,
			verify:      verify,
			fallbackLog: ro.fallbackLog,
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

		lsess := sess
		lsess.AccountID = a.ID
		ro.loaded.set(a.ID, loadedSession{backend: b, session: lsess})

		// A runner even with no games: it force watches when it has
		// force channels, and idles itself as no_games otherwise.
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
		ForceChannels:         func() []string { return ro.forceFor(a.ID, a.Platform) },
		ForceLookup:           ro.forceLookupFor(a.Platform),
	}
}

// forceLookupFor binds the shared force-watch lookup to one platform; nil
// when force watch is off.
func (ro *roster) forceLookupFor(plat string) func(ctx context.Context, login string) (platform.Stream, bool, error) {
	if ro.forceLookup == nil {
		return nil
	}
	lookup := ro.forceLookup
	return func(ctx context.Context, login string) (platform.Stream, bool, error) {
		return lookup(ctx, plat, login)
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
		ro.loaded.remove(id)
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
	ro.loaded.remove(id)
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

// errNoForceLookupAccount means no signed-in account can run a force-watch
// lookup on the platform; the channel's live state stays unknown.
var errNoForceLookupAccount = errors.New("force watch: no signed-in account for lookups")

// forceList returns the cached force-watch list, reading it once after start
// or after forceChanged. A failed read gives nil for that call and is not
// cached.
func (ro *roster) forceList() []store.ForceChannel {
	if c := ro.forceCache.Load(); c != nil {
		return *c
	}
	if ro.force == nil {
		return nil
	}
	ro.forceMu.Lock()
	defer ro.forceMu.Unlock()
	if c := ro.forceCache.Load(); c != nil {
		return *c
	}
	list, err := ro.loadForce()
	if err != nil {
		ro.log().Warn("read force watch list failed", "kind", "error", "err", err)
		return nil
	}
	return list
}

// loadForce reads the list from the store and caches it. Callers hold forceMu.
func (ro *roster) loadForce() ([]store.ForceChannel, error) {
	ctx, cancel := context.WithTimeout(ro.ctx, 5*time.Second)
	defer cancel()
	list, err := ro.force.List(ctx)
	if err != nil {
		return nil, err
	}
	ro.forceCache.Store(&list)
	return list, nil
}

// forceFor is the account's force channels in panel order.
func (ro *roster) forceFor(id, plat string) []string {
	var out []string
	for _, c := range ro.forceList() {
		if c.Platform == plat && slices.Contains(c.Accounts, id) {
			out = append(out, c.Channel)
		}
	}
	return out
}

// forceMembers is the set of account/platform/channel assignments in list.
func forceMembers(list []store.ForceChannel) map[[3]string]bool {
	m := map[[3]string]bool{}
	for _, c := range list {
		for _, id := range c.Accounts {
			m[[3]string{id, c.Platform, c.Channel}] = true
		}
	}
	return m
}

// forceLookupWarm bounds the background lookup forceChanged starts for each
// newly added channel.
const forceLookupWarm = 30 * time.Second

// forceChanged refreshes the cached list and tells every account added to
// or removed from a channel, plus every runner force watching now. Runners
// read the list live, so only a runner that has exited as no_games is
// rebuilt, so it can start force watching. An idle runner re-picks and a
// force-watching one rechecks at its next beat without restarting its watch.
// An account starting, watching or claiming a drop is left alone: the list
// only matters once it is idle. Each newly added channel gets a background
// lookup so its live state fills in. It returns at once; runForceReloader
// does the rebuilds.
func (ro *roster) forceChanged() {
	states := map[string]runner.State{}
	for _, sn := range ro.pool.Snapshots() {
		states[sn.AccountID] = sn.State
	}
	ids := map[string]bool{}
	var added [][2]string
	if ro.force != nil {
		ro.forceMu.Lock()
		var old []store.ForceChannel
		if c := ro.forceCache.Load(); c != nil {
			old = *c
		}
		list, err := ro.loadForce()
		if err != nil {
			// Drop the cache so the next read tries again.
			ro.forceCache.Store(nil)
			ro.log().Error("force watch changed: read list failed", "kind", "error", "err", err)
		} else {
			was, now := forceMembers(old), forceMembers(list)
			for k := range was {
				if !now[k] {
					ids[k[0]] = true
				}
			}
			for k := range now {
				if !was[k] {
					ids[k[0]] = true
				}
			}
			added = addedChannels(old, list)
		}
		ro.forceMu.Unlock()
	}
	for id, st := range states {
		if st == runner.StateForceWatching {
			ids[id] = true
		}
	}
	rebuild := map[string]bool{}
	for id := range ids {
		switch states[id] {
		case runner.StateStarting, runner.StateWatching, runner.StateClaiming:
		case runner.StateNoGames:
			rebuild[id] = true
		default:
			ro.pool.ForceListChanged(id)
		}
	}
	ro.forceQueue.add(rebuild)
	ro.warmForce(added)
}

// addedChannels is the platform/channel pairs in list that old lacks, in
// list order.
func addedChannels(old, list []store.ForceChannel) [][2]string {
	had := make(map[[2]string]bool, len(old))
	for _, c := range old {
		had[[2]string{c.Platform, c.Channel}] = true
	}
	var out [][2]string
	for _, c := range list {
		if k := [2]string{c.Platform, c.Channel}; !had[k] {
			out = append(out, k)
		}
	}
	return out
}

// warmForce looks each channel up in the background, so the shared cache
// knows its live state before the next refresh pass. It never blocks.
func (ro *roster) warmForce(chans [][2]string) {
	if ro.forceLookup == nil || len(chans) == 0 {
		return
	}
	lookup := ro.forceLookup
	go func() {
		for _, c := range chans {
			ctx, cancel := context.WithTimeout(ro.ctx, forceLookupWarm)
			_, _, _ = lookup(ctx, c[0], c[1]) // failures are logged by the checker
			cancel()
		}
	}()
}

// runForceReloader runs queued force-watch reloads until ro.ctx ends.
func (ro *roster) runForceReloader() {
	for {
		select {
		case <-ro.ctx.Done():
			return
		case <-ro.forceQueue.wake:
			for _, id := range ro.forceQueue.take() {
				if ro.ctx.Err() != nil {
					return
				}
				ro.safeReloadAccount(id)
			}
		}
	}
}

// safeReloadAccount keeps a builder panic from killing the reloader goroutine.
func (ro *roster) safeReloadAccount(id string) {
	defer func() {
		if r := recover(); r != nil {
			ro.log().Error("force watch changed: reload panicked", "kind", "error", "account", id, "panic", r)
		}
	}()
	ro.reloadAccount(id)
}

// categoryLookupTimeout caps one account's category lookup, so a hung
// account leaves time to try the next one.
const categoryLookupTimeout = 7 * time.Second

// lookupCategory resolves a category slug to its game name with the first
// signed-in account on plat that can, trying the next one on error. It
// returns the last lookup error, or api.ErrNoCategoryAccount when no
// account could try.
func (ro *roster) lookupCategory(ctx context.Context, plat, slug string) (string, bool, error) {
	var name string
	found, err := ro.lookupWithSession(ctx, plat, func(ctx context.Context, ls loadedSession) (bool, bool, error) {
		cl, ok := ls.backend.(platform.CategoryLooker)
		if !ok {
			return false, false, nil
		}
		n, found, err := cl.LookupCategory(ctx, ls.session, slug)
		name = n
		return true, found, err
	})
	return name, found, err
}

// lookupTwitchGameID finds a game's Twitch id by name, the same way
// lookupCategory finds a name, for box art of games no campaign named.
func (ro *roster) lookupTwitchGameID(ctx context.Context, name string) (string, bool, error) {
	var id string
	found, err := ro.lookupWithSession(ctx, "twitch", func(ctx context.Context, ls loadedSession) (bool, bool, error) {
		gl, ok := ls.backend.(platform.GameIDLooker)
		if !ok {
			return false, false, nil
		}
		v, found, err := gl.LookupGameID(ctx, ls.session, name)
		id = v
		return true, found, err
	})
	return id, found, err
}

// lookupWithSession runs try with each signed-in, already loaded session on
// plat, each under categoryLookupTimeout, until one try runs without error.
// try reports tried=false when the backend cannot do the lookup. It returns
// the last error, or api.ErrNoCategoryAccount when nothing could try.
func (ro *roster) lookupWithSession(ctx context.Context, plat string, try func(context.Context, loadedSession) (tried, found bool, err error)) (bool, error) {
	accts, err := ro.q.ListEnabledAccounts(ctx)
	if err != nil {
		return false, err
	}
	states := map[string]runner.State{}
	for _, sn := range ro.pool.Snapshots() {
		states[sn.AccountID] = sn.State
	}
	var lastErr error
	for _, a := range accts {
		if a.Platform != plat || states[a.ID] == runner.StateNeedsLogin {
			continue
		}
		ls, ok := ro.loaded.get(a.ID)
		if !ok {
			continue
		}
		actx, cancel := context.WithTimeout(ctx, categoryLookupTimeout)
		tried, found, err := try(actx, ls)
		cancel()
		if !tried {
			continue
		}
		if err == nil {
			return found, nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return false, lastErr
	}
	return false, fmt.Errorf("%s: %w", plat, api.ErrNoCategoryAccount)
}

// lookupForce checks one channel with a session a runner build already
// loaded: each enabled account on the platform in turn, skipping ones that
// need a login, until one lookup succeeds. It returns the last lookup error,
// or errNoForceLookupAccount when no account could try. It never loads a
// session itself.
func (ro *roster) lookupForce(ctx context.Context, plat, login string) (platform.Stream, bool, error) {
	accts, err := ro.q.ListEnabledAccounts(ctx)
	if err != nil {
		return platform.Stream{}, false, err
	}
	states := map[string]runner.State{}
	for _, sn := range ro.pool.Snapshots() {
		states[sn.AccountID] = sn.State
	}
	var lastErr error
	noLooker := false
	for _, a := range accts {
		if a.Platform != plat || states[a.ID] == runner.StateNeedsLogin {
			continue
		}
		ls, ok := ro.loaded.get(a.ID)
		if !ok {
			continue
		}
		cl, ok := ls.backend.(platform.ChannelLooker)
		if !ok {
			noLooker = true
			continue
		}
		s, live, err := cl.LookupChannel(ctx, ls.session, login)
		if err == nil {
			return s, live, nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return platform.Stream{}, false, lastErr
	}
	if noLooker {
		if _, warned := ro.noLooker.LoadOrStore(plat, true); !warned {
			ro.log().Warn("force watch: this platform backend cannot look up channels; its force channels stay unknown",
				"kind", "error", "platform", plat)
		}
	}
	return platform.Stream{}, false, fmt.Errorf("%w: %s", errNoForceLookupAccount, plat)
}
