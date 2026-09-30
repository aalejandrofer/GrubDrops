package api

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/platform/twitch"
	"github.com/aalejandrofer/grubdrops/internal/store"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
	"github.com/aalejandrofer/grubdrops/internal/timeutil"
	"github.com/aalejandrofer/grubdrops/internal/web"
)

// tvDiscoveryTestKey is an age secret key used only in tests.
const tvDiscoveryTestKey = "AGE-SECRET-KEY-1DZCAXYWJM6M42NSX5GR4QWZZ2JXEYKJ9ZKWYFYSNU997775JJ6XSY85FK9"

func tvDiscoverySetup(t *testing.T) (context.Context, *gen.Queries, *store.SessionStore) {
	t.Helper()
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "tv.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	q := gen.New(db)
	c, err := store.NewCryptor(tvDiscoveryTestKey)
	require.NoError(t, err)
	sessions := store.NewSessionStore(db, q, c)
	return ctx, q, sessions
}

func createTwitchAccount(t *testing.T, ctx context.Context, q *gen.Queries, id string, enabled bool) {
	t.Helper()
	now := time.Now().Unix()
	en := int64(0)
	if enabled {
		en = 1
	}
	_, err := q.CreateAccount(ctx, gen.CreateAccountParams{
		ID: id, Platform: "twitch", DisplayName: id,
		Status: "idle", FingerprintJson: "{}", Enabled: en,
		CreatedAt: now, UpdatedAt: now,
	})
	require.NoError(t, err)
}

// No enabled Twitch account at all → no alert (Android-only or empty install).
func TestTVDiscoveryStatus_NoAccounts(t *testing.T) {
	ctx, q, sessions := tvDiscoverySetup(t)
	anyTV, empty := tvDiscoveryStatus(ctx, q, sessions)
	require.False(t, anyTV)
	require.False(t, empty)
}

// An Android-login account (ClientID "") must never trigger the banner.
func TestTVDiscoveryStatus_AndroidOnlyDoesNotTrigger(t *testing.T) {
	ctx, q, sessions := tvDiscoverySetup(t)
	createTwitchAccount(t, ctx, q, "acc_android", true)
	require.NoError(t, sessions.Put(ctx, "acc_android", platform.Session{ClientID: ""}))

	anyTV, empty := tvDiscoveryStatus(ctx, q, sessions)
	require.False(t, anyTV)
	require.False(t, empty)
}

// A TV-client session on an enabled Twitch account triggers the banner. With
// at least one whitelisted game (account-level), the whitelist is NOT empty.
func TestTVDiscoveryStatus_TVAccountWithWhitelist(t *testing.T) {
	ctx, q, sessions := tvDiscoverySetup(t)
	createTwitchAccount(t, ctx, q, "acc_tv", true)
	require.NoError(t, sessions.Put(ctx, "acc_tv", platform.Session{ClientID: twitch.ClientTV}))
	require.NoError(t, q.UpsertGame(ctx, gen.UpsertGameParams{ID: "g_test_tv", Name: "Test TV Game", Slug: "test-tv-game"}))
	require.NoError(t, q.AddAccountGame(ctx, gen.AddAccountGameParams{AccountID: "acc_tv", GameID: "g_test_tv", Rank: 0}))

	anyTV, empty := tvDiscoveryStatus(ctx, q, sessions)
	require.True(t, anyTV)
	require.False(t, empty)
}

// A TV-client session with NO account games and NO global games → empty
// effective whitelist, the warning-variant trigger.
func TestTVDiscoveryStatus_TVAccountEmptyWhitelist(t *testing.T) {
	ctx, q, sessions := tvDiscoverySetup(t)
	createTwitchAccount(t, ctx, q, "acc_tv", true)
	require.NoError(t, sessions.Put(ctx, "acc_tv", platform.Session{ClientID: twitch.ClientTV}))

	anyTV, empty := tvDiscoveryStatus(ctx, q, sessions)
	require.True(t, anyTV)
	require.True(t, empty)
}

// A TV account with no account-level games but a non-empty GLOBAL whitelist
// falls back to it (same resolution as gameNamesForAccount) — not empty.
func TestTVDiscoveryStatus_TVAccountFallsBackToGlobal(t *testing.T) {
	ctx, q, sessions := tvDiscoverySetup(t)
	createTwitchAccount(t, ctx, q, "acc_tv", true)
	require.NoError(t, sessions.Put(ctx, "acc_tv", platform.Session{ClientID: twitch.ClientTV}))
	require.NoError(t, q.UpsertGame(ctx, gen.UpsertGameParams{ID: "g_test_tv", Name: "Test TV Game", Slug: "test-tv-game"}))
	require.NoError(t, q.AddGlobalGame(ctx, gen.AddGlobalGameParams{GameID: "g_test_tv", Rank: 0}))

	anyTV, empty := tvDiscoveryStatus(ctx, q, sessions)
	require.True(t, anyTV)
	require.False(t, empty)
}

// A DISABLED Twitch account with a TV session must not trigger the banner —
// only ENABLED accounts count, matching buildDashAlerts' own rule.
func TestTVDiscoveryStatus_DisabledAccountIgnored(t *testing.T) {
	ctx, q, sessions := tvDiscoverySetup(t)
	createTwitchAccount(t, ctx, q, "acc_tv_off", false)
	require.NoError(t, sessions.Put(ctx, "acc_tv_off", platform.Session{ClientID: twitch.ClientTV}))

	anyTV, empty := tvDiscoveryStatus(ctx, q, sessions)
	require.False(t, anyTV)
	require.False(t, empty)
}

// tvDiscoveryAlert renders nil when there is nothing to warn about.
func TestTVDiscoveryAlert_NilWhenNoTVAccount(t *testing.T) {
	ctx, q, sessions := tvDiscoverySetup(t)
	require.Nil(t, tvDiscoveryAlert(ctx, q, sessions, "en"))
}

// tvDiscoveryAlert renders the info variant (Warning=false) when a TV
// account has a non-empty whitelist, and points at /priority.
func TestTVDiscoveryAlert_InfoVariant(t *testing.T) {
	ctx, q, sessions := tvDiscoverySetup(t)
	createTwitchAccount(t, ctx, q, "acc_tv", true)
	require.NoError(t, sessions.Put(ctx, "acc_tv", platform.Session{ClientID: twitch.ClientTV}))
	require.NoError(t, q.UpsertGame(ctx, gen.UpsertGameParams{ID: "g_test_tv", Name: "Test TV Game", Slug: "test-tv-game"}))
	require.NoError(t, q.AddAccountGame(ctx, gen.AddAccountGameParams{AccountID: "acc_tv", GameID: "g_test_tv", Rank: 0}))

	a := tvDiscoveryAlert(ctx, q, sessions, "en")
	require.NotNil(t, a)
	require.Equal(t, "tv_discovery", a.Kind)
	require.False(t, a.Warning)
	require.Equal(t, "/priority", a.URL)
	require.NotEmpty(t, a.Account)
	require.NotEmpty(t, a.Action)
}

// tvDiscoveryAlert renders the warning variant when the TV account's
// effective whitelist is empty.
func TestTVDiscoveryAlert_WarningVariant(t *testing.T) {
	ctx, q, sessions := tvDiscoverySetup(t)
	createTwitchAccount(t, ctx, q, "acc_tv", true)
	require.NoError(t, sessions.Put(ctx, "acc_tv", platform.Session{ClientID: twitch.ClientTV}))

	a := tvDiscoveryAlert(ctx, q, sessions, "en")
	require.NotNil(t, a)
	require.True(t, a.Warning)
}

// renderDashAlerts renders the shared dash_alerts partial (the same
// component both dashboard.html and drops.html use) with an ad-hoc page
// value carrying only the Alerts field, matching the field-by-name lookup
// html/template uses.
func renderDashAlerts(t *testing.T, alerts []dashAlert) string {
	t.Helper()
	tmpl, err := web.Templates()
	require.NoError(t, err)
	var buf bytes.Buffer
	page := struct{ Alerts []dashAlert }{Alerts: alerts}
	require.NoError(t, tmpl.ExecuteTemplate(&buf, "dash_alerts", page))
	return buf.String()
}

// The tv_discovery banner (info variant) renders the title, body, CTA link
// to /priority, and does NOT carry the warning class.
func TestDashAlerts_TVDiscoveryInfoVariant(t *testing.T) {
	out := renderDashAlerts(t, []dashAlert{{
		Kind:    "tv_discovery",
		Account: "Drop discovery is limited.",
		URL:     "/priority",
		Action:  "Whitelist games →",
		Warning: false,
	}})
	require.Contains(t, out, `href="/priority"`)
	require.Contains(t, out, "Drop discovery is limited.")
	require.Contains(t, out, "Twitch no longer lists campaigns for new logins")
	require.Contains(t, out, "Whitelist games")
	require.Contains(t, out, `class="alert alert-tv_discovery"`)
	require.NotContains(t, out, "warning")
}

// The warning variant (empty effective whitelist) adds the warning class to
// the same markup/copy.
func TestDashAlerts_TVDiscoveryWarningVariant(t *testing.T) {
	out := renderDashAlerts(t, []dashAlert{{
		Kind:    "tv_discovery",
		Account: "Drop discovery is limited.",
		URL:     "/priority",
		Action:  "Whitelist games →",
		Warning: true,
	}})
	require.Contains(t, out, `class="alert alert-tv_discovery warning"`)
}

// No alerts at all → the section doesn't render.
func TestDashAlerts_EmptyWhenNoAlerts(t *testing.T) {
	out := renderDashAlerts(t, nil)
	require.False(t, strings.Contains(out, `class="alerts"`))
}

// End-to-end: an enabled Twitch account with a TV session and no whitelist
// produces a dashboard page whose Alerts include the warning-variant
// tv_discovery entry (collectPage is the real dashboard view-model builder).
func TestDashboardCollectPage_TVDiscoveryBanner(t *testing.T) {
	ctx, q, sessions := tvDiscoverySetup(t)
	createTwitchAccount(t, ctx, q, "acc_tv", true)
	require.NoError(t, sessions.Put(ctx, "acc_tv", platform.Session{ClientID: twitch.ClientTV}))

	d := dashboardDeps{q: q, sessions: sessions, start: time.Now(), loc: timeutil.NewZone(time.UTC)}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	page := d.collectPage(req)

	require.Len(t, page.Alerts, 1)
	require.Equal(t, "tv_discovery", page.Alerts[0].Kind)
	require.True(t, page.Alerts[0].Warning)
}

// An Android-only install (no TV session anywhere) must not raise the
// banner on the dashboard.
func TestDashboardCollectPage_NoBannerForAndroidOnly(t *testing.T) {
	ctx, q, sessions := tvDiscoverySetup(t)
	createTwitchAccount(t, ctx, q, "acc_android", true)
	require.NoError(t, sessions.Put(ctx, "acc_android", platform.Session{ClientID: ""}))

	d := dashboardDeps{q: q, sessions: sessions, start: time.Now(), loc: timeutil.NewZone(time.UTC)}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	page := d.collectPage(req)

	for _, a := range page.Alerts {
		require.NotEqual(t, "tv_discovery", a.Kind)
	}
}

// The /drops page shares the same tvDiscoveryAlert construction: a TV
// account with a non-empty whitelist gets the info variant, not warning.
func TestDropsPage_TVDiscoveryBannerInfoVariant(t *testing.T) {
	ctx, q, sessions := tvDiscoverySetup(t)
	createTwitchAccount(t, ctx, q, "acc_tv", true)
	require.NoError(t, sessions.Put(ctx, "acc_tv", platform.Session{ClientID: twitch.ClientTV}))
	require.NoError(t, q.UpsertGame(ctx, gen.UpsertGameParams{ID: "g_test_tv2", Name: "Test TV Game 2", Slug: "test-tv-game-2"}))
	require.NoError(t, q.AddAccountGame(ctx, gen.AddAccountGameParams{AccountID: "acc_tv", GameID: "g_test_tv2", Rank: 0}))

	tv := tvDiscoveryAlert(ctx, q, sessions, "en")
	require.NotNil(t, tv)
	require.False(t, tv.Warning)
}
