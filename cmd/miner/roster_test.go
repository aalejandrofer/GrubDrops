package main

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/api"
	"github.com/aalejandrofer/grubdrops/internal/collect"
	"github.com/aalejandrofer/grubdrops/internal/plan"
	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/platform/platformtest"
	"github.com/aalejandrofer/grubdrops/internal/runner"
	"github.com/aalejandrofer/grubdrops/internal/store"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

const rosterTestKey = "AGE-SECRET-KEY-1DZCAXYWJM6M42NSX5GR4QWZZ2JXEYKJ9ZKWYFYSNU997775JJ6XSY85FK9"

type rosterEnv struct {
	ro   *roster
	db   *sql.DB
	q    *gen.Queries
	fake *platformtest.Fake
	ctx  context.Context
	sub  <-chan api.Event
}

func newRosterEnv(t *testing.T) *rosterEnv {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "r.db"))
	require.NoError(t, err)
	q := gen.New(db)
	cr, err := store.NewCryptor(rosterTestKey)
	require.NoError(t, err)
	fake := platformtest.NewFake()
	hub := api.NewHub()
	sub, unsub := hub.Subscribe()
	pool := runner.NewPool(nil)
	ro := &roster{
		ctx: ctx, q: q,
		sessions:  store.NewSessionStore(db, q, cr),
		chosen:    store.NewChosenGames(db, q),
		skips:     store.NewSkips(q),
		collector: collect.New(db, q),
		hub:       hub, pool: pool,
		backendFor: func(gen.Account) (platform.Backend, error) { return fake, nil },
	}
	t.Cleanup(func() {
		unsub()
		cancel()
		pool.Stop()
		_ = db.Close()
	})
	return &rosterEnv{ro: ro, db: db, q: q, fake: fake, ctx: ctx, sub: sub}
}

func (e *rosterEnv) account(t *testing.T, id string, enabled int64, session bool) gen.Account {
	t.Helper()
	a, err := e.q.CreateAccount(e.ctx, gen.CreateAccountParams{
		ID: id, Platform: "kick", DisplayName: "name-" + id, Status: "idle",
		FingerprintJson: "{}", Enabled: enabled,
		CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix(),
	})
	require.NoError(t, err)
	if session {
		require.NoError(t, e.ro.sessions.Put(e.ctx, id, platform.Session{
			AccessToken: "tok", ExpiresAt: time.Now().Add(time.Hour),
		}))
	}
	return a
}

func (e *rosterEnv) chooseRust(t *testing.T) {
	t.Helper()
	all, err := e.q.ListAllGames(e.ctx)
	require.NoError(t, err)
	for _, g := range all {
		if g.Name == "Rust" {
			require.NoError(t, e.ro.chosen.Set(e.ctx, []string{g.ID}))
			return
		}
	}
	t.Fatal("Rust is not seeded")
}

func (e *rosterEnv) rustDrop(minutes int, claimed bool) {
	e.fake.Mu.Lock()
	defer e.fake.Mu.Unlock()
	e.fake.Campaigns = []platform.Campaign{{
		ID: "c1", Platform: "kick", Game: "Rust", Status: "active", AccountLinked: true,
		Benefits: []platform.DropBenefit{{ID: "b1", Name: "Skin", CampaignID: "c1", RequiredMinutes: 60}},
	}}
	e.fake.Streams["c1"] = []platform.Stream{{Channel: "ch", ChannelID: "id", ViewerCount: 5}}
	e.fake.Progress["b1"] = &platform.Progress{BenefitID: "b1", MinutesWatched: minutes, Claimed: claimed}
}

// waitEvent drains the subscription until pred matches or the deadline hits.
func (e *rosterEnv) waitEvent(t *testing.T, pred func(api.Event) bool) api.Event {
	t.Helper()
	timeout := time.After(10 * time.Second)
	for {
		select {
		case ev, ok := <-e.sub:
			require.True(t, ok, "subscription closed")
			if pred(ev) {
				return ev
			}
		case <-timeout:
			t.Fatal("event not seen")
		}
	}
}

func (e *rosterEnv) flushHub(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithCancel(e.ctx)
	t.Cleanup(cancel)
	go e.ro.hub.Run(ctx)
}

func snapshotOf(t *testing.T, e *rosterEnv, id string) (runner.Snapshot, bool) {
	t.Helper()
	for _, s := range e.ro.pool.Snapshots() {
		if s.AccountID == id {
			return s, true
		}
	}
	return runner.Snapshot{}, false
}

func TestRoster_NoSessionNeedsLogin(t *testing.T) {
	e := newRosterEnv(t)
	a := e.account(t, "a1", 1, false)
	en := e.ro.builder(a)()
	assert.Equal(t, "a1", en.ID)
	s := en.Unit.Snapshot()
	assert.Equal(t, runner.StateNeedsLogin, s.State)
	assert.Equal(t, "a1", s.AccountID)
	assert.Equal(t, "kick", s.Platform)
}

func TestRoster_DisabledIdle(t *testing.T) {
	e := newRosterEnv(t)
	a := e.account(t, "a1", 0, true)
	en := e.ro.builder(a)()
	assert.Equal(t, runner.StateIdle, en.Unit.Snapshot().State)
}

func TestRoster_RunnerWiredToCollector(t *testing.T) {
	e := newRosterEnv(t)
	e.flushHub(t)
	e.account(t, "a1", 1, true)
	e.chooseRust(t)
	e.rustDrop(10, false)

	bs, err := e.ro.builders(e.ctx)
	require.NoError(t, err)
	require.Len(t, bs, 1)
	require.NoError(t, e.ro.pool.Reload(e.ctx, bs))

	e.waitEvent(t, func(ev api.Event) bool { return ev.Name == api.EventGames })
	e.waitEvent(t, func(ev api.Event) bool { return ev.Name == api.EventRunner && ev.Key == "a1" })
	require.Eventually(t, func() bool {
		rows, err := e.q.ListDropStatesForAccount(e.ctx, "a1")
		return err == nil && len(rows) > 0
	}, 10*time.Second, 20*time.Millisecond)
}

func TestRoster_ClaimPublishes(t *testing.T) {
	e := newRosterEnv(t)
	e.flushHub(t)
	e.account(t, "a1", 1, true)
	e.chooseRust(t)
	e.rustDrop(60, false)

	bs, err := e.ro.builders(e.ctx)
	require.NoError(t, err)
	require.NoError(t, e.ro.pool.Reload(e.ctx, bs))

	ev := e.waitEvent(t, func(ev api.Event) bool { return ev.Name == api.EventClaim })
	assert.NotNil(t, ev.Data)
	var entry store.ClaimEntry
	require.Eventually(t, func() bool {
		var err error
		entry, err = e.ro.collector.Claims.Get(e.ctx, "a1", "b1")
		return err == nil
	}, 5*time.Second, 20*time.Millisecond)
	assert.Equal(t, store.ClaimSourceGrubDrops, entry.Source)
	assert.Equal(t, "Rust", entry.Game)
}

func TestRoster_AccountRemoved(t *testing.T) {
	e := newRosterEnv(t)
	e.flushHub(t)
	e.account(t, "a1", 1, true)
	e.chooseRust(t)
	e.rustDrop(10, false)

	bs, err := e.ro.builders(e.ctx)
	require.NoError(t, err)
	require.NoError(t, e.ro.pool.Reload(e.ctx, bs))
	e.waitEvent(t, func(ev api.Event) bool { return ev.Name == api.EventGames })

	games, err := e.ro.chosen.List(e.ctx)
	require.NoError(t, err)
	require.Len(t, games, 1)
	pg := []plan.Game{{Name: "Rust", Slug: "rust"}}
	require.Eventually(t, func() bool {
		return len(e.ro.collector.Board.Games(pg)[0].Campaigns) == 1
	}, 5*time.Second, 20*time.Millisecond)

	e.ro.accountRemoved("a1")

	_, ok := snapshotOf(t, e, "a1")
	assert.False(t, ok)
	assert.Empty(t, e.ro.collector.Board.Games(pg)[0].Campaigns)
	e.waitEvent(t, func(ev api.Event) bool { return ev.Name == api.EventAccounts })
}

func TestRoster_GamesChangedReloadsEach(t *testing.T) {
	e := newRosterEnv(t)
	e.flushHub(t)
	e.account(t, "a1", 1, true)
	e.account(t, "a2", 0, false)
	bs, err := e.ro.builders(e.ctx)
	require.NoError(t, err)
	require.NoError(t, e.ro.pool.Reload(e.ctx, bs))
	s1, _ := snapshotOf(t, e, "a1")
	require.NotEqual(t, runner.StateNeedsLogin, s1.State)

	// a2 gains a session and is enabled; gamesChanged rebuilds it from its row.
	_, err = e.db.ExecContext(e.ctx, `UPDATE accounts SET enabled = 1 WHERE id = 'a2'`)
	require.NoError(t, err)
	require.NoError(t, e.ro.sessions.Put(e.ctx, "a2", platform.Session{AccessToken: "t", ExpiresAt: time.Now().Add(time.Hour)}))
	s2, _ := snapshotOf(t, e, "a2")
	require.Equal(t, runner.StateIdle, s2.State)

	e.ro.gamesChanged()

	require.Eventually(t, func() bool {
		s, ok := snapshotOf(t, e, "a2")
		return ok && s.State != runner.StateIdle
	}, 5*time.Second, 20*time.Millisecond)
	e.waitEvent(t, func(ev api.Event) bool { return ev.Name == api.EventGames })
}
