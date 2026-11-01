package main

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/platform/platformtest"
	"github.com/aalejandrofer/grubdrops/internal/runner"
	"github.com/aalejandrofer/grubdrops/internal/store"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

// withForce turns force watch on with the uncached roster lookup.
func (e *rosterEnv) withForce() {
	e.ro.force = store.NewForceWatch(e.db, e.q)
	e.ro.forceLookup = e.ro.lookupForce
}

func (e *rosterEnv) setForce(t *testing.T, list []store.ForceChannel) {
	t.Helper()
	require.NoError(t, e.ro.force.Set(e.ctx, list))
}

// pointsOn sets each account's points mode to drops_points, which force
// watch needs.
func (e *rosterEnv) pointsOn(t *testing.T, ids ...string) {
	t.Helper()
	for _, id := range ids {
		require.NoError(t, e.q.SetAccountPointsMode(e.ctx, gen.SetAccountPointsModeParams{
			PointsMode: string(platform.PointsDropsPoints), UpdatedAt: time.Now().Unix(), ID: id,
		}))
	}
}

// twitchAccount adds an enabled, signed-in Twitch account: only Twitch has
// a points mode of its own.
func (e *rosterEnv) twitchAccount(t *testing.T, id string) gen.Account {
	t.Helper()
	e.account(t, id, 1, true)
	_, err := e.db.ExecContext(e.ctx, `UPDATE accounts SET platform = 'twitch' WHERE id = ?`, id)
	require.NoError(t, err)
	a, err := e.q.GetAccount(e.ctx, id)
	require.NoError(t, err)
	return a
}

func (e *rosterEnv) boot(t *testing.T) {
	t.Helper()
	bs, err := e.ro.builders(e.ctx)
	require.NoError(t, err)
	require.NoError(t, e.ro.pool.Reload(e.ctx, bs))
}

func TestRoster_ForceForCachedUntilChanged(t *testing.T) {
	e := newRosterEnv(t)
	e.withForce()
	e.account(t, "a1", 1, true)
	e.account(t, "a2", 1, true)
	e.setForce(t, []store.ForceChannel{
		{Platform: "kick", Channel: "xqc", Accounts: []string{"a1"}},
		{Platform: "kick", Channel: "amouranth", Accounts: []string{"a1", "a2"}},
	})
	assert.Equal(t, []string{"xqc", "amouranth"}, e.ro.forceFor("a1", "kick"))
	assert.Equal(t, []string{"amouranth"}, e.ro.forceFor("a2", "kick"))
	assert.Empty(t, e.ro.forceFor("a1", "twitch"))

	// A store write alone is not seen until forceChanged.
	e.setForce(t, []store.ForceChannel{{Platform: "kick", Channel: "xqc", Accounts: []string{"a2"}}})
	assert.Equal(t, []string{"xqc", "amouranth"}, e.ro.forceFor("a1", "kick"))
	e.ro.forceChanged()
	assert.Empty(t, e.ro.forceFor("a1", "kick"))
	assert.Equal(t, []string{"xqc"}, e.ro.forceFor("a2", "kick"))
}

func TestRoster_ForceOffWithoutStore(t *testing.T) {
	e := newRosterEnv(t)
	a := e.account(t, "a1", 1, true)
	assert.Nil(t, e.ro.forceFor("a1", "kick"))
	cfg := e.ro.runnerConfig(e.ctx, a, e.fake, platform.Session{}, nil)
	assert.Nil(t, cfg.ForceLookup, "no checker: feature off")
	e.ro.forceChanged() // nil-safe
}

func TestRoster_ForceLookupUsesLoadedSession(t *testing.T) {
	e := newRosterEnv(t)
	e.withForce()
	e.fake.Lookups["xqc"] = platformtest.Lookup{Stream: platform.Stream{Channel: "xqc", ViewerCount: 9}, Live: true}
	e.account(t, "a0", 0, true) // disabled: never lends its session
	a := e.account(t, "a1", 1, true)

	_, _, err := e.ro.lookupForce(e.ctx, "kick", "xqc")
	require.ErrorIs(t, err, errNoForceLookupAccount, "nothing loaded yet")

	e.ro.builder(a)()
	s, live, err := e.ro.lookupForce(e.ctx, "kick", "xqc")
	require.NoError(t, err)
	assert.True(t, live)
	assert.Equal(t, 9, s.ViewerCount)
	_, _, err = e.ro.lookupForce(e.ctx, "twitch", "xqc")
	assert.ErrorIs(t, err, errNoForceLookupAccount, "no twitch account")

	// A build that ends signed out drops the borrowed session.
	_, err = e.db.Exec(`DELETE FROM sessions WHERE account_id = 'a1'`)
	require.NoError(t, err)
	e.ro.builder(a)()
	_, _, err = e.ro.lookupForce(e.ctx, "kick", "xqc")
	assert.ErrorIs(t, err, errNoForceLookupAccount)
}

func TestRoster_NoGamesForceWatches(t *testing.T) {
	e := newRosterEnv(t)
	e.withForce()
	e.account(t, "a1", 1, true)
	e.pointsOn(t, "a1")
	e.fake.Lookups["xqc"] = platformtest.Lookup{Stream: platform.Stream{Channel: "xqc", ChannelID: "1"}, Live: true}
	e.setForce(t, []store.ForceChannel{{Platform: "kick", Channel: "xqc", Accounts: []string{"a1"}}})
	e.boot(t)
	require.Eventually(t, func() bool {
		s, ok := snapshotOf(t, e, "a1")
		return ok && s.State == runner.StateForceWatching && s.Channel == "xqc"
	}, 5*time.Second, 20*time.Millisecond)
}

// An account with points off does no force watch: its runner reads an empty
// force list, though its assignment stays in the store. drops_points
// restores it.
func TestRoster_PointsOffSkipsForceWatch(t *testing.T) {
	e := newRosterEnv(t)
	e.withForce()
	a := e.twitchAccount(t, "a1")
	e.fake.Lookups["xqc"] = platformtest.Lookup{Stream: platform.Stream{Channel: "xqc", ChannelID: "1"}, Live: true}
	e.setForce(t, []store.ForceChannel{{Platform: "twitch", Channel: "xqc", Accounts: []string{"a1"}}})

	cfg := e.ro.runnerConfig(e.ctx, a, e.fake, platform.Session{}, nil)
	require.NotNil(t, cfg.ForceChannels)
	assert.Empty(t, cfg.ForceChannels(), "points off: no force list")
	assert.Equal(t, platform.PointsOff, cfg.PointsMode())
	assert.Equal(t, []string{"xqc"}, e.ro.forceFor("a1", "twitch"), "assignment kept")

	e.boot(t)
	require.Eventually(t, func() bool {
		s, ok := snapshotOf(t, e, "a1")
		return ok && s.State == runner.StateNoGames
	}, 5*time.Second, 20*time.Millisecond)
	e.fake.Mu.Lock()
	assert.NotContains(t, e.fake.Calls, "Start:xqc")
	e.fake.Mu.Unlock()

	// Turning points on reaches the runner through the live flags; the
	// runner exited as no_games, so it is rebuilt to force watch.
	e.pointsOn(t, "a1")
	e.ro.accountSettingsChanged("a1")
	assert.Equal(t, []string{"xqc"}, cfg.ForceChannels())
	assert.Equal(t, platform.PointsDropsPoints, cfg.PointsMode())
	require.Eventually(t, func() bool {
		s, ok := snapshotOf(t, e, "a1")
		return ok && s.State == runner.StateForceWatching && s.Channel == "xqc"
	}, 5*time.Second, 20*time.Millisecond)
}

// Kick has no channel points, so its stored points mode is ignored: an
// account left at the default off still force watches its channels.
func TestRoster_KickPointsOffStillForceWatches(t *testing.T) {
	e := newRosterEnv(t)
	e.withForce()
	a := e.account(t, "a1", 1, true)
	require.Equal(t, string(platform.PointsOff), a.PointsMode, "stored default")
	e.fake.Lookups["xqc"] = platformtest.Lookup{Stream: platform.Stream{Channel: "xqc", ChannelID: "1"}, Live: true}
	e.setForce(t, []store.ForceChannel{{Platform: "kick", Channel: "xqc", Accounts: []string{"a1"}}})

	cfg := e.ro.runnerConfig(e.ctx, a, e.fake, platform.Session{}, nil)
	assert.Equal(t, []string{"xqc"}, cfg.ForceChannels())
	assert.Equal(t, platform.PointsDropsPoints, cfg.PointsMode())

	e.boot(t)
	require.Eventually(t, func() bool {
		s, ok := snapshotOf(t, e, "a1")
		return ok && s.State == runner.StateForceWatching && s.Channel == "xqc"
	}, 5*time.Second, 20*time.Millisecond)
}

// A Kick account is never points only, even with that mode stored: a live
// drop wins over its force channel, and a games edit rebuilds it.
func TestRoster_KickNeverPointsOnly(t *testing.T) {
	e := newRosterEnv(t)
	e.withForce()
	e.account(t, "a1", 1, true)
	e.setPoints(t, platform.PointsOnly, "a1")
	e.fake.Lookups["xqc"] = platformtest.Lookup{Stream: platform.Stream{Channel: "xqc", ChannelID: "1"}, Live: true}
	e.setForce(t, []store.ForceChannel{{Platform: "kick", Channel: "xqc", Accounts: []string{"a1"}}})
	a, err := e.q.GetAccount(e.ctx, "a1")
	require.NoError(t, err)
	cfg := e.ro.runnerConfig(e.ctx, a, e.fake, platform.Session{}, nil)
	assert.Equal(t, platform.PointsDropsPoints, cfg.PointsMode())

	e.chooseRust(t)
	e.rustDrop(10, false)
	builds := 0
	var mu sync.Mutex
	e.ro.backendFor = func(gen.Account) (platform.Backend, error) {
		mu.Lock()
		defer mu.Unlock()
		builds++
		return e.fake, nil
	}
	e.boot(t)
	require.Eventually(t, func() bool {
		s, ok := snapshotOf(t, e, "a1")
		return ok && s.State == runner.StateWatching && s.Channel == "ch"
	}, 5*time.Second, 20*time.Millisecond, "mines the drop, not the force channel")

	e.ro.gamesChanged()
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return builds == 2
	}, 5*time.Second, 20*time.Millisecond, "a games edit rebuilds it")
	_, stale := e.ro.staleGames.Load("a1")
	assert.False(t, stale)
}

// Skip sleep and points mode reach a watching runner live: no rebuild, and
// the watch is not restarted.
func TestRoster_AccountSettingsChangedKeepsWatch(t *testing.T) {
	e := newRosterEnv(t)
	e.account(t, "a1", 1, true)
	e.chooseRust(t)
	e.rustDrop(10, false)
	builds := 0
	var mu sync.Mutex
	e.ro.backendFor = func(gen.Account) (platform.Backend, error) {
		mu.Lock()
		defer mu.Unlock()
		builds++
		return e.fake, nil
	}
	e.boot(t)
	require.Eventually(t, func() bool {
		s, ok := snapshotOf(t, e, "a1")
		return ok && s.State == runner.StateWatching && s.Channel == "ch"
	}, 5*time.Second, 20*time.Millisecond)
	mu.Lock()
	builds = 0
	mu.Unlock()

	require.NoError(t, e.q.SetAccountSkipSleep(e.ctx, gen.SetAccountSkipSleepParams{
		SkipSleep: 1, UpdatedAt: time.Now().Unix(), ID: "a1",
	}))
	e.ro.accountSettingsChanged("a1")
	e.pointsOn(t, "a1")
	e.ro.accountSettingsChanged("a1")
	assert.Equal(t, liveFlags{skipSleep: true, points: platform.PointsDropsPoints},
		e.ro.flags.get(gen.Account{ID: "a1"}), "the live flags follow the row")
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	assert.Zero(t, builds, "no rebuild")
	mu.Unlock()
	s, ok := snapshotOf(t, e, "a1")
	require.True(t, ok)
	assert.Equal(t, runner.StateWatching, s.State)
	assert.Equal(t, "ch", s.Channel)
	assert.Equal(t, 1, e.fake.CallCount("Start:ch"))
	assert.Zero(t, e.fake.CallCount("Stop:ch"), "the watch is not restarted")
}

// A force-list edit rebuilds only a runner that has exited as no_games, so
// it can start force watching. Other runners take the edit live: an idle one
// re-picks, a force-watching one rechecks in place, one mining a drop is left
// alone.
func TestRoster_ForceChangedRebuildsOnlyExited(t *testing.T) {
	e := newRosterEnv(t)
	e.withForce()
	for _, id := range []string{"a1", "a2", "a3", "a4"} {
		e.account(t, id, 1, true)
		// Own empty lists: these never mine a drop.
		require.NoError(t, e.ro.own.Set(e.ctx, id, true, nil))
	}
	// a5 mines the global list and is watching a drop.
	e.account(t, "a5", 1, true)
	e.pointsOn(t, "a1", "a2", "a3", "a4", "a5")
	e.chooseRust(t)
	e.rustDrop(10, false)
	e.fake.Lookups["live3"] = platformtest.Lookup{Stream: platform.Stream{Channel: "live3", ChannelID: "3"}, Live: true}
	e.setForce(t, []store.ForceChannel{
		{Platform: "kick", Channel: "off1", Accounts: []string{"a1"}},
		{Platform: "kick", Channel: "live3", Accounts: []string{"a3"}},
	})
	builds := map[string]int{}
	var mu sync.Mutex
	e.ro.backendFor = func(a gen.Account) (platform.Backend, error) {
		mu.Lock()
		defer mu.Unlock()
		builds[a.ID]++
		return e.fake, nil
	}
	e.boot(t)
	for id, want := range map[string]runner.State{
		"a1": runner.StateSleeping, "a2": runner.StateNoGames,
		"a3": runner.StateForceWatching, "a5": runner.StateWatching,
	} {
		require.Eventually(t, func() bool {
			s, ok := snapshotOf(t, e, id)
			return ok && s.State == want
		}, 5*time.Second, 20*time.Millisecond, id)
	}
	mu.Lock()
	clear(builds)
	mu.Unlock()

	// a1 leaves off1, a2 and a5 join it; live3 moves below off1 and keeps
	// a3, which is force watching. a5 is mining a drop, so it is left alone.
	e.setForce(t, []store.ForceChannel{
		{Platform: "kick", Channel: "off1", Accounts: []string{"a2", "a5"}},
		{Platform: "kick", Channel: "live3", Accounts: []string{"a3"}},
	})
	for range 3 {
		e.ro.forceChanged()
	}
	assert.Len(t, e.ro.forceQueue.wake, 1, "requests fold into one wake")
	mu.Lock()
	assert.Empty(t, builds, "forceChanged returns before any reload")
	mu.Unlock()

	go e.ro.runForceReloader()
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return builds["a2"] == 1
	}, 5*time.Second, 20*time.Millisecond)
	// a1 takes the edit live: rescanned, it has nothing left and idles.
	require.Eventually(t, func() bool {
		s, ok := snapshotOf(t, e, "a1")
		return ok && s.State == runner.StateNoGames
	}, 5*time.Second, 20*time.Millisecond)
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	assert.Equal(t, map[string]int{"a2": 1}, builds, "only the exited runner is rebuilt")
	mu.Unlock()
	s, ok := snapshotOf(t, e, "a3")
	require.True(t, ok)
	assert.Equal(t, runner.StateForceWatching, s.State)
	assert.Equal(t, "live3", s.Channel)
	e.fake.Mu.Lock()
	assert.NotContains(t, e.fake.Calls, "Stop:live3", "the force watch is not restarted")
	e.fake.Mu.Unlock()
	s, ok = snapshotOf(t, e, "a5")
	require.True(t, ok)
	assert.Equal(t, runner.StateWatching, s.State)
}

// lookupBackend is a Fake whose LookupChannel is replaced.
type lookupBackend struct {
	*platformtest.Fake
	lookup func(login string) (platform.Stream, bool, error)
}

func (b lookupBackend) LookupChannel(_ context.Context, _ platform.Session, login string) (platform.Stream, bool, error) {
	return b.lookup(login)
}

// noLookBackend hides every optional interface, LookupChannel included.
type noLookBackend struct{ platform.Backend }

func TestRoster_ForceLookupFallsThrough(t *testing.T) {
	e := newRosterEnv(t)
	e.withForce()
	var mu sync.Mutex
	calls := []string{}
	backends := map[string]platform.Backend{
		"a0": lookupBackend{e.fake, func(string) (platform.Stream, bool, error) {
			mu.Lock()
			defer mu.Unlock()
			calls = append(calls, "a0")
			return platform.Stream{}, true, nil
		}},
		"a1": lookupBackend{e.fake, func(string) (platform.Stream, bool, error) {
			mu.Lock()
			defer mu.Unlock()
			calls = append(calls, "a1")
			return platform.Stream{}, false, errors.New("boom")
		}},
		"a2": lookupBackend{e.fake, func(login string) (platform.Stream, bool, error) {
			mu.Lock()
			defer mu.Unlock()
			calls = append(calls, "a2")
			return platform.Stream{Channel: login, ViewerCount: 7}, true, nil
		}},
	}
	e.ro.backendFor = func(a gen.Account) (platform.Backend, error) { return backends[a.ID], nil }
	for _, id := range []string{"a0", "a1", "a2"} {
		e.account(t, id, 1, true)
	}
	e.boot(t)
	// a0 has a loaded session but its runner now needs a login: skipped.
	a0, err := e.q.GetAccount(e.ctx, "a0")
	require.NoError(t, err)
	e.ro.pool.ReloadAccount("a0", func() runner.Entry { return e.ro.idle(a0, runner.StateNeedsLogin) })
	e.ro.loaded.set("a0", loadedSession{backend: backends["a0"]})

	s, live, err := e.ro.lookupForce(e.ctx, "kick", "xqc")
	require.NoError(t, err)
	assert.True(t, live)
	assert.Equal(t, 7, s.ViewerCount, "result from the second candidate")
	mu.Lock()
	assert.Equal(t, []string{"a1", "a2"}, calls)
	mu.Unlock()

	// Every candidate fails: the last error comes back.
	backends["a2"] = backends["a1"]
	e.ro.builder(gen.Account{ID: "a2"})()
	_, _, err = e.ro.lookupForce(e.ctx, "kick", "xqc")
	assert.EqualError(t, err, "boom")
}

func TestRoster_ForceLookupNoLookerWarnsOnce(t *testing.T) {
	e := newRosterEnv(t)
	e.withForce()
	var buf bytes.Buffer
	e.ro.logger = slog.New(slog.NewTextHandler(&buf, nil))
	e.ro.backendFor = func(gen.Account) (platform.Backend, error) { return noLookBackend{e.fake}, nil }
	a := e.account(t, "a1", 1, true)
	e.ro.builder(a)()
	for range 3 {
		_, _, err := e.ro.lookupForce(e.ctx, "kick", "xqc")
		require.ErrorIs(t, err, errNoForceLookupAccount)
	}
	assert.Equal(t, 1, strings.Count(buf.String(), "cannot look up channels"))
}

func TestRoster_ForceChangedWarmsNewChannels(t *testing.T) {
	e := newRosterEnv(t)
	e.withForce()
	a := e.account(t, "a1", 1, true)
	e.ro.builder(a)()
	e.setForce(t, []store.ForceChannel{{Platform: "kick", Channel: "old1", Accounts: []string{"a1"}}})
	e.ro.forceList() // cache old1
	e.setForce(t, []store.ForceChannel{
		{Platform: "kick", Channel: "old1", Accounts: []string{"a1"}},
		{Platform: "kick", Channel: "new1"},
	})
	e.ro.forceChanged()
	require.Eventually(t, func() bool {
		e.fake.Mu.Lock()
		defer e.fake.Mu.Unlock()
		return slices.Contains(e.fake.Calls, "Lookup:new1")
	}, 5*time.Second, 20*time.Millisecond)
	e.fake.Mu.Lock()
	assert.NotContains(t, e.fake.Calls, "Lookup:old1", "only new channels are warmed")
	e.fake.Mu.Unlock()
}

func TestRoster_ForceChangedReadFailureRetries(t *testing.T) {
	e := newRosterEnv(t)
	e.withForce()
	e.account(t, "a1", 1, true)
	e.setForce(t, []store.ForceChannel{{Platform: "kick", Channel: "xqc", Accounts: []string{"a1"}}})
	assert.Equal(t, []string{"xqc"}, e.ro.forceFor("a1", "kick"))
	_, err := e.db.Exec(`ALTER TABLE force_channels RENAME TO force_channels_gone`)
	require.NoError(t, err)
	e.ro.forceChanged()
	assert.Nil(t, e.ro.forceFor("a1", "kick"), "failed read gives nil")
	_, err = e.db.Exec(`ALTER TABLE force_channels_gone RENAME TO force_channels`)
	require.NoError(t, err)
	assert.Equal(t, []string{"xqc"}, e.ro.forceFor("a1", "kick"), "next read retries")
}

// A build whose row read began before a saved change does not overwrite the
// flags that change stored.
func TestAccountFlags_StaleBuildKeepsSavedChange(t *testing.T) {
	var f accountFlags
	v := f.version("a1") // a build starts reading the row
	sv := f.bump("a1")   // a save lands and stores its flags
	f.set(gen.Account{ID: "a1", SkipSleep: 1, PointsMode: string(platform.PointsDropsPoints)}, sv)
	f.set(gen.Account{ID: "a1"}, v) // the build stores its older row
	assert.Equal(t, liveFlags{skipSleep: true, points: platform.PointsDropsPoints},
		f.get(gen.Account{ID: "a1"}))
}

// setPoints sets each account's points mode.
func (e *rosterEnv) setPoints(t *testing.T, mode platform.PointsMode, ids ...string) {
	t.Helper()
	for _, id := range ids {
		require.NoError(t, e.q.SetAccountPointsMode(e.ctx, gen.SetAccountPointsModeParams{
			PointsMode: string(mode), UpdatedAt: time.Now().Unix(), ID: id,
		}))
	}
}

// A points-only account with no games but a force list gets a runner and
// watches its channel, with no campaign scan.
func TestRoster_PointsOnlyNoGamesWatches(t *testing.T) {
	e := newRosterEnv(t)
	e.withForce()
	e.twitchAccount(t, "a1")
	e.setPoints(t, platform.PointsOnly, "a1")
	e.fake.Lookups["xqc"] = platformtest.Lookup{Stream: platform.Stream{Channel: "xqc", ChannelID: "1"}, Live: true}
	e.setForce(t, []store.ForceChannel{{Platform: "twitch", Channel: "xqc", Accounts: []string{"a1"}}})
	e.boot(t)
	require.Eventually(t, func() bool {
		s, ok := snapshotOf(t, e, "a1")
		return ok && s.State == runner.StateForceWatching && s.Channel == "xqc"
	}, 5*time.Second, 20*time.Millisecond)
	assert.Zero(t, e.fake.CallCount("List"), "no campaign scan")
}

// Switching a no_games account to points only rebuilds it once: with no
// force channel it idles as no_channels, and a later force list edit
// reaches it live, without another rebuild.
func TestRoster_PointsOnlyFromNoGames(t *testing.T) {
	e := newRosterEnv(t)
	e.withForce()
	e.twitchAccount(t, "a1")
	builds := 0
	var mu sync.Mutex
	e.ro.backendFor = func(gen.Account) (platform.Backend, error) {
		mu.Lock()
		defer mu.Unlock()
		builds++
		return e.fake, nil
	}
	e.boot(t)
	require.Eventually(t, func() bool {
		s, ok := snapshotOf(t, e, "a1")
		return ok && s.State == runner.StateNoGames
	}, 5*time.Second, 20*time.Millisecond)

	e.setPoints(t, platform.PointsOnly, "a1")
	e.ro.accountSettingsChanged("a1")
	require.Eventually(t, func() bool {
		s, ok := snapshotOf(t, e, "a1")
		return ok && s.State == runner.StateNoChannels
	}, 5*time.Second, 20*time.Millisecond)

	e.fake.Lookups["xqc"] = platformtest.Lookup{Stream: platform.Stream{Channel: "xqc", ChannelID: "1"}, Live: true}
	e.setForce(t, []store.ForceChannel{{Platform: "twitch", Channel: "xqc", Accounts: []string{"a1"}}})
	e.ro.forceChanged()
	require.Eventually(t, func() bool {
		s, ok := snapshotOf(t, e, "a1")
		return ok && s.State == runner.StateForceWatching && s.Channel == "xqc"
	}, 5*time.Second, 20*time.Millisecond)
	mu.Lock()
	assert.Equal(t, 2, builds, "boot plus the one points-only rebuild")
	mu.Unlock()
	assert.Zero(t, e.fake.CallCount("List"), "no campaign scan")
}

// A games edit does not rebuild a points-only runner, which reads no games:
// its force watch keeps going. Leaving points only afterwards rebuilds it
// with the current games, so it mines them.
func TestRoster_GamesChangedSkipsPointsOnly(t *testing.T) {
	e := newRosterEnv(t)
	e.withForce()
	e.twitchAccount(t, "a1")
	e.setPoints(t, platform.PointsOnly, "a1")
	e.fake.Lookups["xqc"] = platformtest.Lookup{Stream: platform.Stream{Channel: "xqc", ChannelID: "1"}, Live: true}
	e.setForce(t, []store.ForceChannel{{Platform: "twitch", Channel: "xqc", Accounts: []string{"a1"}}})
	builds := 0
	var mu sync.Mutex
	e.ro.backendFor = func(gen.Account) (platform.Backend, error) {
		mu.Lock()
		defer mu.Unlock()
		builds++
		return e.fake, nil
	}
	e.boot(t)
	require.Eventually(t, func() bool {
		s, ok := snapshotOf(t, e, "a1")
		return ok && s.State == runner.StateForceWatching && s.Channel == "xqc"
	}, 5*time.Second, 20*time.Millisecond)

	e.chooseRust(t)
	e.rustDrop(10, false)
	e.ro.gamesChanged()
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	assert.Equal(t, 1, builds, "no rebuild on a games edit")
	mu.Unlock()
	assert.Zero(t, e.fake.CallCount("Stop:xqc"), "the force watch is not restarted")

	e.setPoints(t, platform.PointsDropsPoints, "a1")
	e.ro.accountSettingsChanged("a1")
	require.Eventually(t, func() bool {
		s, ok := snapshotOf(t, e, "a1")
		return ok && s.State == runner.StateWatching && s.Channel == "ch"
	}, 5*time.Second, 20*time.Millisecond, "mines the games chosen while points only")
	mu.Lock()
	assert.Equal(t, 2, builds, "one rebuild on leaving points only")
	mu.Unlock()
}

// failGetAccount fails the next n GetAccount reads with a store error.
type failGetAccount struct {
	gen.DBTX
	mu sync.Mutex
	n  int
}

func (d *failGetAccount) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	d.mu.Lock()
	fail := d.n > 0 && strings.Contains(query, "-- name: GetAccount :one")
	if fail {
		d.n--
	}
	d.mu.Unlock()
	if fail {
		return d.DBTX.QueryRowContext(ctx, "SELECT * FROM no_such_table")
	}
	return d.DBTX.QueryRowContext(ctx, query, args...)
}

// A points mode save whose row read fails still applies: the account is
// rebuilt from the row instead of keeping its old mode.
func TestRoster_AccountSettingsChangedReadFailureRebuilds(t *testing.T) {
	e := newRosterEnv(t)
	e.withForce()
	e.twitchAccount(t, "a1")
	e.boot(t)
	require.Eventually(t, func() bool {
		s, ok := snapshotOf(t, e, "a1")
		return ok && s.State == runner.StateNoGames
	}, 5*time.Second, 20*time.Millisecond)

	e.setPoints(t, platform.PointsOnly, "a1")
	e.ro.q = gen.New(&failGetAccount{DBTX: e.db, n: 1})
	e.ro.accountSettingsChanged("a1")
	require.Eventually(t, func() bool {
		s, ok := snapshotOf(t, e, "a1")
		return ok && s.State == runner.StateNoChannels
	}, 5*time.Second, 20*time.Millisecond)
	assert.Equal(t, platform.PointsOnly, e.ro.flags.get(gen.Account{ID: "a1"}).points)
}
