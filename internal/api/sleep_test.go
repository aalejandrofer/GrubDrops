package api

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/plan"
	"github.com/aalejandrofer/grubdrops/internal/runner"
	"github.com/aalejandrofer/grubdrops/internal/store"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

func TestSettings_SleepWindow(t *testing.T) {
	e := newSettingsEnv(t)
	ctx := context.Background()
	_, b := e.do("GET", "/settings", nil)
	assert.JSONEq(t, `{"enabled":false,"start":"02:00","end":"08:00"}`, jsonPath(t, b, "sleep"))

	code, b := e.do("PUT", "/settings", map[string]any{"sleep": map[string]any{"enabled": true, "start": "23:30", "end": "07:00"}})
	require.Equal(t, 200, code, string(b))
	assert.JSONEq(t, `{"enabled":true,"start":"23:30","end":"07:00"}`, jsonPath(t, b, "sleep"))
	assert.Equal(t, 1, e.live, "a sleep change reaches the runners live")
	assert.Equal(t, 0, e.rescans, "a sleep change rescans nothing")
	w, err := e.st.SleepWindow(ctx)
	require.NoError(t, err)
	assert.Equal(t, store.SleepWindow{Enabled: true, Start: "23:30", End: "07:00"}, w)

	// Partial: only the start moves.
	code, b = e.do("PUT", "/settings", map[string]any{"sleep": map[string]any{"start": "22:00"}})
	require.Equal(t, 200, code, string(b))
	assert.JSONEq(t, `{"enabled":true,"start":"22:00","end":"07:00"}`, jsonPath(t, b, "sleep"))
	assert.Equal(t, 2, e.live)

	// Unchanged: no nudge.
	code, _ = e.do("PUT", "/settings", map[string]any{"sleep": map[string]any{"enabled": true, "start": "22:00", "end": "07:00"}})
	require.Equal(t, 200, code)
	assert.Equal(t, 2, e.live)

	// Turning it off is a change.
	code, _ = e.do("PUT", "/settings", map[string]any{"sleep": map[string]any{"enabled": false}})
	require.Equal(t, 200, code)
	assert.Equal(t, 3, e.live)
	assert.Equal(t, 0, e.rescans)
}

func TestSettings_SleepWindowInvalidSavesNothing(t *testing.T) {
	e := newSettingsEnv(t)
	ctx := context.Background()
	require.NoError(t, e.st.SetSleepWindow(ctx, store.SleepWindow{Enabled: true, Start: "23:30", End: "07:00"}))
	for _, sleep := range []map[string]any{
		{"start": "25:00"},
		{"end": "7:00"},
		{"start": "07:00"}, // equals the saved end
		{"start": "10:00", "end": "10:00"},
		{"enabled": true, "start": ""},
	} {
		code, b := e.do("PUT", "/settings", map[string]any{"theme": "light", "sleep": sleep})
		assert.Equal(t, 400, code, "%v", sleep)
		assert.JSONEq(t, `{"error":"bad_request"}`, string(b))
		th, _ := e.st.UITheme(ctx)
		assert.Equal(t, "dark", th)
		w, _ := e.st.SleepWindow(ctx)
		assert.Equal(t, store.SleepWindow{Enabled: true, Start: "23:30", End: "07:00"}, w)
	}
	assert.Equal(t, 0, e.updates)
	assert.Equal(t, 0, e.rescans)
	assert.Equal(t, 0, e.live)
}

// The sleep window is read in the display zone: a zone change reaches the
// runners live, with no rescan.
func TestSettings_TimezoneNudgesLive(t *testing.T) {
	e := newSettingsEnv(t)
	code, _ := e.do("PUT", "/settings", map[string]any{"timezone": "Asia/Tokyo"})
	require.Equal(t, 200, code)
	assert.Equal(t, 1, e.live)
	code, _ = e.do("PUT", "/settings", map[string]any{"timezone": "Asia/Tokyo"})
	require.Equal(t, 200, code)
	assert.Equal(t, 1, e.live)
	assert.Equal(t, 0, e.rescans)
}

func jsonPath(t *testing.T, b []byte, key string) string {
	t.Helper()
	var m map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(b, &m), string(b))
	v, ok := m[key]
	require.True(t, ok, "no %q in %s", key, b)
	return string(v)
}

func TestAccounts_SkipSleep(t *testing.T) {
	e, c := accountsEnv(t)
	id := e.createAccount("twitch", "Alpha")
	ctx := context.Background()
	_, tok := e.session()

	skip := func() bool {
		t.Helper()
		_, body := e.do("GET", "/api/accounts", "", nil)
		var list []map[string]any
		unmarshal(t, body, &list)
		require.Len(t, list, 1)
		v, ok := list[0]["skip_sleep"].(bool)
		require.True(t, ok, "skip_sleep missing: %v", list[0])
		return v
	}
	assert.False(t, skip(), "off by default")
	before := len(c.changedList())
	live := len(c.settingsList())

	resp, b := e.do("PUT", "/api/accounts/"+id+"/skip-sleep", tok, map[string]bool{"skip_sleep": true})
	require.Equal(t, 204, resp.StatusCode, string(b))
	a, err := e.s.d.Q.GetAccount(ctx, id)
	require.NoError(t, err)
	assert.EqualValues(t, 1, a.SkipSleep)
	assert.True(t, skip())
	assert.Equal(t, before, len(c.changedList()), "no rebuild")
	assert.Equal(t, []string{id}, c.settingsList()[live:], "the runner takes the flag live")

	resp, _ = e.do("PUT", "/api/accounts/"+id+"/skip-sleep", tok, map[string]bool{"skip_sleep": false})
	require.Equal(t, 204, resp.StatusCode)
	assert.False(t, skip())
	assert.Equal(t, before, len(c.changedList()))
	assert.Len(t, c.settingsList(), live+2)

	resp, _ = e.do("PUT", "/api/accounts/"+id+"/skip-sleep", tok, map[string]any{})
	assert.Equal(t, 400, resp.StatusCode)
	resp, _ = e.do("PUT", "/api/accounts/nope/skip-sleep", tok, map[string]bool{"skip_sleep": true})
	assert.Equal(t, 404, resp.StatusCode)
	resp, _ = e.do("PUT", "/api/accounts/"+id+"/skip-sleep", "", map[string]bool{"skip_sleep": true})
	assert.Equal(t, 403, resp.StatusCode)
}

func TestRunnerJSON_SleepUntil(t *testing.T) {
	wake := time.Date(2026, 10, 31, 7, 12, 0, 0, time.UTC)
	d := RunnerJSON(runner.Snapshot{AccountID: "a1", State: runner.StateSleepWindow, SleepUntil: wake}, gen.Account{ID: "a1"}, false)
	require.NotNil(t, d.SleepUntil)
	assert.Equal(t, "2026-10-31T07:12:00Z", *d.SleepUntil)
	assert.Equal(t, "sleep_window", d.State)

	b, err := json.Marshal(RunnerJSON(runner.Snapshot{AccountID: "a1", State: runner.StateWatching}, gen.Account{ID: "a1"}, false))
	require.NoError(t, err)
	assert.Equal(t, "null", jsonPath(t, b, "sleep_until"))
}

func TestRunners_MineNowSleepWindow(t *testing.T) {
	e := newResEnv(t, nil)
	e.pool.snaps = []runner.Snapshot{{
		AccountID: "a1", Platform: "twitch", State: runner.StateSleepWindow,
		UpNext: []plan.Target{tgt("G", "n1", 0)},
	}}
	_, tok := e.session()
	resp, b := e.do("POST", "/api/runners/a1/mine-now", tok, nil)
	assert.Equal(t, 409, resp.StatusCode)
	assert.JSONEq(t, `{"error":"sleep_window"}`, string(b))
	assert.Empty(t, e.pool.mineNow)
}

func TestOverview_SleepWindow(t *testing.T) {
	e := newResEnv(t, nil)
	early := time.Date(2026, 10, 31, 7, 5, 0, 0, time.UTC)
	late := time.Date(2026, 10, 31, 7, 15, 0, 0, time.UTC)
	e.pool.snaps = []runner.Snapshot{
		{AccountID: "a1", State: runner.StateSleepWindow, SleepUntil: late, NextScanAt: late, UpNext: []plan.Target{tgt("G", "n1", 0)}},
		{AccountID: "b2", State: runner.StateSleepWindow, SleepUntil: early, NextScanAt: early},
	}
	st := e.overview()["status"].(map[string]any)
	assert.Equal(t, "sleep_window", st["state"])
	assert.Equal(t, "2026-10-31T07:05:00Z", st["sleep_until"])
	assert.Equal(t, "2026-10-31T07:05:00Z", st["next_scan_at"])

	// One account awake and not skipping the window: not the whole app asleep.
	e.pool.snaps[1] = runner.Snapshot{AccountID: "b2", State: runner.StateSleeping}
	st = e.overview()["status"].(map[string]any)
	assert.Equal(t, "idle", st["state"])
	assert.Nil(t, st["sleep_until"])

	// That account skips the window: the rest of the app still sleeps.
	_, err := e.db.Exec("UPDATE accounts SET skip_sleep = 1 WHERE id = 'b2'")
	require.NoError(t, err)
	st = e.overview()["status"].(map[string]any)
	assert.Equal(t, "sleep_window", st["state"])
	assert.Equal(t, "2026-10-31T07:15:00Z", st["sleep_until"])

	// A skipping account that mines makes the app mining.
	e.pool.snaps[1] = runner.Snapshot{AccountID: "b2", State: runner.StateWatching}
	assert.Equal(t, "mining", e.overview()["status"].(map[string]any)["state"])

	// A paused account does not keep the app awake.
	_, err = e.db.Exec("UPDATE accounts SET skip_sleep = 0, enabled = 0 WHERE id = 'b2'")
	require.NoError(t, err)
	e.pool.snaps[1] = runner.Snapshot{AccountID: "b2", State: runner.StateIdle}
	assert.Equal(t, "sleep_window", e.overview()["status"].(map[string]any)["state"])
}
