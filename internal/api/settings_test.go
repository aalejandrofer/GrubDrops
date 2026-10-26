package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/store"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

type fakeNotify struct {
	err    error
	events []string
}

func (f *fakeNotify) Notify(_ context.Context, e string, _ map[string]any) error {
	f.events = append(f.events, e)
	return f.err
}

type settingsEnv struct {
	h       http.Handler
	st      *store.Settings
	n       *fakeNotify
	updates int
	zones   []string
}

func newSettingsEnv(t *testing.T) *settingsEnv {
	t.Helper()
	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "t.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	e := &settingsEnv{st: store.NewSettings(gen.New(db)), n: &fakeNotify{}}
	s := &server{d: Deps{
		Settings: e.st, Notifier: e.n,
		OnSettingsUpdate: func() { e.updates++ },
		SetZone:          func(n string) { e.zones = append(e.zones, n) },
	}}
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
	assert.Equal(t, map[string]any{"claim": true, "auth": false, "errors": true, "daily_summary": false}, got["notify"])
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
	} {
		code, _ := e.do("PUT", "/settings", body)
		assert.Equal(t, 400, code)
		th, _ := e.st.UITheme(context.Background())
		assert.Equal(t, "dark", th)
	}
	assert.Equal(t, 0, e.updates)
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
	local := time.Local
	t.Cleanup(func() { time.Local = local })
	time.Local = time.FixedZone("Local", 0)
	assert.Equal(t, "UTC", zone(build(nil)))
}
