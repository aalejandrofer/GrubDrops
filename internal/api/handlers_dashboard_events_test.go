package api

import (
	"testing"

	mlog "github.com/aalejandrofer/grubdrops/internal/log"
)

func TestClassifyEvent(t *testing.T) {
	cases := []struct {
		msg, level, want string
	}{
		{"drop claimed", "INFO", "claim"},
		{"minute-watched heartbeat", "INFO", "progress"},
		{"watch progress 12/60", "INFO", "progress"},
		{"device-code login pending", "INFO", "auth"},
		{"session expired", "WARN", "auth"}, // "session" matches auth before level fallback
		{"watcher state change", "INFO", "state"},
		{"watcher discovery", "INFO", "discovery"},
		{"campaign persisted", "INFO", "discovery"},
		{"totally unknown line", "ERROR", "error"},
		{"totally unknown line", "WARN", "error"},
		{"totally unknown line", "INFO", "info"},
	}
	for _, c := range cases {
		if got := classifyEvent(c.msg, c.level); got != c.want {
			t.Errorf("classifyEvent(%q,%q) = %q, want %q", c.msg, c.level, got, c.want)
		}
	}
}

func TestColorForKind(t *testing.T) {
	cases := map[string]string{
		"claim": "green", "progress": "amber", "state": "blue",
		"discovery": "muted", "error": "red", "auth": "accent",
	}
	for kind, want := range cases {
		if got := colorForKind(kind, "INFO"); got != want {
			t.Errorf("colorForKind(%q) = %q, want %q", kind, got, want)
		}
	}
	// Unknown kind falls back to the log level.
	if got := colorForKind("weird", "ERROR"); got != "red" {
		t.Errorf("colorForKind unknown/ERROR = %q, want red", got)
	}
	if got := colorForKind("weird", "INFO"); got != "muted" {
		t.Errorf("colorForKind unknown/INFO = %q, want muted", got)
	}
}

func TestHTMLEscape(t *testing.T) {
	// html.EscapeString escapes the double-quote too (&#34;) — the XSS-safe
	// behaviour that replaced the old custom escaper which left quotes raw.
	if got := htmlEscape(`<b>&"</b>`); got != `&lt;b&gt;&amp;&#34;&lt;/b&gt;` {
		t.Errorf("htmlEscape = %q", got)
	}
}

func TestDetailsForOrdersPriorityKeysAndDropsKind(t *testing.T) {
	l := mlog.LogLine{Fields: map[string]any{
		"kind": "claim", "zeta": "z", "account": "acc1", "channel": "chan",
	}}
	got := detailsFor(l)
	// kind dropped; account+channel (priority) before alphabetical rest (zeta).
	want := []string{"account", "channel", "zeta"}
	if len(got) != len(want) {
		t.Fatalf("detailsFor len = %d, want %d (%v)", len(got), len(want), got)
	}
	for i, k := range want {
		if got[i].Key != k {
			t.Errorf("detailsFor[%d].Key = %q, want %q", i, got[i].Key, k)
		}
	}
}
