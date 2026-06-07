package web

import "testing"

// TestTemplatesParse loads every embedded template through the real
// loader. html/template only reports unbalanced {{end}} / bad actions at
// parse time, not at `go build` — so without this, a broken template
// crash-loops the binary in prod. Keep it as the parse smoke test.
func TestTemplatesParse(t *testing.T) {
	if _, err := Templates(); err != nil {
		t.Fatalf("Templates() parse failed: %v", err)
	}
}
