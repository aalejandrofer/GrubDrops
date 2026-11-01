package api

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/store"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

// newPointsEnv is a resEnv with GET /api/points, a clock at now and a third
// account c3 (twitch, Cara) in points mode drops_points.
func newPointsEnv(t *testing.T, now time.Time, mut func(*Deps)) (*resEnv, *store.PointsBalances) {
	t.Helper()
	var pb *store.PointsBalances
	e := newResEnv(t, func(d *Deps) {
		pb = store.NewPointsBalances(d.Q)
		d.Points = pb
		d.Now = func() time.Time { return now }
		if mut != nil {
			mut(d)
		}
	})
	e.s.routesPoints(e.s.protected)
	ctx := context.Background()
	_, err := e.q.CreateAccount(ctx, gen.CreateAccountParams{
		ID: "c3", Platform: "twitch", DisplayName: "Cara", Status: "active",
		FingerprintJson: "{}", Enabled: 1, CreatedAt: 102, UpdatedAt: 102,
	})
	require.NoError(t, err)
	_, err = e.db.ExecContext(ctx, `UPDATE accounts SET points_mode = 'drops_points' WHERE id = 'c3'`)
	require.NoError(t, err)
	return e, pb
}

func TestGetPoints_Shape(t *testing.T) {
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	e, pb := newPointsEnv(t, now, func(d *Deps) {
		d.ForceLive = stubLive(map[string]*bool{"twitch/keshaeuw": boolPtr(true)})
	})
	ctx := context.Background()
	e.setForce([]store.ForceChannel{
		{Platform: "twitch", Channel: "keshaeuw", Accounts: []string{"a1", "c3"}},
		{Platform: "kick", Channel: "xqc", Accounts: []string{"b2"}},
		{Platform: "twitch", Channel: "nopoints", Accounts: []string{"c3"}},
		{Platform: "twitch", Channel: "flaky", Accounts: []string{"c3"}},
		{Platform: "twitch", Channel: "old", Accounts: []string{"c3"}},
		{Platform: "twitch", Channel: "fresh", Accounts: []string{"c3"}},
	})
	// a1 has points off: its old balance shows, marked off.
	require.NoError(t, pb.RecordOK(ctx, "a1", "keshaeuw", 300, now.Add(-30*time.Minute)))
	require.NoError(t, pb.RecordOK(ctx, "c3", "keshaeuw", 1200, now.Add(-10*time.Minute)))
	require.NoError(t, pb.RecordErr(ctx, "c3", "nopoints", store.PointsErrUnavailable, 0, now.Add(-5*time.Minute)))
	require.NoError(t, pb.RecordOK(ctx, "c3", "flaky", 40, now.Add(-90*time.Minute)))
	require.NoError(t, pb.RecordErr(ctx, "c3", "flaky", store.PointsErrFailed, 1, now.Add(-time.Minute)))
	require.NoError(t, pb.RecordOK(ctx, "c3", "old", 5, now.Add(-3*time.Hour)))

	resp, b := e.do("GET", "/api/points", "", nil)
	require.Equal(t, 200, resp.StatusCode, string(b))
	assert.JSONEq(t, `{
		"refresh_every_sec": 3600,
		"accounts": [
			{"id":"a1","name":"Alice","platform":"twitch","enabled":true,"points_mode":"off","balances_available":true,"total":300},
			{"id":"b2","name":"Bob","platform":"kick","enabled":true,"points_mode":"off","balances_available":false,"total":null},
			{"id":"c3","name":"Cara","platform":"twitch","enabled":true,"points_mode":"drops_points","balances_available":true,"total":1245}
		],
		"channels": [
			{"platform":"twitch","channel":"keshaeuw","live":true,"balances":[
				{"account_id":"a1","status":"off","balance":300,"fetched_at":"2026-10-30T11:30:00Z","checked_at":"2026-10-30T11:30:00Z","stale":false},
				{"account_id":"c3","status":"ok","balance":1200,"fetched_at":"2026-10-30T11:50:00Z","checked_at":"2026-10-30T11:50:00Z","stale":false}
			]},
			{"platform":"kick","channel":"xqc","live":null,"balances":[
				{"account_id":"b2","status":"unavailable","balance":null,"fetched_at":null,"checked_at":null,"stale":false}
			]},
			{"platform":"twitch","channel":"nopoints","live":null,"balances":[
				{"account_id":"c3","status":"unavailable","balance":null,"fetched_at":null,"checked_at":"2026-10-30T11:55:00Z","stale":false}
			]},
			{"platform":"twitch","channel":"flaky","live":null,"balances":[
				{"account_id":"c3","status":"error","balance":40,"fetched_at":"2026-10-30T10:30:00Z","checked_at":"2026-10-30T11:59:00Z","stale":false}
			]},
			{"platform":"twitch","channel":"old","live":null,"balances":[
				{"account_id":"c3","status":"ok","balance":5,"fetched_at":"2026-10-30T09:00:00Z","checked_at":"2026-10-30T09:00:00Z","stale":true}
			]},
			{"platform":"twitch","channel":"fresh","live":null,"balances":[
				{"account_id":"c3","status":"pending","balance":null,"fetched_at":null,"checked_at":null,"stale":false}
			]}
		]
	}`, string(b))
}

func TestGetPoints_PausedAccountIsOff(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	e, _ := newPointsEnv(t, now, func(d *Deps) { d.PointsEvery = 30 * time.Minute })
	_, err := e.db.Exec(`UPDATE accounts SET enabled = 0 WHERE id = 'c3'`)
	require.NoError(t, err)
	e.setForce([]store.ForceChannel{{Platform: "twitch", Channel: "keshaeuw", Accounts: []string{"c3"}}})
	var got pointsJSON
	e.getJSON("/api/points", &got)
	assert.Equal(t, 1800, got.RefreshEverySec)
	require.Len(t, got.Channels, 1)
	require.Len(t, got.Channels[0].Balances, 1)
	assert.Equal(t, "off", got.Channels[0].Balances[0].Status)
	assert.False(t, got.Accounts[2].Enabled)
}

func TestGetPoints_EmptyAndUnavailable(t *testing.T) {
	e, _ := newPointsEnv(t, time.Unix(1_000_000, 0), nil)
	var got pointsJSON
	e.getJSON("/api/points", &got)
	assert.Empty(t, got.Channels)
	assert.NotNil(t, got.Channels, "channels is [] not null")
	assert.Len(t, got.Accounts, 3)

	e2, _ := newPointsEnv(t, time.Unix(1_000_000, 0), func(d *Deps) { d.Points = nil })
	resp, _ := e2.do("GET", "/api/points", "", nil)
	assert.Equal(t, 503, resp.StatusCode)
}

func TestPostPointsRefresh(t *testing.T) {
	calls := 0
	ok, wait := true, time.Duration(0)
	e, _ := newPointsEnv(t, time.Now(), func(d *Deps) {
		d.PointsRefresh = func() (bool, time.Duration) { calls++; return ok, wait }
	})
	_, tok := e.session()

	resp, b := e.do("POST", "/api/points/refresh", "", nil)
	assert.Equal(t, 403, resp.StatusCode, "CSRF token required")
	assert.JSONEq(t, `{"error":"csrf"}`, string(b))
	assert.Zero(t, calls, "refused before the handler")

	resp, b = e.do("POST", "/api/points/refresh", tok, nil)
	assert.Equal(t, 202, resp.StatusCode, string(b))
	assert.Equal(t, 1, calls)

	ok, wait = false, 41500*time.Millisecond
	resp, b = e.do("POST", "/api/points/refresh", tok, nil)
	assert.Equal(t, 429, resp.StatusCode)
	assert.JSONEq(t, `{"error":"cooldown","retry_after_sec":42}`, string(b))
	assert.Equal(t, "42", resp.Header.Get("Retry-After"))
}

func TestPostPointsRefresh_Off(t *testing.T) {
	for name, mut := range map[string]func(*Deps){
		"points off":   func(d *Deps) { d.Points = nil; d.PointsRefresh = func() (bool, time.Duration) { return true, 0 } },
		"no refresher": func(d *Deps) { d.PointsRefresh = nil },
	} {
		e, _ := newPointsEnv(t, time.Now(), mut)
		_, tok := e.session()
		resp, b := e.do("POST", "/api/points/refresh", tok, nil)
		assert.Equal(t, 503, resp.StatusCode, name)
		assert.JSONEq(t, `{"error":"unavailable"}`, string(b), name)
	}
}
