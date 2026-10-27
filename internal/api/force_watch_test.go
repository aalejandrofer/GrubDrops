package api

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/runner"
	"github.com/aalejandrofer/grubdrops/internal/store"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

func boolPtr(b bool) *bool { return &b }

// stubLive answers ForceLive from a "platform/login" map; absent = unknown.
func stubLive(m map[string]*bool) func(platform, login string) *bool {
	return func(platform, login string) *bool { return m[platform+"/"+login] }
}

func (e *resEnv) forceList() []store.ForceChannel {
	e.t.Helper()
	l, err := e.s.d.ForceWatch.List(context.Background())
	require.NoError(e.t, err)
	return l
}

func (e *resEnv) setForce(list []store.ForceChannel) {
	e.t.Helper()
	require.NoError(e.t, e.s.d.ForceWatch.Set(context.Background(), list))
}

func TestGetForceWatch(t *testing.T) {
	e := newResEnv(t, func(d *Deps) {
		d.ForceLive = stubLive(map[string]*bool{
			"twitch/keshaeuw": boolPtr(true),
			"kick/xqc":        boolPtr(false),
		})
	})
	e.setForce([]store.ForceChannel{
		{Platform: "twitch", Channel: "keshaeuw", Accounts: []string{"a1"}},
		{Platform: "kick", Channel: "xqc", Accounts: []string{"b2"}},
		{Platform: "twitch", Channel: "unseen"},
	})
	e.pool.snaps = []runner.Snapshot{
		{AccountID: "a1", Platform: "twitch", State: runner.StateForceWatching, Channel: "keshaeuw"},
		// Same channel name, other platform: not a watcher.
		{AccountID: "b2", Platform: "kick", State: runner.StateForceWatching, Channel: "unseen"},
	}
	resp, b := e.do("GET", "/api/force-watch", "", nil)
	require.Equal(t, 200, resp.StatusCode, string(b))
	assert.JSONEq(t, `{"channels":[
		{"platform":"twitch","channel":"keshaeuw","live":true,"accounts":["a1"],"watching":["a1"]},
		{"platform":"kick","channel":"xqc","live":false,"accounts":["b2"],"watching":[]},
		{"platform":"twitch","channel":"unseen","live":null,"accounts":[],"watching":[]}
	]}`, string(b))
}

func TestGetForceWatch_NilLiveAndEmpty(t *testing.T) {
	e := newResEnv(t, nil)
	resp, b := e.do("GET", "/api/force-watch", "", nil)
	require.Equal(t, 200, resp.StatusCode, string(b))
	assert.JSONEq(t, `{"channels":[]}`, string(b))

	e.setForce([]store.ForceChannel{{Platform: "twitch", Channel: "keshaeuw"}})
	resp, b = e.do("GET", "/api/force-watch", "", nil)
	require.Equal(t, 200, resp.StatusCode, string(b))
	assert.JSONEq(t, `{"channels":[{"platform":"twitch","channel":"keshaeuw","live":null,"accounts":[],"watching":[]}]}`, string(b))
}

func TestPutForceWatch_ReplacesAndNotifies(t *testing.T) {
	changed := 0
	e := newResEnv(t, func(d *Deps) {
		d.ForceChanged = func() { changed++ }
		d.ForceLive = stubLive(map[string]*bool{"kick/xqc": boolPtr(true)})
	})
	e.setForce([]store.ForceChannel{{Platform: "twitch", Channel: "old", Accounts: []string{"a1"}}})
	ch, cancel := e.s.d.Hub.Subscribe()
	defer cancel()
	_, tok := e.session()

	body := map[string]any{"channels": []map[string]any{
		{"platform": "kick", "channel": "xqc", "accounts": []string{"b2"}},
		{"platform": "twitch", "channel": "KeshaEUW", "accounts": []string{"a1"}},
	}}
	resp, b := e.do("PUT", "/api/force-watch", tok, body)
	require.Equal(t, 200, resp.StatusCode, string(b))
	assert.JSONEq(t, `{"channels":[
		{"platform":"kick","channel":"xqc","live":true,"accounts":["b2"],"watching":[]},
		{"platform":"twitch","channel":"keshaeuw","live":null,"accounts":["a1"],"watching":[]}
	]}`, string(b))
	assert.Equal(t, []store.ForceChannel{
		{Platform: "kick", Channel: "xqc", Accounts: []string{"b2"}},
		{Platform: "twitch", Channel: "keshaeuw", Accounts: []string{"a1"}},
	}, e.forceList())
	assert.Equal(t, 1, changed)
	select {
	case ev := <-ch:
		assert.Equal(t, EventForceWatch, ev.Name)
	case <-time.After(time.Second):
		t.Fatal("no force_watch event after PUT")
	}

	// An empty list clears it.
	resp, b = e.do("PUT", "/api/force-watch", tok, map[string]any{"channels": []any{}})
	require.Equal(t, 200, resp.StatusCode, string(b))
	assert.JSONEq(t, `{"channels":[]}`, string(b))
	assert.Empty(t, e.forceList())
	assert.Equal(t, 2, changed)
}

func TestPutForceWatch_RejectsPlatformMismatchAndDuplicate(t *testing.T) {
	changed := 0
	e := newResEnv(t, func(d *Deps) { d.ForceChanged = func() { changed++ } })
	before := []store.ForceChannel{{Platform: "twitch", Channel: "keep", Accounts: []string{"a1"}}}
	e.setForce(before)
	ch, cancel := e.s.d.Hub.Subscribe()
	defer cancel()
	_, tok := e.session()

	type row = map[string]any
	cases := map[string]any{
		"platform mismatch": row{"channels": []row{{"platform": "twitch", "channel": "keshaeuw", "accounts": []string{"b2"}}}},
		"duplicate": row{"channels": []row{
			{"platform": "twitch", "channel": "keshaeuw", "accounts": []string{"a1"}},
			{"platform": "twitch", "channel": "keshaeuw", "accounts": []string{}},
		}},
		"bad name":         row{"channels": []row{{"platform": "twitch", "channel": "a", "accounts": []string{"a1"}}}},
		"unknown platform": row{"channels": []row{{"platform": "youtube", "channel": "keshaeuw", "accounts": []string{}}}},
		"unknown account":  row{"channels": []row{{"platform": "twitch", "channel": "keshaeuw", "accounts": []string{"ghost"}}}},
		"missing list":     row{},
		"unknown field":    row{"channels": []row{}, "extra": 1},
	}
	for name, body := range cases {
		resp, b := e.do("PUT", "/api/force-watch", tok, body)
		assert.Equal(t, 400, resp.StatusCode, name)
		assert.JSONEq(t, `{"error":"bad_request"}`, string(b), name)
		assert.Equal(t, before, e.forceList(), name)
	}

	// No CSRF token: refused before the handler.
	resp, b := e.do("PUT", "/api/force-watch", "", map[string]any{"channels": []any{}})
	assert.Equal(t, 403, resp.StatusCode)
	assert.JSONEq(t, `{"error":"csrf"}`, string(b))
	assert.Equal(t, before, e.forceList())

	assert.Equal(t, 0, changed)
	select {
	case ev := <-ch:
		t.Fatalf("unexpected event %q", ev.Name)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestOverview_ForceWatch(t *testing.T) {
	live := map[string]*bool{}
	e := newResEnv(t, func(d *Deps) { d.ForceLive = stubLive(live) })

	m := e.overview()
	v, ok := m["force_watch"]
	require.True(t, ok, "force_watch key present")
	assert.Nil(t, v, "empty list gives null")

	e.setForce([]store.ForceChannel{
		{Platform: "twitch", Channel: "one", Accounts: []string{"a1"}},
		{Platform: "kick", Channel: "two", Accounts: []string{"b2"}},
		{Platform: "twitch", Channel: "three", Accounts: []string{"a1"}},
		{Platform: "kick", Channel: "four"},
		{Platform: "twitch", Channel: "five"},
	})

	// None live: empty lines, total still counts the list.
	m = e.overview()
	b, _ := json.Marshal(m["force_watch"])
	assert.JSONEq(t, `{"live":[],"more":0,"total":5}`, string(b))

	// Four live, the third watched: watched first, then panel order, two max.
	for _, k := range []string{"twitch/one", "kick/two", "twitch/three", "kick/four"} {
		live[k] = boolPtr(true)
	}
	live["twitch/five"] = boolPtr(false)
	e.pool.snaps = []runner.Snapshot{
		{AccountID: "a1", Platform: "twitch", State: runner.StateForceWatching, Channel: "three"},
		{AccountID: "b2", Platform: "kick", State: runner.StateWatching, Channel: "two"},
	}
	m = e.overview()
	b, _ = json.Marshal(m["force_watch"])
	assert.JSONEq(t, `{"live":[
		{"platform":"twitch","channel":"three","watchers":["Alice"]},
		{"platform":"twitch","channel":"one","watchers":[]}
	],"more":2,"total":5}`, string(b))
}

func TestOverview_ForceWatchWatchedUnknownIsLive(t *testing.T) {
	e := newResEnv(t, nil) // no ForceLive: every channel unknown
	e.setForce([]store.ForceChannel{
		{Platform: "twitch", Channel: "one", Accounts: []string{"a1"}},
		{Platform: "kick", Channel: "two"},
	})
	e.pool.snaps = []runner.Snapshot{
		{AccountID: "a1", Platform: "twitch", State: runner.StateForceWatching, Channel: "one"},
	}
	b, _ := json.Marshal(e.overview()["force_watch"])
	assert.JSONEq(t, `{"live":[{"platform":"twitch","channel":"one","watchers":["Alice"]}],"more":0,"total":2}`, string(b))
}

func TestOverview_NoGamesButForceWatch(t *testing.T) {
	e := newResEnv(t, nil)
	require.NoError(t, e.s.d.Chosen.Set(context.Background(), nil))
	state := func() string { return e.overview()["status"].(map[string]any)["state"].(string) }
	assert.Equal(t, "no_games", state(), "no games and no force list")

	// A force watching runner: not no_games, and not counted as mining.
	e.pool.snaps = []runner.Snapshot{
		{AccountID: "a1", Platform: "twitch", State: runner.StateForceWatching, Channel: "one"},
	}
	m := e.overview()
	st := m["status"].(map[string]any)
	assert.NotEqual(t, "no_games", st["state"])
	assert.EqualValues(t, 0, st["accounts_mining"])

	// A force list with nobody watching yet: still not no_games.
	e.pool.snaps = nil
	e.setForce([]store.ForceChannel{{Platform: "twitch", Channel: "one", Accounts: []string{"a1"}}})
	assert.NotEqual(t, "no_games", state())

	// Channels no account is assigned to cannot be watched: no_games.
	e.setForce([]store.ForceChannel{
		{Platform: "twitch", Channel: "one"},
		{Platform: "kick", Channel: "two"},
	})
	assert.Equal(t, "no_games", state(), "no channel has an account")

	// Only a paused account assigned: still nothing to watch.
	e.setForce([]store.ForceChannel{{Platform: "twitch", Channel: "one", Accounts: []string{"a1"}}})
	require.NoError(t, e.s.d.Q.SetAccountEnabled(context.Background(), gen.SetAccountEnabledParams{ID: "a1", Enabled: 0}))
	assert.Equal(t, "no_games", state(), "only a paused account assigned")
}

func TestPutForceWatch_NormalizesPlatform(t *testing.T) {
	e := newResEnv(t, nil)
	_, tok := e.session()
	resp, b := e.do("PUT", "/api/force-watch", tok, map[string]any{"channels": []map[string]any{
		{"platform": " Twitch ", "channel": "keshaeuw", "accounts": []string{"a1"}},
	}})
	require.Equal(t, 200, resp.StatusCode, string(b))
	assert.Equal(t, []store.ForceChannel{{Platform: "twitch", Channel: "keshaeuw", Accounts: []string{"a1"}}}, e.forceList())
}
