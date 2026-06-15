package api

import (
	"bytes"
	"strings"
	"testing"

	"github.com/aalejandrofer/grubdrops/internal/web"
)

func renderDropsTable(t *testing.T, page dropsPage) string {
	t.Helper()
	tmpl, err := web.Templates()
	if err != nil {
		t.Fatalf("load templates: %v", err)
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "drops_table", page); err != nil {
		t.Fatalf("render drops_table: %v", err)
	}
	return buf.String()
}

// TestDropsTable_ColdStartCTA verifies the cold-start trap fix: when no
// games are whitelisted at all (NoWhitelist), discovery never runs and the
// page would otherwise be silently empty. We must show a bootstrap CTA that
// links to where the user can add a game, instead of the misleading
// "discovery populates this list" empty text.
func TestDropsTable_ColdStartCTA(t *testing.T) {
	out := renderDropsTable(t, dropsPage{Tab: tabCurrent, NoWhitelist: true})
	if !strings.Contains(strings.ToLower(out), "no games whitelisted") {
		t.Errorf("cold-start panel missing the 'no games whitelisted' explanation")
	}
	if !strings.Contains(out, `href="/settings/priority"`) {
		t.Errorf("cold-start panel must link to where games are added")
	}
}

// TestDropsTable_NoColdStartWhenWhitelisted verifies the CTA does NOT appear
// once any game is whitelisted (NoWhitelist false) — the normal case.
func TestDropsTable_NoColdStartWhenWhitelisted(t *testing.T) {
	out := renderDropsTable(t, dropsPage{Tab: tabCurrent, NoWhitelist: false})
	if strings.Contains(strings.ToLower(out), "no games whitelisted") {
		t.Errorf("cold-start panel should not render when a whitelist exists")
	}
}
