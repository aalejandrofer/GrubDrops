package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/notify"
	"github.com/aalejandrofer/grubdrops/internal/store"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

type fakeNotify struct {
	err     error
	events  []string
	targets []string
}

func (f *fakeNotify) Notify(ctx context.Context, e string, _ map[string]any) error {
	f.events = append(f.events, e)
	f.targets = append(f.targets, notify.TargetFrom(ctx))
	return f.err
}

type settingsEnv struct {
	h       http.Handler
	st      *store.Settings
	n       *fakeNotify
	updates int
	zones   []string
	rescans int
	live    int
	s       *server
}

func newSettingsEnv(t *testing.T) *settingsEnv {
	t.Helper()
	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "t.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	e := &settingsEnv{st: store.NewSettings(gen.New(db)), n: &fakeNotify{}}
	s := &server{d: Deps{
		Settings: e.st, Notifier: e.n, Pool: gamesFakePool{rescans: &e.rescans, live: &e.live},
		OnSettingsUpdate: func() { e.updates++ },
		SetZone:          func(n string) { e.zones = append(e.zones, n) },
	}}
	e.s = s
	r := chi.NewRouter()
	s.routesSettings(r)
	e.h = r
	return e
}

func (e *settingsEnv) do(method, path string, body any) (int, []byte) {
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, httptest.NewRequest(method, path, rd))
	return w.Code, w.Body.Bytes()
}

func TestSettings_GetMasksWebhook(t *testing.T) {
	e := newSettingsEnv(t)
	require.NoError(t, e.st.SetGlobalDiscordWebhook(context.Background(), "https://discord.com/api/webhooks/123/abc"))
	code, b := e.do("GET", "/settings", nil)
	require.Equal(t, 200, code)
	var got map[string]any
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, map[string]any{"configured": true, "webhook_hint": "https://discord.com/api/webhooks/••••"}, got["discord"])
	assert.NotContains(t, string(b), "abc")
	assert.Equal(t, "en", got["language"])
	assert.Equal(t, "24h", got["time_format"])
	assert.Equal(t, "dark", got["theme"])
	assert.Equal(t, true, got["update_auto"])
	assert.Equal(t, "ordered", got["priority_mode"])
	assert.Equal(t, map[string]any{"claim": true, "auth": false, "errors": true, "daily_summary": false, "progress": false, "canary": false, "reward_codes": true}, got["notify"])
}

func TestSettings_PutPartial(t *testing.T) {
	e := newSettingsEnv(t)
	code, b := e.do("PUT", "/settings", map[string]any{"theme": "light"})
	require.Equal(t, 200, code)
	var got settingsBody
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, "light", got.Theme)
	assert.Equal(t, "en", got.Language)
	assert.Equal(t, "24h", got.TimeFormat)
	assert.True(t, got.UpdateAuto)
	assert.Equal(t, 1, e.updates)
	assert.Empty(t, e.zones)
}

func TestSettings_PutInvalidSavesNothing(t *testing.T) {
	e := newSettingsEnv(t)
	for _, body := range []map[string]any{
		{"theme": "light", "language": "fr"},
		{"theme": "light", "timezone": "Nowhere/Land"},
		{"theme": "light", "discord_webhook": "https://evil.example/hook"},
		{"theme": "light", "time_format": "13h"},
		{"theme": "light", "priority_mode": "random"},
	} {
		code, _ := e.do("PUT", "/settings", body)
		assert.Equal(t, 400, code)
		th, _ := e.st.UITheme(context.Background())
		assert.Equal(t, "dark", th)
	}
	assert.Equal(t, 0, e.updates)
}

// A priority change replans every runner now; other saves do not.
func TestSettings_PriorityModeRescans(t *testing.T) {
	e := newSettingsEnv(t)
	code, b := e.do("PUT", "/settings", map[string]any{"priority_mode": "ending_soonest"})
	require.Equal(t, 200, code)
	var got settingsBody
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, "ending_soonest", got.PriorityMode)
	assert.Equal(t, 1, e.rescans)

	code, _ = e.do("PUT", "/settings", map[string]any{"priority_mode": "ending_soonest", "theme": "light"})
	require.Equal(t, 200, code)
	assert.Equal(t, 1, e.rescans)
}

func TestSettings_NotifyKeepsProgress(t *testing.T) {
	e := newSettingsEnv(t)
	ctx := context.Background()
	require.NoError(t, e.st.SetNotifyKinds(ctx, true, true, false, true, true))
	code, _ := e.do("PUT", "/settings", map[string]any{"notify": map[string]any{"claim": false, "daily_summary": true}})
	require.Equal(t, 200, code)
	claim, progress, auth, errs, canary := e.st.NotifyKinds(ctx)
	assert.False(t, claim)
	assert.True(t, progress)
	assert.False(t, auth)
	assert.True(t, errs)
	assert.True(t, canary)
	sum, _ := e.st.NotifyDailySummary(ctx)
	assert.True(t, sum)
}

func TestSettings_RewardCodesToggle(t *testing.T) {
	e := newSettingsEnv(t)
	ctx := context.Background()
	on, _ := e.st.NotifyRewardCodes(ctx)
	assert.True(t, on, "on by default")
	code, _ := e.do("PUT", "/settings", map[string]any{"notify": map[string]any{"reward_codes": false}})
	require.Equal(t, 200, code)
	on, _ = e.st.NotifyRewardCodes(ctx)
	assert.False(t, on)
	sum, _ := e.st.NotifyDailySummary(ctx)
	assert.False(t, sum, "other toggles untouched")
}

func TestSettings_TimezoneCallsSetZone(t *testing.T) {
	e := newSettingsEnv(t)
	code, _ := e.do("PUT", "/settings", map[string]any{"timezone": "Asia/Shanghai"})
	require.Equal(t, 200, code)
	assert.Equal(t, []string{"Asia/Shanghai"}, e.zones)
	code, _ = e.do("PUT", "/settings", map[string]any{"timezone": "Asia/Shanghai"})
	require.Equal(t, 200, code)
	assert.Len(t, e.zones, 1, "unchanged zone is not re-applied")
	code, _ = e.do("PUT", "/settings", map[string]any{"timezone": ""})
	require.Equal(t, 200, code)
	assert.Equal(t, []string{"Asia/Shanghai", ""}, e.zones)
}

func TestSettings_PutAcceptsGetBody(t *testing.T) {
	e := newSettingsEnv(t)
	_, b := e.do("GET", "/settings", nil)
	code, _ := e.do("PUT", "/settings", json.RawMessage(b))
	assert.Equal(t, 200, code)
}

func TestNotifyTest_NoWebhook400(t *testing.T) {
	e := newSettingsEnv(t)
	code, _ := e.do("POST", "/settings/notify-test", nil)
	assert.Equal(t, 400, code)
	assert.Empty(t, e.n.events)
}

func TestNotifyTest_SendFails502(t *testing.T) {
	e := newSettingsEnv(t)
	require.NoError(t, e.st.SetGlobalDiscordWebhook(context.Background(), "https://discord.com/api/webhooks/1/x"))
	e.n.err = errors.New("boom")
	code, _ := e.do("POST", "/settings/notify-test", nil)
	assert.Equal(t, 502, code)
	e.n.err = nil
	code, _ = e.do("POST", "/settings/notify-test", nil)
	assert.Equal(t, 204, code)
	assert.Equal(t, []string{"test", "test"}, e.n.events)
}

func TestSettings_WebhookFallback(t *testing.T) {
	e := newSettingsEnv(t)
	e.h = func() http.Handler {
		s := &server{d: Deps{Settings: e.st, Notifier: e.n, WebhookFallback: "https://discord.com/api/webhooks/9/secret"}}
		r := chi.NewRouter()
		s.routesSettings(r)
		return r
	}()
	code, b := e.do("GET", "/settings", nil)
	require.Equal(t, 200, code)
	var got settingsBody
	require.NoError(t, json.Unmarshal(b, &got))
	assert.True(t, got.Discord.Configured)
	assert.Equal(t, "", got.Discord.WebhookHint)
	assert.NotContains(t, string(b), "secret")
	code, _ = e.do("POST", "/settings/notify-test", nil)
	assert.Equal(t, 204, code)
}

func TestSettings_ZoneEffective(t *testing.T) {
	e := newSettingsEnv(t)
	zone := func(h http.Handler) string {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "/settings", nil))
		var got map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
		return got["zone_effective"].(string)
	}
	build := func(z func() *time.Location) http.Handler {
		s := &server{d: Deps{Settings: e.st, Zone: z}}
		r := chi.NewRouter()
		s.routesSettings(r)
		return r
	}
	sh, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	assert.Equal(t, "Asia/Shanghai", zone(build(func() *time.Location { return sh })))
	// A zone named Local, as time.Local is with no TZ name, reads as UTC.
	// Injected, not set on time.Local: other tests' goroutines read it.
	assert.Equal(t, "UTC", zone(build(func() *time.Location { return time.FixedZone("Local", 0) })))
	// No Zone falls back to time.Local.
	want := time.Local.String()
	if want == "Local" {
		want = "UTC"
	}
	assert.Equal(t, want, zone(build(nil)))
}

func TestSettings_ScanAndPreempt(t *testing.T) {
	e := newSettingsEnv(t)
	_, b := e.do("GET", "/settings", nil)
	var got settingsBody
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, 60, got.ScanIntervalMin)
	assert.True(t, got.Preempt)

	code, b := e.do("PUT", "/settings", map[string]any{"scan_interval_min": 15, "preempt": false})
	require.Equal(t, 200, code)
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, 15, got.ScanIntervalMin)
	assert.False(t, got.Preempt)
	assert.Equal(t, 1, e.rescans, "a new scan pace re-arms the runners")

	code, _ = e.do("PUT", "/settings", map[string]any{"scan_interval_min": 15})
	require.Equal(t, 200, code)
	assert.Equal(t, 1, e.rescans)
	code, _ = e.do("PUT", "/settings", map[string]any{"scan_interval_min": 5})
	assert.Equal(t, 400, code)
}

// The GET hides the proxy password; sending the hint back keeps the saved URL.
func TestSettings_ProxyHint(t *testing.T) {
	e := newSettingsEnv(t)
	ctx := context.Background()
	code, b := e.do("PUT", "/settings", map[string]any{"proxy_enabled": true, "proxy_url": "http://bob:s3cret@10.0.0.1:3128"})
	require.Equal(t, 200, code)
	assert.NotContains(t, string(b), "s3cret")
	var got settingsBody
	require.NoError(t, json.Unmarshal(b, &got))
	assert.True(t, got.Proxy.Enabled)
	assert.Equal(t, "http://bob:%2A%2A%2A@10.0.0.1:3128", got.Proxy.URL)

	code, _ = e.do("PUT", "/settings", map[string]any{"proxy_url": got.Proxy.URL})
	require.Equal(t, 200, code)
	u, _ := e.st.ProxyURL(ctx)
	assert.Equal(t, "http://bob:s3cret@10.0.0.1:3128", u)

	// A new host on the masked URL keeps the saved password.
	code, _ = e.do("PUT", "/settings", map[string]any{"proxy_url": "http://bob:%2A%2A%2A@10.0.0.2:3128"})
	require.Equal(t, 200, code)
	u, _ = e.st.ProxyURL(ctx)
	assert.Equal(t, "http://bob:s3cret@10.0.0.2:3128", u)

	for _, bad := range []string{"ftp://x:1", "not a url", "http://"} {
		code, _ = e.do("PUT", "/settings", map[string]any{"proxy_url": bad})
		assert.Equal(t, 400, code, bad)
	}
	code, _ = e.do("PUT", "/settings", map[string]any{"proxy_url": ""})
	require.Equal(t, 200, code)
	u, _ = e.st.ProxyURL(ctx)
	assert.Empty(t, u)
}

func TestSettings_Advanced(t *testing.T) {
	e := newSettingsEnv(t)
	ctx := context.Background()
	code, b := e.do("PUT", "/settings", map[string]any{
		"kick_watch_mode":   "ws",
		"notify_avatar_url": "https://example.com/a.png",
		"notify":            map[string]any{"progress": true, "canary": true},
		"canary":            map[string]any{"twitch_channel": "foo_bar", "kick_channel": "baz", "interval_sec": 3600},
	})
	require.Equal(t, 200, code)
	var got settingsBody
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, "ws", got.KickWatchMode)
	assert.Equal(t, "https://example.com/a.png", got.NotifyAvatarURL)
	assert.True(t, got.Notify.Progress)
	assert.True(t, got.Notify.Canary)
	assert.True(t, got.Notify.Claim, "untouched kinds keep their value")
	assert.Equal(t, settingsCanary{TwitchChannel: "foo_bar", KickChannel: "baz", IntervalSec: 3600}, got.Canary)

	code, _ = e.do("PUT", "/settings", map[string]any{"canary": map[string]any{"interval_sec": 0}})
	require.Equal(t, 200, code)
	n, _ := e.st.CanaryIntervalSec(ctx)
	assert.Equal(t, 0, n)

	for _, bad := range []map[string]any{
		{"kick_watch_mode": "tv"},
		{"notify_avatar_url": "http://example.com/a.png"},
		{"canary": map[string]any{"twitch_channel": "has space"}},
		{"canary": map[string]any{"interval_sec": 10}},
	} {
		code, _ = e.do("PUT", "/settings", bad)
		assert.Equal(t, 400, code, bad)
	}
}

// connectProxy is a minimal HTTP CONNECT proxy for proxyTest.
func connectProxy(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect {
			http.Error(w, "connect only", http.StatusMethodNotAllowed)
			return
		}
		up, err := net.Dial("tcp", r.Host)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			_ = up.Close()
			return
		}
		go func() { _, _ = io.Copy(up, conn); _ = up.Close() }()
		_, _ = io.Copy(conn, up)
		_ = conn.Close()
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestProxyTest(t *testing.T) {
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	t.Cleanup(site.Close)
	old := proxyTestURL
	proxyTestURL = site.URL
	t.Cleanup(func() { proxyTestURL = old })
	proxy := connectProxy(t)

	e := newSettingsEnv(t)
	code, b := e.do("POST", "/settings/proxy-test", map[string]any{"url": proxy.URL})
	require.Equal(t, 200, code, string(b))
	var got struct{ Status int }
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, http.StatusTeapot, got.Status)

	// Nothing saved and no URL sent: nothing to test.
	code, _ = e.do("POST", "/settings/proxy-test", map[string]any{})
	assert.Equal(t, 400, code)

	// Saved URL with its hint sent back: the saved URL is tested.
	require.NoError(t, e.st.SetProxyURL(context.Background(), proxy.URL))
	code, _ = e.do("POST", "/settings/proxy-test", map[string]any{"url": proxy.URL})
	assert.Equal(t, 200, code)

	proxy.Close()
	code, b = e.do("POST", "/settings/proxy-test", map[string]any{})
	assert.Equal(t, 502, code)
	assert.Contains(t, string(b), "proxy_failed")
}

func TestSettings_MentionIDs(t *testing.T) {
	e := newSettingsEnv(t)
	ctx := context.Background()
	code, b := e.do("PUT", "/settings", map[string]any{
		"discord_mention_id":  " 123456789012345678 ",
		"telegram_mention_id": "987654321",
	})
	require.Equal(t, 200, code)
	var got settingsBody
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, "123456789012345678", got.DiscordMentionID)
	assert.Equal(t, "987654321", got.TelegramMentionID)
	assert.Equal(t, 1, e.updates, "a save rebuilds the notifier")

	for _, bad := range []map[string]any{
		{"discord_mention_id": "<@123>"},
		{"discord_mention_id": "@everyone"},
		{"discord_mention_id": "12a"},
		{"discord_mention_id": "123456789012345678901"},
		{"telegram_mention_id": "@grub_user"},
		{"telegram_mention_id": "-100123"},
		{"telegram_mention_id": `1"><b>`},
	} {
		code, _ = e.do("PUT", "/settings", bad)
		assert.Equal(t, 400, code, bad)
	}
	d, _ := e.st.DiscordMentionID(ctx)
	tg, _ := e.st.TelegramMentionID(ctx)
	assert.Equal(t, "123456789012345678", d, "a rejected save keeps the old ID")
	assert.Equal(t, "987654321", tg)

	code, _ = e.do("PUT", "/settings", map[string]any{"discord_mention_id": "", "telegram_mention_id": ""})
	require.Equal(t, 200, code)
	d, _ = e.st.DiscordMentionID(ctx)
	tg, _ = e.st.TelegramMentionID(ctx)
	assert.Empty(t, d, "empty clears the mention")
	assert.Empty(t, tg)
}

// Follow raids reads off by default, saves, and a change tells every
// runner to apply it live; an unchanged value does not.
func TestSettings_FollowRaids(t *testing.T) {
	e := newSettingsEnv(t)
	_, b := e.do("GET", "/settings", nil)
	var got settingsBody
	require.NoError(t, json.Unmarshal(b, &got))
	assert.False(t, got.FollowRaids, "off by default")

	code, b := e.do("PUT", "/settings", map[string]any{"follow_raids": true})
	require.Equal(t, 200, code)
	require.NoError(t, json.Unmarshal(b, &got))
	assert.True(t, got.FollowRaids)
	assert.Equal(t, 1, e.live, "a change applies to the running watches")
	assert.Equal(t, 0, e.rescans, "no rescan")

	code, _ = e.do("PUT", "/settings", map[string]any{"follow_raids": true})
	require.Equal(t, 200, code)
	assert.Equal(t, 1, e.live, "unchanged value")

	code, b = e.do("PUT", "/settings", map[string]any{"follow_raids": false})
	require.Equal(t, 200, code)
	require.NoError(t, json.Unmarshal(b, &got))
	assert.False(t, got.FollowRaids)
	assert.Equal(t, 2, e.live)
}

// Sharing the Android catalog reads off by default, saves, and a change
// rescans every runner so TV accounts list the shared campaigns now.
func TestSettings_ExpShareCatalog(t *testing.T) {
	e := newSettingsEnv(t)
	_, b := e.do("GET", "/settings", nil)
	var got settingsBody
	require.NoError(t, json.Unmarshal(b, &got))
	assert.False(t, got.ExpShareCatalog, "off by default")

	code, b := e.do("PUT", "/settings", map[string]any{"exp_share_catalog": true})
	require.Equal(t, 200, code)
	require.NoError(t, json.Unmarshal(b, &got))
	assert.True(t, got.ExpShareCatalog)
	assert.Equal(t, 1, e.rescans, "a change rescans")
	on, err := e.st.ExpShareCatalog(context.Background())
	require.NoError(t, err)
	assert.True(t, on)

	code, _ = e.do("PUT", "/settings", map[string]any{"exp_share_catalog": true})
	require.Equal(t, 200, code)
	assert.Equal(t, 1, e.rescans, "unchanged value")

	code, b = e.do("PUT", "/settings", map[string]any{"exp_share_catalog": false})
	require.Equal(t, 200, code)
	require.NoError(t, json.Unmarshal(b, &got))
	assert.False(t, got.ExpShareCatalog)
	assert.Equal(t, 2, e.rescans)
}

// share_catalog_available follows ShareCatalogReady, is false without it, and
// the GET body sent back is accepted with the field ignored.
func TestSettings_ShareCatalogAvailable(t *testing.T) {
	e := newSettingsEnv(t)
	_, b := e.do("GET", "/settings", nil)
	var got map[string]any
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, false, got["share_catalog_available"], "no source wired")

	android := false
	e.s.d.ShareCatalogReady = func(context.Context) bool { return android }
	_, b = e.do("GET", "/settings", nil)
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, false, got["share_catalog_available"])

	android = true
	_, b = e.do("GET", "/settings", nil)
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, true, got["share_catalog_available"])

	code, _ := e.do("PUT", "/settings", map[string]any{"share_catalog_available": false, "exp_share_catalog": true})
	require.Equal(t, 200, code, "read-only field is ignored")
	_, b = e.do("GET", "/settings", nil)
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, true, got["share_catalog_available"])
	assert.Equal(t, true, got["exp_share_catalog"])
}
