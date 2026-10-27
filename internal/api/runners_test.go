package api

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/plan"
	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/runner"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

func tgt(game, bid string, mins int) plan.Target {
	return plan.Target{
		Campaign: platform.Campaign{
			ID: "c-" + bid, Platform: "twitch", Game: game, Name: "Camp " + bid,
			EndsAt: time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC),
		},
		Benefit: platform.DropBenefit{ID: bid, Name: "Item " + bid, RequiredMinutes: 60, ImageURL: "http://img/" + bid},
		Minutes: mins,
	}
}

func (e *resEnv) runners() []map[string]any {
	e.t.Helper()
	var out []map[string]any
	e.getJSON("/api/runners", &out)
	return out
}

func TestRunners_ShapeAndOrderCustom(t *testing.T) {
	e := newResEnv(t, nil)
	cur, n1, n2 := tgt("Game", "cur", 12), tgt("Game", "n1", 0), tgt("Game", "n2", 5)
	started := time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)
	e.pool.snaps = []runner.Snapshot{{
		AccountID: "a1", Platform: "twitch", State: runner.StateWatching, Current: &cur,
		Channel: "streamer", ViewerCount: 42, MinutesWatched: 12, StartedAt: started,
		UpNext:  []plan.Target{n1, n2},
		Blocked: []plan.Blocked{{Target: tgt("Other", "bl", 0), Reason: plan.ReasonNotLinked}},
	}}
	rs := e.runners()
	require.Len(t, rs, 1)
	r := rs[0]
	assert.Equal(t, "a1", r["account_id"])
	assert.Equal(t, "Alice", r["account_name"])
	assert.Equal(t, "twitch", r["platform"])
	assert.Equal(t, "watching", r["state"])
	assert.Equal(t, "streamer", r["channel"])
	assert.EqualValues(t, 42, r["viewer_count"])
	assert.EqualValues(t, 12, r["minutes"])
	assert.Equal(t, "2026-10-25T09:00:00Z", r["started_at"])
	assert.Nil(t, r["last_scan_at"])
	assert.Equal(t, false, r["order_custom"])
	assert.Equal(t, map[string]any{
		"game": "Game", "platform": "twitch", "campaign_id": "c-cur", "campaign_name": "Camp cur",
		"benefit_id": "cur", "benefit_name": "Item cur", "image_url": "http://img/cur",
		"required_minutes": float64(60), "minutes": float64(12), "ends_at": "2026-11-01T12:00:00Z",
		"channels": []any{}, "channel_count": float64(0),
	}, r["current"])
	up := r["up_next"].([]any)
	require.Len(t, up, 2)
	assert.Equal(t, "n1", up[0].(map[string]any)["benefit_id"])
	bl := r["blocked"].([]any)
	require.Len(t, bl, 1)
	assert.Equal(t, "not_linked", bl[0].(map[string]any)["reason"])
	assert.Equal(t, "bl", bl[0].(map[string]any)["target"].(map[string]any)["benefit_id"])

	e.pool.SetOrder("a1", []string{"n2"})
	r = e.runners()[0]
	assert.Equal(t, true, r["order_custom"])
	assert.Equal(t, "n2", r["up_next"].([]any)[0].(map[string]any)["benefit_id"])
}

func TestRunners_NullsAndEmptyLists(t *testing.T) {
	e := newResEnv(t, nil)
	e.pool.snaps = []runner.Snapshot{{AccountID: "b2", Platform: "kick", State: runner.StateIdle}}
	r := e.runners()[0]
	assert.Nil(t, r["current"])
	assert.Nil(t, r["started_at"])
	assert.Equal(t, []any{}, r["up_next"])
	assert.Equal(t, []any{}, r["blocked"])
}

func TestRunnerJSON_KickAvatarProxied(t *testing.T) {
	sn := runner.Snapshot{AccountID: "k1", Platform: "kick", State: runner.StateIdle}
	d := RunnerJSON(sn, gen.Account{ID: "k1", Platform: "kick", AvatarUrl: "https://files.kick.com/a.png"}, false)
	assert.Equal(t, "/api/img/kick?u=https%3A%2F%2Ffiles.kick.com%2Fa.png", d.AvatarURL)
	d = RunnerJSON(runner.Snapshot{AccountID: "t1", Platform: "twitch"}, gen.Account{ID: "t1", Platform: "twitch", AvatarUrl: "https://static-cdn.jtvnw.net/a.png"}, false)
	assert.Equal(t, "https://static-cdn.jtvnw.net/a.png", d.AvatarURL)
}

func TestRunners_OrderUnknownIDsIgnored(t *testing.T) {
	e := newResEnv(t, nil)
	e.pool.snaps = []runner.Snapshot{{
		AccountID: "a1", Platform: "twitch", State: runner.StateWatching,
		UpNext: []plan.Target{tgt("G", "a", 0), tgt("G", "b2", 0)},
	}}
	_, tok := e.session()
	resp, _ := e.do("PUT", "/api/runners/a1/order", tok, map[string]any{"benefit_ids": []string{"ghost", "b2"}})
	require.Equal(t, 204, resp.StatusCode)
	assert.Equal(t, []string{"ghost", "b2"}, e.pool.orders["a1"])
	up := e.runners()[0]["up_next"].([]any)
	require.Len(t, up, 2)
	assert.Equal(t, "b2", up[0].(map[string]any)["benefit_id"])
	assert.Equal(t, "a", up[1].(map[string]any)["benefit_id"])

	resp, b := e.do("PUT", "/api/runners/nope/order", tok, map[string]any{"benefit_ids": []string{"x"}})
	assert.Equal(t, 404, resp.StatusCode)
	assert.JSONEq(t, `{"error":"not_found"}`, string(b))
	resp, _ = e.do("PUT", "/api/runners/a1/order", tok, map[string]any{"bogus": 1})
	assert.Equal(t, 400, resp.StatusCode)
}

func TestRunners_ResetOrder(t *testing.T) {
	e := newResEnv(t, nil)
	_, tok := e.session()
	resp, _ := e.do("DELETE", "/api/runners/a1/order", tok, nil)
	assert.Equal(t, 204, resp.StatusCode)
	assert.Equal(t, []string{"a1"}, e.pool.resets)
	resp, _ = e.do("DELETE", "/api/runners/nope/order", tok, nil)
	assert.Equal(t, 404, resp.StatusCode)
}

func TestRunners_MineNow(t *testing.T) {
	e := newResEnv(t, nil)
	cur := tgt("G", "cur", 10)
	e.pool.snaps = []runner.Snapshot{{
		AccountID: "a1", Platform: "twitch", State: runner.StateWatching, Current: &cur,
		UpNext: []plan.Target{tgt("G", "n1", 0), tgt("G", "n2", 0)},
	}}
	_, tok := e.session()
	// No body: the server starts the first Up next target.
	resp, b := e.do("POST", "/api/runners/a1/mine-now", tok, nil)
	assert.Equal(t, 202, resp.StatusCode)
	assert.JSONEq(t, `{"benefit_id":"n1"}`, string(b))
	assert.Equal(t, []string{"a1/n1"}, e.pool.mineNow)

	resp, _ = e.do("POST", "/api/runners/a1/mine-now", "", nil)
	assert.Equal(t, 403, resp.StatusCode)

	resp, b = e.do("POST", "/api/runners/b2/mine-now", tok, nil)
	assert.Equal(t, 409, resp.StatusCode)
	assert.JSONEq(t, `{"error":"not_running"}`, string(b))

	resp, _ = e.do("POST", "/api/runners/nope/mine-now", tok, nil)
	assert.Equal(t, 404, resp.StatusCode)

	e.pool.mu.Lock()
	e.pool.snaps[0].UpNext = nil
	e.pool.mu.Unlock()
	resp, b = e.do("POST", "/api/runners/a1/mine-now", tok, nil)
	assert.Equal(t, 404, resp.StatusCode)
	assert.JSONEq(t, `{"error":"not_queued"}`, string(b))
	assert.Equal(t, []string{"a1/n1"}, e.pool.mineNow)
}

// Mine next acts on the account in the URL only, so two accounts with queues
// each start their own first Up next drop.
func TestRunners_MineNextPerAccount(t *testing.T) {
	e := newResEnv(t, nil)
	e.pool.snaps = []runner.Snapshot{
		{AccountID: "a1", Platform: "twitch", State: runner.StateWatching, UpNext: []plan.Target{tgt("G", "n1", 0)}},
		{AccountID: "b2", Platform: "kick", State: runner.StateWatching, UpNext: []plan.Target{tgt("H", "k1", 0)}},
	}
	_, tok := e.session()
	resp, b := e.do("POST", "/api/runners/b2/mine-now", tok, nil)
	assert.Equal(t, 202, resp.StatusCode)
	assert.JSONEq(t, `{"benefit_id":"k1"}`, string(b))
	assert.Equal(t, []string{"b2/k1"}, e.pool.mineNow)
	resp, _ = e.do("POST", "/api/runners/a1/mine-now", tok, nil)
	assert.Equal(t, 202, resp.StatusCode)
	assert.Equal(t, []string{"b2/k1", "a1/n1"}, e.pool.mineNow)
}

func TestAccounts_RecheckLinks(t *testing.T) {
	e := newResEnv(t, nil)
	e.s.routesAccounts(e.s.protected)
	e.pool.snaps = []runner.Snapshot{{AccountID: "b2", Platform: "kick", State: runner.StatePicking}}
	e.pool.recheckRes = runner.LinkRecheck{Claimed: 2, StillNeedsLink: 1}
	_, tok := e.session()

	resp, b := e.do("POST", "/api/accounts/b2/recheck-links", tok, nil)
	assert.Equal(t, 200, resp.StatusCode)
	assert.JSONEq(t, `{"claimed":2,"still_needs_link":1}`, string(b))
	assert.Equal(t, []string{"b2"}, e.pool.recheck)

	resp, _ = e.do("POST", "/api/accounts/b2/recheck-links", "", nil)
	assert.Equal(t, 403, resp.StatusCode)

	resp, b = e.do("POST", "/api/accounts/a1/recheck-links", tok, nil)
	assert.Equal(t, 409, resp.StatusCode)
	assert.JSONEq(t, `{"error":"not_running"}`, string(b))

	resp, _ = e.do("POST", "/api/accounts/nope/recheck-links", tok, nil)
	assert.Equal(t, 404, resp.StatusCode)

	e.pool.recheckErr = errors.New("boom")
	resp, b = e.do("POST", "/api/accounts/b2/recheck-links", tok, nil)
	assert.Equal(t, 502, resp.StatusCode)
	assert.JSONEq(t, `{"error":"recheck_failed"}`, string(b))
}

func TestTargetJSON_Channels(t *testing.T) {
	c := platform.Campaign{AllowedChannels: []string{"oilrats", "a", "b", "c", "d"}, AllowedChannelCount: 5}
	got := TargetJSON(plan.Target{Campaign: c})
	assert.Equal(t, []string{"oilrats", "a", "b"}, got.Channels, "capped at three names")
	assert.Equal(t, 5, got.ChannelCount)

	// Kick sends the list without a count.
	got = TargetJSON(plan.Target{Campaign: platform.Campaign{AllowedChannels: []string{"oilrats"}}})
	assert.Equal(t, []string{"oilrats"}, got.Channels)
	assert.Equal(t, 1, got.ChannelCount)

	got = TargetJSON(plan.Target{})
	assert.Equal(t, []string{}, got.Channels, "any channel: empty list, not null")
	assert.Equal(t, 0, got.ChannelCount)
}
