package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/plan"
	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/runner"
	"github.com/aalejandrofer/grubdrops/internal/store"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

type fakePool struct {
	mu         sync.Mutex
	snaps      []runner.Snapshot
	orders     map[string][]string
	resets     []string
	mineNow    []string
	recheck    []string
	recheckRes runner.LinkRecheck
	recheckErr error
}

func (f *fakePool) RecheckLinks(_ context.Context, id string) (runner.LinkRecheck, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, s := range f.snaps {
		if s.AccountID == id {
			f.recheck = append(f.recheck, id)
			return f.recheckRes, f.recheckErr
		}
	}
	return runner.LinkRecheck{}, runner.ErrNotRunning
}

func (f *fakePool) Snapshots() []runner.Snapshot {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := append([]runner.Snapshot(nil), f.snaps...)
	for i := range out {
		if o := f.orders[out[i].AccountID]; len(o) > 0 {
			out[i].UpNext = plan.ApplyOrder(out[i].UpNext, o)
		}
	}
	return out
}

func (f *fakePool) SetOrder(id string, ids []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.orders == nil {
		f.orders = map[string][]string{}
	}
	f.orders[id] = ids
}

func (f *fakePool) ResetOrder(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.orders, id)
	f.resets = append(f.resets, id)
}

func (*fakePool) RescanAll() {}

// MineNow mirrors the real pool's checks: Up next must hold a target.
func (f *fakePool) MineNow(id string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, s := range f.snaps {
		if s.AccountID != id {
			continue
		}
		if len(s.UpNext) == 0 {
			return "", runner.ErrNotQueued
		}
		bid := s.UpNext[0].Benefit.ID
		f.mineNow = append(f.mineNow, id+"/"+bid)
		return bid, nil
	}
	return "", runner.ErrNotRunning
}
func (f *fakePool) OrderFunc(id string) func() []string {
	return func() []string {
		f.mu.Lock()
		defer f.mu.Unlock()
		return f.orders[id]
	}
}

type resEnv struct {
	*env
	pool    *fakePool
	db      *sql.DB
	q       *gen.Queries
	minutes *store.WatchMinutes
}

// newResEnv builds a server with the overview and runners routes, two
// accounts (a1 "Alice" twitch, b2 "Bob" kick), one chosen game and auth bypass.
func newResEnv(t *testing.T, mut func(*Deps)) *resEnv {
	t.Helper()
	t.Setenv("GRUB_AUTHBYPASS", "true")
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "t.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	q := gen.New(db)
	sm := scs.New()
	sm.Store = NewKVSessionStore(db)
	sm.Lifetime = time.Hour
	pool := &fakePool{}
	d := Deps{
		DB: db, Q: q, Session: sm, Version: "1.0.0", Hub: NewHub(),
		SPA:     fstest.MapFS{"index.html": {Data: []byte("spa")}},
		Pool:    pool,
		Minutes: store.NewWatchMinutes(q),
		Claims:  store.NewClaimLog(q),
		Scans:   store.NewAccountScans(q),
		Chosen:  store.NewChosenGames(db, q),

		ForceWatch: store.NewForceWatch(db, q),
	}
	cr, err := store.NewCryptor(testAgeKey)
	require.NoError(t, err)
	d.Sessions = store.NewSessionStore(db, q, cr)
	if mut != nil {
		mut(&d)
	}
	for i, a := range []struct{ id, plat, name string }{{"a1", "twitch", "Alice"}, {"b2", "kick", "Bob"}} {
		_, err := q.CreateAccount(ctx, gen.CreateAccountParams{
			ID: a.id, Platform: a.plat, DisplayName: a.name, Status: "active",
			FingerprintJson: "{}", Enabled: 1, CreatedAt: int64(100 + i), UpdatedAt: int64(100 + i),
		})
		require.NoError(t, err)
	}
	require.NoError(t, q.UpsertGame(ctx, gen.UpsertGameParams{ID: "g1", Name: "Game", Slug: "game"}))
	require.NoError(t, d.Chosen.Set(ctx, []string{"g1"}))
	for _, id := range []string{"a1", "b2"} {
		require.NoError(t, d.Sessions.Put(ctx, id, platform.Session{AccessToken: "x", ExpiresAt: time.Now().Add(time.Hour)}))
	}
	s := newServer(d)
	s.routesOverview(s.protected)
	s.routesForceWatch(s.protected)
	srv := httptest.NewServer(s.handler())
	t.Cleanup(srv.Close)
	jar, _ := cookiejar.New(nil)
	e := &env{t: t, srv: srv, cl: &http.Client{Jar: jar}, s: s}
	return &resEnv{env: e, pool: pool, db: db, q: q, minutes: d.Minutes}
}

func (e *resEnv) getJSON(path string, v any) {
	e.t.Helper()
	resp, b := e.do("GET", path, "", nil)
	require.Equal(e.t, 200, resp.StatusCode, string(b))
	require.NoError(e.t, json.Unmarshal(b, v))
}

func (e *resEnv) overview() map[string]any {
	e.t.Helper()
	var m map[string]any
	e.getJSON("/api/overview", &m)
	return m
}

func TestOverview_Mining(t *testing.T) {
	e := newResEnv(t, nil)
	scan := time.Date(2026, 10, 25, 10, 0, 0, 0, time.UTC)
	e.pool.snaps = []runner.Snapshot{
		{AccountID: "a1", State: runner.StateWatching, LastScanAt: scan},
		{AccountID: "b2", State: runner.StateNeedsLogin},
	}
	m := e.overview()
	st := m["status"].(map[string]any)
	assert.Equal(t, "mining", st["state"])
	assert.EqualValues(t, 2, st["accounts_total"])
	assert.EqualValues(t, 1, st["accounts_mining"])
	assert.Equal(t, "2026-10-25T10:00:00Z", st["last_scan_at"])
	assert.Equal(t, []any{map[string]any{"kind": "needs_login", "account_id": "b2", "account_name": "Bob"}}, m["problems"])
	k := m["kpis"].(map[string]any)
	assert.EqualValues(t, 1, k["accounts_healthy"])
	assert.EqualValues(t, 0, k["minutes_total"])
	assert.EqualValues(t, 0, k["claimed_all_time"])
	assert.Equal(t, map[string]any{"current": "1.0.0", "latest": "1.0.0", "update_available": false}, m["version"])
}

func TestOverview_UpdateStatus(t *testing.T) {
	e := newResEnv(t, func(d *Deps) {
		d.UpdateStatus = func(cur string) (bool, string) { return true, "1.1.0" }
	})
	assert.Equal(t, map[string]any{"current": "1.0.0", "latest": "1.1.0", "update_available": true}, e.overview()["version"])
}

func TestOverview_NothingLive(t *testing.T) {
	e := newResEnv(t, nil)
	e.pool.snaps = []runner.Snapshot{{AccountID: "a1", State: runner.StateSleeping}, {AccountID: "b2", State: runner.StateNeedsLogin}}
	m := e.overview()
	st := m["status"].(map[string]any)
	assert.Equal(t, "nothing_live", st["state"])
	assert.Nil(t, st["last_scan_at"])
}

func TestOverview_PausedIsNotReady(t *testing.T) {
	e := newResEnv(t, nil)
	_, err := e.db.Exec("UPDATE accounts SET enabled = 0")
	require.NoError(t, err)
	e.pool.snaps = []runner.Snapshot{{AccountID: "a1", State: runner.StateIdle}, {AccountID: "b2", State: runner.StateIdle}}
	st := e.overview()["status"].(map[string]any)
	assert.Equal(t, "idle", st["state"], "paused accounts are not ready, so not nothing_live")
	assert.EqualValues(t, 2, st["accounts_paused"])

	_, err = e.db.Exec("UPDATE accounts SET enabled = 1 WHERE id = 'a1'")
	require.NoError(t, err)
	st = e.overview()["status"].(map[string]any)
	assert.Equal(t, "nothing_live", st["state"])
	assert.EqualValues(t, 1, st["accounts_paused"])
}

func TestOverview_Idle(t *testing.T) {
	e := newResEnv(t, nil)
	e.pool.snaps = []runner.Snapshot{{AccountID: "a1", State: runner.StateNeedsLogin}, {AccountID: "b2", State: runner.StateNeedsLogin}}
	assert.Equal(t, "idle", e.overview()["status"].(map[string]any)["state"])
	assert.Len(t, e.overview()["problems"], 2)
}

func TestOverview_NoAccounts(t *testing.T) {
	e := newResEnv(t, nil)
	_, err := e.db.Exec("DELETE FROM accounts")
	require.NoError(t, err)
	m := e.overview()
	assert.Equal(t, "no_accounts", m["status"].(map[string]any)["state"])
	assert.Equal(t, []any{}, m["problems"])
}

func TestOverview_NoGames(t *testing.T) {
	e := newResEnv(t, nil)
	require.NoError(t, e.s.d.Chosen.Set(context.Background(), nil))
	e.pool.snaps = []runner.Snapshot{{AccountID: "a1", State: runner.StateWatching}}
	assert.Equal(t, "no_games", e.overview()["status"].(map[string]any)["state"])
}

func TestOverview_KPIsUseZone(t *testing.T) {
	sh, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	now := time.Date(2026, 10, 25, 23, 30, 0, 0, time.UTC) // 2026-10-26 07:30 in Shanghai
	e := newResEnv(t, func(d *Deps) {
		d.Zone = func() *time.Location { return sh }
		d.Now = func() time.Time { return now }
	})
	ctx := context.Background()
	require.NoError(t, e.minutes.Add(ctx, "a1", "2026-10-26", 30))
	require.NoError(t, e.minutes.Add(ctx, "b2", "2026-10-25", 100))
	// Claimed 2026-10-25 20:00Z is 2026-10-26 04:00 in Shanghai: today. 2026-10-25 10:00Z is yesterday there.
	for i, at := range []time.Time{time.Date(2026, 10, 25, 20, 0, 0, 0, time.UTC), time.Date(2026, 10, 25, 10, 0, 0, 0, time.UTC)} {
		require.NoError(t, e.s.d.Claims.Record(ctx, store.ClaimEntry{
			AccountID: "a1", Platform: "twitch", BenefitID: string(rune('x' + i)), ClaimedAt: at,
		}))
	}
	k := e.overview()["kpis"].(map[string]any)
	assert.EqualValues(t, 30, k["minutes_today"])
	assert.EqualValues(t, 130, k["minutes_total"])
	assert.EqualValues(t, 1, k["claimed_today"])
	assert.EqualValues(t, 2, k["claimed_all_time"])
}

func TestOverview_LastScanFromStore(t *testing.T) {
	e := newResEnv(t, nil)
	at := time.Date(2026, 10, 24, 8, 0, 0, 0, time.UTC)
	require.NoError(t, e.s.d.Scans.Put(context.Background(), store.AccountScan{AccountID: "a1", ScannedAt: at}))
	assert.Equal(t, "2026-10-24T08:00:00Z", e.overview()["status"].(map[string]any)["last_scan_at"])
}

func TestOverview_PausedSignedOutIsProblem(t *testing.T) {
	e := newResEnv(t, nil)
	_, err := e.db.Exec("UPDATE accounts SET enabled = 0 WHERE id = 'b2'")
	require.NoError(t, err)
	_, err = e.db.Exec("DELETE FROM sessions WHERE account_id = 'b2'")
	require.NoError(t, err)
	e.pool.snaps = []runner.Snapshot{{AccountID: "a1", State: runner.StateSleeping}, {AccountID: "b2", State: runner.StateIdle}}
	m := e.overview()
	assert.Equal(t, []any{map[string]any{"kind": "needs_login", "account_id": "b2", "account_name": "Bob"}}, m["problems"])
	assert.EqualValues(t, 1, m["kpis"].(map[string]any)["accounts_healthy"])
}

func TestOverview_PausedWithSessionNoProblem(t *testing.T) {
	e := newResEnv(t, nil)
	_, err := e.db.Exec("UPDATE accounts SET enabled = 0 WHERE id = 'b2'")
	require.NoError(t, err)
	e.pool.snaps = []runner.Snapshot{{AccountID: "a1", State: runner.StateSleeping}, {AccountID: "b2", State: runner.StateIdle}}
	assert.Equal(t, []any{}, e.overview()["problems"])
}

func TestOverview_SignedOutNeedsLoginNotDuplicated(t *testing.T) {
	e := newResEnv(t, nil)
	_, err := e.db.Exec("DELETE FROM sessions WHERE account_id = 'b2'")
	require.NoError(t, err)
	e.pool.snaps = []runner.Snapshot{{AccountID: "a1", State: runner.StateSleeping}, {AccountID: "b2", State: runner.StateNeedsLogin}}
	assert.Len(t, e.overview()["problems"], 1)
}

func TestOverview_NextScanAt(t *testing.T) {
	e := newResEnv(t, nil)
	t0 := time.Date(2026, 10, 25, 10, 0, 0, 0, time.UTC)
	_, err := e.db.Exec("UPDATE accounts SET enabled = 0 WHERE id = 'b2'")
	require.NoError(t, err)
	e.pool.snaps = []runner.Snapshot{
		{AccountID: "a1", State: runner.StateSleeping, NextScanAt: t0.Add(5 * time.Minute)},
		{AccountID: "b2", State: runner.StateSleeping, NextScanAt: t0.Add(time.Minute)},
	}
	assert.Equal(t, "2026-10-25T10:05:00Z", e.overview()["status"].(map[string]any)["next_scan_at"])
	e.pool.snaps = nil
	assert.Nil(t, e.overview()["status"].(map[string]any)["next_scan_at"])
}
