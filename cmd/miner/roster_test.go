package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"sync"
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
	ro := newRoster(roster{
		ctx: ctx, q: q,
		sessions:  store.NewSessionStore(db, q, cr),
		chosen:    store.NewChosenGames(db, q),
		skips:     store.NewSkips(q),
		collector: collect.New(db, q),
		hub:       hub, pool: pool,
		backendFor: func(gen.Account) (platform.Backend, error) { return fake, nil },
	}, nil)
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
	m := asMap(t, ev.Data)
	assert.Equal(t, "b1", m["benefit_id"])
	assert.Equal(t, "name-a1", m["account_name"])
	assert.Equal(t, store.ClaimSourceGrubDrops, m["source"])
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
	e.account(t, "a2", 1, true)
	builds := map[string]int{}
	var mu sync.Mutex
	e.ro.backendFor = func(a gen.Account) (platform.Backend, error) {
		mu.Lock()
		defer mu.Unlock()
		builds[a.ID]++
		return e.fake, nil
	}
	bs, err := e.ro.builders(e.ctx)
	require.NoError(t, err)
	require.NoError(t, e.ro.pool.Reload(e.ctx, bs))
	for _, id := range []string{"a1", "a2"} {
		require.Eventually(t, func() bool {
			s, ok := snapshotOf(t, e, id)
			return ok && s.State == runner.StateNoGames
		}, 5*time.Second, 20*time.Millisecond, id)
	}

	e.chooseRust(t)
	e.rustDrop(10, false)
	e.ro.gamesChanged()

	mu.Lock()
	assert.Equal(t, map[string]int{"a1": 2, "a2": 2}, builds)
	mu.Unlock()
	for _, id := range []string{"a1", "a2"} {
		require.Eventually(t, func() bool {
			s, ok := snapshotOf(t, e, id)
			return ok && s.State != runner.StateNoGames && s.State != runner.StateIdle
		}, 5*time.Second, 20*time.Millisecond, id)
	}
	e.waitEvent(t, func(ev api.Event) bool { return ev.Name == api.EventGames })
}

func TestRoster_RunnerEventCarriesRenamedAccount(t *testing.T) {
	e := newRosterEnv(t)
	e.flushHub(t)
	e.account(t, "a1", 1, true)
	e.chooseRust(t)
	e.rustDrop(10, false)
	bs, err := e.ro.builders(e.ctx) // captures the old name
	require.NoError(t, err)
	require.NoError(t, e.q.UpdateAccountDisplayName(e.ctx, gen.UpdateAccountDisplayNameParams{ID: "a1", DisplayName: "renamed"}))
	require.NoError(t, e.ro.pool.Reload(e.ctx, bs))

	ev := e.waitEvent(t, func(ev api.Event) bool { return ev.Name == api.EventRunner && ev.Key == "a1" })
	assert.Equal(t, "renamed", asMap(t, ev.Data)["account_name"])
}

func TestRoster_FoundClaimPublishes(t *testing.T) {
	e := newRosterEnv(t)
	e.flushHub(t)
	e.account(t, "a1", 1, true)
	tg := plan.Target{
		Campaign: platform.Campaign{ID: "c1", Platform: "kick", Game: "Rust", Name: "Camp", Status: "active"},
		Benefit:  platform.DropBenefit{ID: "b9", CampaignID: "c1", Name: "Skin", RequiredMinutes: 60},
	}
	e.ro.collector.OnScan(e.ctx, runner.Scan{
		AccountID: "a1", Platform: "kick", At: time.Now(), InventoryOK: true,
		Campaigns: []platform.Campaign{{ID: "c1", Platform: "kick", Game: "Rust", Status: "active",
			Benefits: []platform.DropBenefit{tg.Benefit}}},
		Progress: []platform.Progress{{BenefitID: "b9", Claimed: true}},
		Result:   plan.Result{Claimed: []plan.Target{tg}},
	})
	ev := e.waitEvent(t, func(ev api.Event) bool { return ev.Name == api.EventClaim })
	m := asMap(t, ev.Data)
	assert.Equal(t, "b9", m["benefit_id"])
	assert.Equal(t, "name-a1", m["account_name"])
	assert.Equal(t, store.ClaimSourceInventory, m["source"])
}

type fakeKick struct {
	mu        sync.Mutex
	sidecars  [][2]string
	channels  map[string][]string
	chanCalls int
}

func (f *fakeKick) RegisterSidecar(id, user string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sidecars = append(f.sidecars, [2]string{id, user})
}

func (f *fakeKick) RegisterChannels(id string, chs []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.chanCalls++
	if f.channels == nil {
		f.channels = map[string][]string{}
	}
	f.channels[id] = chs
}

func TestRoster_KickRegistersOncePerBuild(t *testing.T) {
	e := newRosterEnv(t)
	fk := &fakeKick{}
	e.ro.kick = fk
	a := e.account(t, "a1", 1, false)
	require.NoError(t, e.ro.sessions.Put(e.ctx, "a1", platform.Session{
		AccessToken: "t", ExpiresAt: time.Now().Add(time.Hour),
		Cookies: map[string]string{"kick": `{"channels":["xqc"]}`},
	}))
	e.ro.builder(a)()
	fk.mu.Lock()
	defer fk.mu.Unlock()
	assert.Equal(t, [][2]string{{"a1", "name-a1"}}, fk.sidecars)
	assert.Equal(t, 1, fk.chanCalls)
	assert.Equal(t, []string{"xqc"}, fk.channels["a1"])
}

func asMap(t *testing.T, v any) map[string]any {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))
	return m
}
