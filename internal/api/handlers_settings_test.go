package api

import (
	"bytes"
	"context"
	"errors"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aalejandrofer/grubdrops/internal/store"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
	"github.com/aalejandrofer/grubdrops/internal/web"
)

func TestSettingsTemplateRenders(t *testing.T) {
	tmpl, err := web.Templates()
	if err != nil {
		t.Fatalf("load templates: %v", err)
	}
	var buf bytes.Buffer
	err = tmpl.ExecuteTemplate(&buf, "settings.html", templateData{
		AuthedAdmin: true, CSRFToken: "tok", Active: "notifications",
		Page: settingsPageData{
			GlobalDiscordWebhook: "https://discord.com/api/webhooks/x",
			NotifyAvatarURL:      "https://img/a.png",
			NotifyClaim:          true,
		},
	})
	if err != nil {
		t.Fatalf("render settings.html: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		`name="notify_avatar_url"`,        // avatar input wired
		`https://img/a.png`,               // its value rendered
		`hx-post="/settings/notify-test"`, // test button wired
		`id="notify-test-result"`,         // result target present
	} {
		if !strings.Contains(out, want) {
			t.Errorf("settings.html missing %q", want)
		}
	}
}

type fakeNotifier struct {
	calls int
	last  map[string]any
	err   error
}

func (f *fakeNotifier) Notify(_ context.Context, _ string, fields map[string]any) error {
	f.calls++
	f.last = fields
	return f.err
}

// newTestSettings spins up a migrated sqlite-backed settings store + queries.
func newTestSettings(t *testing.T) (*store.Settings, *gen.Queries) {
	t.Helper()
	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	q := gen.New(db)
	return store.NewSettings(q), q
}

func TestNotifyTest_FiresSampleAndReportsOK(t *testing.T) {
	s, q := newTestSettings(t)
	if err := s.SetGlobalDiscordWebhook(context.Background(), "https://discord/x"); err != nil {
		t.Fatal(err)
	}
	fn := &fakeNotifier{}
	d := &settingsDeps{notifier: fn, s: s, q: q}

	rec := httptest.NewRecorder()
	d.notifyTest(rec, httptest.NewRequest("POST", "/settings/notify-test", nil))

	if fn.calls != 1 {
		t.Fatalf("expected notifier called once, got %d", fn.calls)
	}
	if got := strings.ToLower(rec.Body.String()); !strings.Contains(got, "sent") {
		t.Fatalf("expected success fragment, got %q", rec.Body.String())
	}
	// Sample must carry the rich fields so the operator sees a real-looking embed.
	for _, k := range []string{"game", "drop", "channel", "platform", "req_min"} {
		if _, ok := fn.last[k]; !ok {
			t.Errorf("sample event missing %q field", k)
		}
	}
}

func TestNotifyTest_ReportsErrorFromNotifier(t *testing.T) {
	s, q := newTestSettings(t)
	_ = s.SetGlobalDiscordWebhook(context.Background(), "https://discord/x")
	fn := &fakeNotifier{err: errors.New("webhook 404")}
	d := &settingsDeps{notifier: fn, s: s, q: q}

	rec := httptest.NewRecorder()
	d.notifyTest(rec, httptest.NewRequest("POST", "/settings/notify-test", nil))

	if got := rec.Body.String(); !strings.Contains(got, "webhook 404") {
		t.Fatalf("expected error surfaced, got %q", got)
	}
}

func TestNotifyTest_NoWebhookConfigured(t *testing.T) {
	// Notifier wired, but no global webhook and no account webhooks → must
	// report honestly and NOT call the notifier (avoids silent Noop success).
	s, q := newTestSettings(t)
	fn := &fakeNotifier{}
	d := &settingsDeps{notifier: fn, s: s, q: q}

	rec := httptest.NewRecorder()
	d.notifyTest(rec, httptest.NewRequest("POST", "/settings/notify-test", nil))

	if fn.calls != 0 {
		t.Fatalf("notifier should not fire with no webhook, got %d calls", fn.calls)
	}
	if got := strings.ToLower(rec.Body.String()); !strings.Contains(got, "no webhook") {
		t.Fatalf("expected 'no webhook' message, got %q", rec.Body.String())
	}
}

func TestNotifyTest_NoNotifierConfigured(t *testing.T) {
	d := &settingsDeps{notifier: nil}
	rec := httptest.NewRecorder()
	d.notifyTest(rec, httptest.NewRequest("POST", "/settings/notify-test", nil))
	if got := strings.ToLower(rec.Body.String()); !strings.Contains(got, "no notifier") {
		t.Fatalf("expected 'no notifier' message, got %q", rec.Body.String())
	}
}

func TestSettings_SSOCard_Enabled(t *testing.T) {
	tmpl, err := web.Templates()
	if err != nil {
		t.Fatalf("load templates: %v", err)
	}
	var buf bytes.Buffer
	err = tmpl.ExecuteTemplate(&buf, "settings.html", templateData{
		Active: "security",
		Page: settingsPageData{
			OIDC: settingsOIDC{
				Enabled:      true,
				ProviderName: "authentik",
				Issuer:       "https://auth.ryuzec.dev/application/o/grubdrops/",
				CallbackURL:  "https://drops.ryuzec.dev/auth/oidc/callback",
			},
		},
	})
	if err != nil {
		t.Fatalf("render settings: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"Single sign-on", "authentik", "auth.ryuzec.dev", "drops.ryuzec.dev/auth/oidc/callback"} {
		if !strings.Contains(out, want) {
			t.Errorf("settings missing %q", want)
		}
	}
}

func TestSettings_SSOCard_Disabled(t *testing.T) {
	tmpl, err := web.Templates()
	if err != nil {
		t.Fatalf("load templates: %v", err)
	}
	var buf bytes.Buffer
	err = tmpl.ExecuteTemplate(&buf, "settings.html", templateData{
		Active: "security",
		Page:   settingsPageData{OIDC: settingsOIDC{Enabled: false}},
	})
	if err != nil {
		t.Fatalf("render settings: %v", err)
	}
	if !strings.Contains(buf.String(), "Not configured") {
		t.Errorf("expected disabled SSO card to show 'Not configured'")
	}
}

func renderSettingsTab(t *testing.T, active string, page settingsPageData) string {
	t.Helper()
	tmpl, err := web.Templates()
	if err != nil {
		t.Fatalf("load templates: %v", err)
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "settings.html", templateData{Active: active, Page: page}); err != nil {
		t.Fatalf("render: %v", err)
	}
	return buf.String()
}

func TestSettingsTabs_SubnavHasAllLinks(t *testing.T) {
	out := renderSettingsTab(t, "settings", settingsPageData{})
	for _, want := range []string{
		`href="/settings"`,
		`href="/settings/notifications"`, `href="/settings/security"`,
		`href="/settings/accounts"`, `href="/settings/experimental"`,
		`href="/settings/health"`,
		"General", "Notifications", "Security", "Accounts", "Experimental", "Health",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("subnav missing %q", want)
		}
	}
	// Drop Priority moved out of Settings to the top-level /priority nav item.
	if strings.Contains(out, `href="/settings/priority"`) {
		t.Errorf("settings subnav should no longer link to priority (moved to /priority)")
	}
}

func TestSettingsTabs_GeneralSectionOnly(t *testing.T) {
	out := renderSettingsTab(t, "settings", settingsPageData{})
	if !strings.Contains(out, `name="tick_interval_sec"`) {
		t.Errorf("general tab should show interval fields")
	}
	if strings.Contains(out, `action="/settings/global-games"`) {
		t.Errorf("general tab should NOT show the priority list form")
	}
	if strings.Contains(out, `name="discord_webhook"`) {
		t.Errorf("general tab should NOT show notifications")
	}
}

func TestSettingsTabs_PrioritySection(t *testing.T) {
	out := renderSettingsTab(t, "priority", settingsPageData{})
	if !strings.Contains(out, `action="/settings/global-games"`) {
		t.Errorf("priority tab should show the global priority list form")
	}
	if !strings.Contains(out, `name="priority_mode"`) {
		t.Errorf("priority tab should show the priority mode selector")
	}
	if !strings.Contains(out, `action="/settings/priority-mode"`) {
		t.Errorf("priority mode posts to its own endpoint")
	}
}

func TestSettingsTabs_NotificationsSection(t *testing.T) {
	out := renderSettingsTab(t, "notifications", settingsPageData{})
	if !strings.Contains(out, `name="discord_webhook"`) {
		t.Errorf("notifications tab should show the webhook field")
	}
	if !strings.Contains(out, `action="/settings/notifications"`) {
		t.Errorf("notifications form posts to /settings/notifications")
	}
}

func TestSettingsTabs_SecuritySection(t *testing.T) {
	out := renderSettingsTab(t, "security", settingsPageData{OIDC: settingsOIDC{Enabled: false}})
	if !strings.Contains(out, "Single sign-on") {
		t.Errorf("security tab should show the SSO card")
	}
	if !strings.Contains(out, `action="/settings/password"`) {
		t.Errorf("security tab should show the password form")
	}
}

// TestSettingsTabs_HealthSection verifies the Health tab renders canary
// results, "not configured" for unconfigured platforms, the settings form,
// and the Run-now HTMX button.
func TestSettingsTabs_HealthSection(t *testing.T) {
	out := renderSettingsTab(t, "health", settingsPageData{
		CanaryTwitch: canaryView{
			Configured: true,
			OK:         true,
			Detail:     "",
			When:       "2m ago",
		},
		CanaryKick:          canaryView{Configured: false},
		CanaryTwitchChannel: "somestreamer",
		CanaryKickChannel:   "",
		CanaryIntervalSec:   300,
	})

	// Twitch OK result shows success indicator
	if !strings.Contains(out, "canary-ok") {
		t.Errorf("health tab should show canary-ok class for a passing twitch result")
	}
	// Twitch "when" is shown
	if !strings.Contains(out, "2m ago") {
		t.Errorf("health tab should show the 'when' timestamp for twitch")
	}
	// Kick not configured
	if !strings.Contains(out, "not configured") {
		t.Errorf("health tab should show 'not configured' for unconfigured kick")
	}
	// Settings form fields present
	if !strings.Contains(out, `name="canary_twitch_channel"`) {
		t.Errorf("health tab should show canary_twitch_channel input")
	}
	if !strings.Contains(out, `name="canary_kick_channel"`) {
		t.Errorf("health tab should show canary_kick_channel input")
	}
	if !strings.Contains(out, `name="canary_interval_sec"`) {
		t.Errorf("health tab should show canary_interval_sec input")
	}
	// Form posts to /settings/canary
	if !strings.Contains(out, `action="/settings/canary"`) {
		t.Errorf("health tab canary form should post to /settings/canary")
	}
	// Run-now HTMX control
	if !strings.Contains(out, `hx-post="/settings/canary/run"`) {
		t.Errorf("health tab should have Run-now hx-post control")
	}
	if !strings.Contains(out, `hx-target="#canary-panel"`) {
		t.Errorf("health tab Run-now should target #canary-panel")
	}
	// canary-panel id present for fragment swap target
	if !strings.Contains(out, `id="canary-panel"`) {
		t.Errorf("health tab should have id=canary-panel for htmx swap")
	}
}

// TestCanaryPanelFragment verifies the canary_panel template can be rendered
// stand-alone (the fragment path used by canaryRun).
func TestCanaryPanelFragment(t *testing.T) {
	t.Helper()
	tmpl, err := web.Templates()
	if err != nil {
		t.Fatalf("load templates: %v", err)
	}
	var buf bytes.Buffer
	err = tmpl.ExecuteTemplate(&buf, "canary_panel", templateData{
		CSRFToken: "testtoken",
		Active:    "health",
		Page: settingsPageData{
			CanaryTwitch: canaryView{Configured: true, OK: false, Detail: "beacon timeout", When: "1m ago"},
			CanaryKick:   canaryView{Configured: true, OK: true, When: "30s ago"},
		},
	})
	if err != nil {
		t.Fatalf("render canary_panel: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "canary-err") {
		t.Errorf("canary_panel fragment: twitch failure should show canary-err class")
	}
	if !strings.Contains(out, "beacon timeout") {
		t.Errorf("canary_panel fragment: twitch detail should be in output")
	}
	if !strings.Contains(out, "canary-ok") {
		t.Errorf("canary_panel fragment: kick ok should show canary-ok class")
	}
	if !strings.Contains(out, `hx-post="/settings/canary/run"`) {
		t.Errorf("canary_panel fragment: Run-now button missing")
	}
}
