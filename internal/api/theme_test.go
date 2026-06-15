package api

import (
	"bytes"
	"strings"
	"testing"

	"github.com/aalejandrofer/grubdrops/internal/web"
)

func renderLayoutPage(t *testing.T) string {
	t.Helper()
	tmpl, err := web.Templates()
	if err != nil {
		t.Fatalf("load templates: %v", err)
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "settings.html", templateData{
		AuthedAdmin: true, CSRFToken: "tok", Active: "settings",
		Page: settingsPageData{},
	}); err != nil {
		t.Fatalf("render: %v", err)
	}
	return buf.String()
}

// TestTheme_NoFOUCInit verifies the layout sets the saved theme on the document
// element from localStorage BEFORE paint (no flash of the wrong theme). The init
// must live in <head>, ahead of <body>.
func TestTheme_NoFOUCInit(t *testing.T) {
	out := renderLayoutPage(t)
	headEnd := strings.Index(out, "</head>")
	if headEnd < 0 {
		t.Fatal("no </head> in layout")
	}
	head := out[:headEnd]
	for _, want := range []string{"localStorage", "theme", "data-theme"} {
		if !strings.Contains(head, want) {
			t.Errorf("theme init missing %q in <head> (FOUC risk)", want)
		}
	}
}

// TestTheme_ToggleButton verifies a theme toggle control is rendered in the nav.
func TestTheme_ToggleButton(t *testing.T) {
	out := renderLayoutPage(t)
	if !strings.Contains(out, "theme-toggle") {
		t.Errorf("nav missing theme-toggle control")
	}
}
