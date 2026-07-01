package web

import (
	"bytes"
	"strings"
	"testing"
)

// The language switcher POSTs to /api/lang via a JS-built hidden form. That
// endpoint is behind CSRF protection, so the form MUST carry the csrf_token —
// otherwise every language switch 403s. This regression guards Abu's bug where
// switching languages was impossible.
func TestLangSwitchFormCarriesCSRFToken(t *testing.T) {
	tmpls, err := Templates()
	if err != nil {
		t.Fatalf("Templates(): %v", err)
	}
	const token = "TESTCSRFTOKEN123"
	var buf bytes.Buffer
	data := map[string]any{
		"CSRFToken":   token,
		"Lang":        "en",
		"AuthedAdmin": false,
	}
	if err := tmpls.ExecuteTemplate(&buf, "layout", data); err != nil {
		t.Fatalf("render layout: %v", err)
	}
	out := buf.String()

	// Locate the language-switch form builder.
	idx := strings.Index(out, "/api/lang")
	if idx < 0 {
		t.Fatal("layout does not reference /api/lang")
	}
	// The csrf_token field and its value must appear near the form builder.
	region := out[idx:]
	if end := idx + 800; end < len(out) {
		region = out[idx:end]
	}
	if !strings.Contains(region, "csrf_token") {
		t.Errorf("lang-switch form is missing a csrf_token field:\n%s", region)
	}
	if !strings.Contains(region, token) {
		t.Errorf("lang-switch form does not inject the CSRF token value %q:\n%s", token, region)
	}
}
