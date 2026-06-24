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
	if !strings.Contains(out, `href="/priority"`) {
		t.Errorf("cold-start panel must link to the Priority page where games are added")
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

func renderCampaignItems(t *testing.T, detail campaignDetailRow) string {
	t.Helper()
	tmpl, err := web.Templates()
	if err != nil {
		t.Fatalf("load templates: %v", err)
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "drops_campaign_items", detail); err != nil {
		t.Fatalf("render drops_campaign_items: %v", err)
	}
	return buf.String()
}

// TestCampaignItems_CollectedMarkIsClickableUncollect verifies the manual
// un-collect escape hatch: each COLLECTED mark renders as an hx-post button
// carrying the exact (account_id, benefit_id, campaign_id) to delete, plus a
// confirm prompt. No visible "X" — the action is the chip itself.
func TestCampaignItems_CollectedMarkIsClickableUncollect(t *testing.T) {
	detail := campaignDetailRow{
		ID:        "camp-1",
		CSRFToken: "tok123",
		Benefits: []campaignBenefitRow{{
			Name:            "Esports Pack",
			RequiredMinutes: 60,
			Collected: []collectedMark{{
				Login: "TTik3r", Platform: "kick",
				AccountID: "acc-9", BenefitID: "ben-5",
			}},
		}},
	}
	out := renderCampaignItems(t, detail)

	for _, want := range []string{
		`hx-post="/drops/claim/remove"`,
		`hx-confirm=`,
		`"account_id":"acc-9"`,
		`"benefit_id":"ben-5"`,
		`"campaign_id":"camp-1"`,
		`tok123`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("un-collect control missing %q\n--- rendered ---\n%s", want, out)
		}
	}
	if strings.Contains(out, "✕") || strings.Contains(out, " x<") {
		t.Errorf("un-collect mark must not show a visible X")
	}
}
