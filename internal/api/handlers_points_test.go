package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/store"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
	"github.com/aalejandrofer/grubdrops/internal/web"
)

// The Points page lists every Twitch and Kick account with its saved channels
// and toggle, and the force-watch endpoints it posts to send the user back to
// it and restart only the edited account.
func TestPointsPage_ListsAccountsAndEditsRedirectBack(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	q := gen.New(db)
	for _, a := range [][2]string{{"acc-tw", "twitch"}, {"acc-kk", "kick"}} {
		_, err := db.ExecContext(ctx,
			`INSERT INTO accounts (id, platform, display_name, status, fingerprint_json, enabled, created_at, updated_at)
			VALUES (?, ?, ?, 'idle', '{}', 1, 1, 1)`, a[0], a[1], "name-"+a[1])
		require.NoError(t, err)
	}

	sm := scs.New()
	var reloaded []string
	acc := accountsDeps{q: q, db: db, sm: sm, rootCtx: ctx,
		reloadAccount: func(_ context.Context, id string) { reloaded = append(reloaded, id) }}

	post := func(id, path string, form url.Values, h func(http.ResponseWriter, *http.Request)) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req, _ = loadSession(t, sm, req)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", id)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		rec := httptest.NewRecorder()
		h(rec, req)
		return rec
	}

	for _, rec := range []*httptest.ResponseRecorder{
		post("acc-tw", "/accounts/acc-tw/force-channels/add", url.Values{"channel": {"XQC"}}, acc.addForceChannel),
		post("acc-tw", "/accounts/acc-tw/force-channels/add", url.Values{"channel": {"shroud"}}, acc.addForceChannel),
		post("acc-kk", "/accounts/acc-kk/force-channels/add", url.Values{"channel": {"trainwreckstv"}}, acc.addForceChannel),
		post("acc-tw", "/accounts/acc-tw/force-watch", url.Values{"enabled": {"1"}}, acc.forceWatchToggle),
	} {
		require.Equal(t, http.StatusSeeOther, rec.Code)
		assert.Equal(t, "/points", rec.Header().Get("Location"))
	}
	assert.Equal(t, []string{"acc-tw", "acc-tw", "acc-kk", "acc-tw"}, reloaded, "only the edited account restarts")

	tmpl, err := web.Templates()
	require.NoError(t, err)
	pts := &pointsDeps{q: q, t: tmpl, sm: sm}
	req, _ := loadSession(t, sm, httptest.NewRequest("GET", "/points", nil))
	rec := httptest.NewRecorder()
	pts.get(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	body := rec.Body.String()
	for _, want := range []string{
		"name-twitch", "name-kick",
		`data-channel="xqc"`, `data-channel="shroud"`, `data-channel="trainwreckstv"`,
		`action="/accounts/acc-tw/force-watch"`, `action="/accounts/acc-kk/force-channels/add"`,
	} {
		assert.Contains(t, body, want)
	}
	assert.Less(t, strings.Index(body, `data-channel="xqc"`), strings.Index(body, `data-channel="shroud"`),
		"channels render in saved priority order")
}

// Watch time defaults to a safe 75%, is clamped to the 50-100 range, and raids
// are on until switched off. Kick has no raid setting, so a save must not
// write one for it.
func TestPointsSettings_DefaultsClampAndPerPlatform(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	q := gen.New(db)
	for _, a := range [][2]string{{"acc-tw", "twitch"}, {"acc-kk", "kick"}} {
		_, err := db.ExecContext(ctx,
			`INSERT INTO accounts (id, platform, display_name, status, fingerprint_json, enabled, created_at, updated_at)
			VALUES (?, ?, ?, 'idle', '{}', 1, 1, 1)`, a[0], a[1], "name-"+a[1])
		require.NoError(t, err)
	}

	assert.Equal(t, 75, PointsWatchPercent(ctx, q, "acc-tw"), "safe default")
	assert.True(t, PointsFollowRaids(ctx, q, "acc-tw"), "raids default on")

	sm := scs.New()
	acc := accountsDeps{q: q, db: db, sm: sm, rootCtx: ctx}
	save := func(id string, form url.Values) {
		req := httptest.NewRequest("POST", "/accounts/"+id+"/points-settings", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req, _ = loadSession(t, sm, req)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", id)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		rec := httptest.NewRecorder()
		acc.pointsSettings(rec, req)
		require.Equal(t, http.StatusSeeOther, rec.Code)
		assert.Equal(t, "/points", rec.Header().Get("Location"))
	}

	save("acc-tw", url.Values{"watch_percent": {"80"}}) // raids box unticked
	assert.Equal(t, 80, PointsWatchPercent(ctx, q, "acc-tw"))
	assert.False(t, PointsFollowRaids(ctx, q, "acc-tw"), "unticked box turns raids off")

	save("acc-tw", url.Values{"watch_percent": {"10"}, "follow_raids": {"1"}})
	assert.Equal(t, MinWatchPercent, PointsWatchPercent(ctx, q, "acc-tw"), "clamped up to the floor")
	assert.True(t, PointsFollowRaids(ctx, q, "acc-tw"))

	save("acc-tw", url.Values{"watch_percent": {"500"}, "follow_raids": {"1"}})
	assert.Equal(t, 100, PointsWatchPercent(ctx, q, "acc-tw"), "clamped down to 100")

	save("acc-kk", url.Values{"watch_percent": {"60"}})
	assert.Equal(t, 60, PointsWatchPercent(ctx, q, "acc-kk"))
	_, err = q.GetSettingString(ctx, PointsFollowRaidsKey("acc-kk"))
	assert.Error(t, err, "no raid setting is stored for Kick")

	tmpl, err := web.Templates()
	require.NoError(t, err)
	req, _ := loadSession(t, sm, httptest.NewRequest("GET", "/points", nil))
	rec := httptest.NewRecorder()
	(&pointsDeps{q: q, t: tmpl, sm: sm}).get(rec, req)
	body := rec.Body.String()
	assert.Equal(t, 1, strings.Count(body, `name="follow_raids"`), "raid checkbox only on the Twitch card")
	assert.Contains(t, body, `name="watch_percent"`)
	assert.Contains(t, body, `value="60"`)
}
