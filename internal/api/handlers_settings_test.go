package api

import (
	"bytes"
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aalejandrofer/grubdrops/internal/web"
)

func TestSettingsTemplateRenders(t *testing.T) {
	tmpl, err := web.Templates()
	if err != nil {
		t.Fatalf("load templates: %v", err)
	}
	var buf bytes.Buffer
	err = tmpl.ExecuteTemplate(&buf, "settings.html", templateData{
		AuthedAdmin: true, CSRFToken: "tok", Active: "settings",
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
		`Hextech Chest`,                   // preview embed present
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

func TestNotifyTest_FiresSampleAndReportsOK(t *testing.T) {
	fn := &fakeNotifier{}
	d := &settingsDeps{notifier: fn}

	rec := httptest.NewRecorder()
	d.notifyTest(rec, httptest.NewRequest("POST", "/settings/notify-test", nil))

	if fn.calls != 1 {
		t.Fatalf("expected notifier called once, got %d", fn.calls)
	}
	if got := rec.Body.String(); !strings.Contains(strings.ToLower(got), "sent") {
		t.Fatalf("expected success fragment, got %q", got)
	}
	// Sample must carry the rich fields so the operator sees a real-looking embed.
	for _, k := range []string{"game", "drop", "channel", "platform", "req_min"} {
		if _, ok := fn.last[k]; !ok {
			t.Errorf("sample event missing %q field", k)
		}
	}
}

func TestNotifyTest_ReportsErrorFromNotifier(t *testing.T) {
	fn := &fakeNotifier{err: errors.New("webhook 404")}
	d := &settingsDeps{notifier: fn}

	rec := httptest.NewRecorder()
	d.notifyTest(rec, httptest.NewRequest("POST", "/settings/notify-test", nil))

	if got := rec.Body.String(); !strings.Contains(got, "webhook 404") {
		t.Fatalf("expected error surfaced, got %q", got)
	}
}

func TestNotifyTest_NoNotifierConfigured(t *testing.T) {
	d := &settingsDeps{notifier: nil}
	rec := httptest.NewRecorder()
	d.notifyTest(rec, httptest.NewRequest("POST", "/settings/notify-test", nil))
	if got := strings.ToLower(rec.Body.String()); !strings.Contains(got, "no webhook") && !strings.Contains(got, "not configured") {
		t.Fatalf("expected 'not configured' message, got %q", rec.Body.String())
	}
}
