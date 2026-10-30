package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/store"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
	"github.com/aalejandrofer/grubdrops/internal/web"
)

type balanceTestBackend struct{ platform.Backend }

func (balanceTestBackend) Name() string { return "twitch" }

func (balanceTestBackend) ChannelPointsBalance(context.Context, platform.Session, string) (int, error) {
	return 0, nil
}

type pointsEnv struct {
	t   *testing.T
	ctx context.Context
	q   *gen.Queries
	sm  *scs.SessionManager
	acc accountsDeps
	pts *pointsDeps
	rld []string
}

func newPointsEnv(t *testing.T) *pointsEnv {
	t.Helper()
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	for _, a := range [][2]string{{"acc-tw", "twitch"}, {"acc-kk", "kick"}} {
		_, err := db.ExecContext(ctx,
			`INSERT INTO accounts (id, platform, display_name, status, fingerprint_json, enabled, created_at, updated_at)
			VALUES (?, ?, ?, 'idle', '{}', 1, 1, 1)`, a[0], a[1], "name-"+a[1])
		require.NoError(t, err)
	}
	tmpl, err := web.Templates()
	require.NoError(t, err)
	e := &pointsEnv{t: t, ctx: ctx, q: gen.New(db), sm: scs.New()}
	e.acc = accountsDeps{q: e.q, db: db, sm: e.sm, rootCtx: ctx,
		reloadAccount: func(_ context.Context, id string) { e.rld = append(e.rld, id) }}
	e.pts = &pointsDeps{q: e.q, t: tmpl, sm: e.sm}
	return e
}

func (e *pointsEnv) post(id, path string, form url.Values, h func(http.ResponseWriter, *http.Request)) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req, _ = loadSession(e.t, e.sm, req)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	h(rec, req)
	return rec
}

func (e *pointsEnv) page(tab string) string {
	req, _ := loadSession(e.t, e.sm, httptest.NewRequest("GET", "/points?tab="+tab, nil))
	rec := httptest.NewRecorder()
	e.pts.get(rec, req)
	require.Equal(e.t, http.StatusOK, rec.Code)
	return rec.Body.String()
}

// Each platform has its own tab listing only its accounts, and the edit
// endpoints send the user back to the tab of the account they edited.
func TestPointsTabs_SplitByPlatformAndRedirectBack(t *testing.T) {
	e := newPointsEnv(t)
	for _, c := range []struct {
		id, tab, channel string
	}{
		{"acc-tw", "twitch", "XQC"}, {"acc-tw", "twitch", "shroud"}, {"acc-kk", "kick", "trainwreckstv"},
	} {
		rec := e.post(c.id, "/accounts/"+c.id+"/force-channels/add", url.Values{"channel": {c.channel}}, e.acc.addForceChannel)
		require.Equal(t, http.StatusSeeOther, rec.Code)
		assert.Equal(t, "/points?tab="+c.tab, rec.Header().Get("Location"))
	}
	rec := e.post("acc-tw", "/accounts/acc-tw/force-watch", url.Values{"enabled": {"1"}}, e.acc.forceWatchToggle)
	assert.Equal(t, "/points?tab=twitch", rec.Header().Get("Location"))
	assert.Equal(t, []string{"acc-tw", "acc-tw", "acc-kk", "acc-tw"}, e.rld, "only the edited account restarts")

	e.pts.points = &channelPointsCache{entries: map[string]channelPointsEntry{
		"acc-tw\x00xqc":           {Balance: 1234, FetchedAt: time.Now()},
		"acc-tw\x00shroud":        {Balance: 0, FetchedAt: time.Now()},
		"acc-kk\x00trainwreckstv": {Balance: 777, FetchedAt: time.Now()},
	}}
	e.pts.registry = platform.NewRegistry()
	e.pts.registry.Register(balanceTestBackend{})

	tw := e.page("twitch")
	assert.Contains(t, tw, "name-twitch")
	assert.NotContains(t, tw, "name-kick")
	assert.Contains(t, tw, `data-channel="xqc"`)
	assert.Contains(t, tw, `data-channel="shroud"`)
	assert.Less(t, strings.Index(tw, `data-channel="xqc"`), strings.Index(tw, `data-channel="shroud"`),
		"channels render in saved priority order")
	xqcStart := strings.Index(tw, `data-channel="xqc"`)
	require.NotEqual(t, -1, xqcStart)
	xqcTail := tw[xqcStart:]
	xqcEnd := strings.Index(xqcTail, "</li>")
	require.NotEqual(t, -1, xqcEnd)
	assert.Contains(t, xqcTail[:xqcEnd], "1234 pts")

	shroudStart := strings.Index(tw, `data-channel="shroud"`)
	require.NotEqual(t, -1, shroudStart)
	shroudTail := tw[shroudStart:]
	shroudEnd := strings.Index(shroudTail, "</li>")
	require.NotEqual(t, -1, shroudEnd)
	assert.Contains(t, shroudTail[:shroudEnd], "0 pts", "zero balance is still displayed")
	assert.Contains(t, tw, `action="/accounts/acc-tw/force-watch"`)

	kk := e.page("kick")
	assert.Contains(t, kk, "name-kick")
	assert.NotContains(t, kk, "name-twitch")
	assert.Contains(t, kk, `data-channel="trainwreckstv"`)

	assert.NotContains(t, kk, "777 pts", "Kick balances are not shown in the Twitch-only counter")
	assert.Contains(t, e.page("nonsense"), "name-twitch", "unknown tab falls back to Twitch")
	assert.NotContains(t, tw, `name="sleep_start"`, "safety settings live only on their own tab")
}

// One safety setting set serves every account: it defaults to a 02:00-08:00
// sleep window with raids on, keeps the old times when new ones are invalid,
// and applies to Twitch and Kick alike.
func TestPointsSafety_SharedAcrossPlatforms(t *testing.T) {
	e := newPointsEnv(t)
	assert.Equal(t, SleepWindow{Enabled: true, Start: 120, End: 480}, PointsSleepWindow(e.ctx, e.q), "safe default")
	assert.True(t, PointsFollowRaids(e.ctx, e.q), "raids default on")

	save := func(form url.Values) {
		rec := e.post("", "/points/safety", form, e.pts.saveSafety)
		require.Equal(t, http.StatusSeeOther, rec.Code)
		assert.Equal(t, "/points?tab=safety", rec.Header().Get("Location"))
	}

	save(url.Values{"sleep": {"1"}, "sleep_start": {"23:30"}, "sleep_end": {"07:15"}}) // raids box unticked
	w := PointsSleepWindow(e.ctx, e.q)
	assert.Equal(t, SleepWindow{Enabled: true, Start: 23*60 + 30, End: 7*60 + 15}, w)
	assert.False(t, PointsFollowRaids(e.ctx, e.q), "unticked box turns raids off")

	save(url.Values{"sleep": {"1"}, "sleep_start": {"junk"}, "sleep_end": {"07:15"}, "follow_raids": {"1"}})
	assert.Equal(t, w, PointsSleepWindow(e.ctx, e.q), "bad time keeps the old window")
	assert.True(t, PointsFollowRaids(e.ctx, e.q))

	// The same window is what both platforms' accounts sleep by.
	night := time.Date(2026, 10, 10, 23, 50, 0, 0, time.UTC)
	for _, id := range []string{"acc-tw", "acc-kk"} {
		_, asleep := PointsSleepWindow(e.ctx, e.q).Until(night, id)
		assert.True(t, asleep, id)
	}

	save(url.Values{"sleep_start": {"01:00"}, "sleep_end": {"05:00"}, "follow_raids": {"1"}}) // sleep box unticked
	off := PointsSleepWindow(e.ctx, e.q)
	assert.False(t, off.Enabled, "unticked box turns the sleep window off")
	_, asleep := off.Until(night, "acc-tw")
	assert.False(t, asleep)

	save(url.Values{"sleep": {"1"}, "sleep_start": {"03:00"}, "sleep_end": {"09:00"}, "follow_raids": {"1"}})
	assert.Contains(t, e.page("safety"), `name="sleep_start" value="03:00"`)
}

// The per-account mode switch only restarts the account when it changes.
func TestPointsMode_RestartsOnlyOnChange(t *testing.T) {
	e := newPointsEnv(t)
	assert.False(t, PointsOnly(e.ctx, e.q, "acc-kk"), "drops + points by default")

	rec := e.post("acc-kk", "/accounts/acc-kk/points-mode", url.Values{"mode": {"points"}}, e.acc.pointsMode)
	assert.Equal(t, "/points?tab=kick", rec.Header().Get("Location"))
	assert.True(t, PointsOnly(e.ctx, e.q, "acc-kk"))
	e.post("acc-kk", "/accounts/acc-kk/points-mode", url.Values{"mode": {"points"}}, e.acc.pointsMode)
	e.post("acc-kk", "/accounts/acc-kk/points-mode", url.Values{"mode": {"drops"}}, e.acc.pointsMode)
	assert.False(t, PointsOnly(e.ctx, e.q, "acc-kk"))
	assert.Equal(t, []string{"acc-kk", "acc-kk"}, e.rld)
}

// Twitch accounts choose any positive stream count. Changes reload only that
// account, and Kick never stores a stream setting.
func TestPointsStreams_AnyPositiveCount(t *testing.T) {
	e := newPointsEnv(t)
	assert.Equal(t, 2, PointsStreams(e.ctx, e.q, "acc-tw"), "accounts default to two slots")

	post := func(id string, form url.Values) *httptest.ResponseRecorder {
		rec := e.post(id, "/accounts/"+id+"/points-mode", form, e.acc.pointsMode)
		require.Equal(t, http.StatusSeeOther, rec.Code)
		return rec
	}
	post("acc-tw", url.Values{"mode": {"drops"}, "streams": {"5"}})
	assert.Equal(t, 5, PointsStreams(e.ctx, e.q, "acc-tw"), "Twitch accounts may exceed two")
	post("acc-tw", url.Values{"mode": {"drops"}, "streams": {"5"}}) // unchanged
	post("acc-tw", url.Values{"mode": {"drops"}, "streams": {"7"}})
	assert.Equal(t, 7, PointsStreams(e.ctx, e.q, "acc-tw"))

	_, err := e.q.CreateAccount(e.ctx, gen.CreateAccountParams{
		ID: "acc-tw2", Platform: "twitch", DisplayName: "secondary",
		Status: "idle", FingerprintJson: "{}", Enabled: 1, CreatedAt: 2, UpdatedAt: 2,
	})
	require.NoError(t, err)
	assert.Equal(t, 2, PointsStreams(e.ctx, e.q, "acc-tw2"), "new accounts default to two")
	post("acc-tw2", url.Values{"mode": {"drops"}, "streams": {"5"}})
	assert.Equal(t, 5, PointsStreams(e.ctx, e.q, "acc-tw2"))

	post("acc-tw", url.Values{"mode": {"points"}, "streams": {"0"}})
	assert.False(t, PointsOnly(e.ctx, e.q, "acc-tw"), "invalid stream counts do not partially save mode")
	assert.Equal(t, 7, PointsStreams(e.ctx, e.q, "acc-tw"))

	post("acc-kk", url.Values{"mode": {"drops"}, "streams": {"2"}})
	_, err = e.q.GetSettingString(e.ctx, PointsStreamsKey("acc-kk"))
	assert.Error(t, err, "Kick has no streams setting")

	assert.Equal(t, []string{"acc-tw", "acc-tw", "acc-tw2"}, e.rld, "only changed accounts restart")
	tw := e.page("twitch")
	assert.Contains(t, tw, `type="number" name="streams" min="1" step="1" value="7" required`)
	assert.Contains(t, tw, `type="number" name="streams" min="1" step="1" value="5" required`)
	assert.NotContains(t, tw, `type="radio" name="streams"`)
	assert.NotContains(t, e.page("kick"), `name="streams"`)
}
