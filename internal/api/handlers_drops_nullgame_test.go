package api

import (
	"strings"
	"testing"
)

// TestDropsTable_DisabledAccountTaggedInDropdown verifies a disabled account
// still appears in the null-game WHITELIST+ dropdown (so a whitelist can be
// pre-staged) but is visually tagged + dimmed so it's clearly off.
func TestDropsTable_DisabledAccountTaggedInDropdown(t *testing.T) {
	page := dropsPage{
		Tab: tabCurrent,
		NullGameRows: []dropsRow{{
			Platform: "kick", CampaignName: "Football Drop", Channels: []string{"footy"},
		}},
		Accounts: []dropsAccount{
			{ID: "a1", Label: "TTik3r (kick)", Platform: "kick"},
			{ID: "a2", Label: "Phluses (kick)", Platform: "kick", Disabled: true},
		},
	}
	out := renderDropsTable(t, page)
	if !strings.Contains(out, `value="a2" class="opt-disabled"`) {
		t.Errorf("disabled account option missing opt-disabled class")
	}
	if !strings.Contains(out, "Phluses (kick) · disabled") {
		t.Errorf("disabled account option missing ' · disabled' tag")
	}
	// Enabled account must remain a plain option.
	if !strings.Contains(out, `value="a1">TTik3r (kick)</option>`) {
		t.Errorf("enabled account option should be untagged")
	}
}

// TestPartitionNullGame_DisabledDoesNotBlockPromotion is the regression for the
// stuck-in-discover bug: a null-game drop must be promoted to the Whitelisted
// (mining) table once every ENABLED matching-platform account whitelists one of
// its channels. A disabled account on the same platform must NOT count toward
// "fully adopted" — otherwise it permanently blocks promotion (it never mines,
// so it never whitelists), leaving the drop stranded in the Discoverable
// null-game section even though the enabled account already mines it.
func TestPartitionNullGame_DisabledDoesNotBlockPromotion(t *testing.T) {
	rows := []dropsRow{{
		Platform: "kick",
		Game:     "", // null-game
		Channels: []string{"footy"},
	}}
	accts := []nullGameAcct{
		// Enabled account already whitelists the channel.
		{id: "acc-on", login: "TTik3r", platform: "kick", enabled: true,
			chans: map[string]bool{"footy": true}},
		// Disabled account on the same platform, no whitelist. Must be ignored.
		{id: "acc-off", login: "Phluses", platform: "kick", enabled: false,
			chans: map[string]bool{}},
	}

	kept, nullGame, promoted := partitionNullGame(rows, accts)

	if len(kept) != 0 {
		t.Fatalf("kept = %d, want 0 (row is null-game)", len(kept))
	}
	if len(nullGame) != 0 {
		t.Fatalf("nullGame = %d, want 0 — disabled account must not block promotion", len(nullGame))
	}
	if len(promoted) != 1 {
		t.Fatalf("promoted = %d, want 1 — enabled account adopts it", len(promoted))
	}
	// The disabled account must not show as a ✓ mining chip.
	if got := len(promoted[0].WhitelistedBy); got != 1 {
		t.Fatalf("WhitelistedBy = %d, want 1 (only the enabled account)", got)
	}
	if promoted[0].WhitelistedBy[0].AccountID != "acc-on" {
		t.Fatalf("chip = %q, want acc-on", promoted[0].WhitelistedBy[0].AccountID)
	}
}

// TestPartitionNullGame_StaysWhenEnabledNotAdopted verifies a null-game drop
// stays in the dedicated section while any enabled matching-platform account
// has NOT yet whitelisted it.
func TestPartitionNullGame_StaysWhenEnabledNotAdopted(t *testing.T) {
	rows := []dropsRow{{Platform: "kick", Game: "", Channels: []string{"footy"}}}
	accts := []nullGameAcct{
		{id: "a1", login: "one", platform: "kick", enabled: true, chans: map[string]bool{"footy": true}},
		{id: "a2", login: "two", platform: "kick", enabled: true, chans: map[string]bool{}},
	}

	_, nullGame, promoted := partitionNullGame(rows, accts)

	if len(promoted) != 0 {
		t.Fatalf("promoted = %d, want 0 — a2 has not adopted it", len(promoted))
	}
	if len(nullGame) != 1 {
		t.Fatalf("nullGame = %d, want 1", len(nullGame))
	}
}

// TestPartitionNullGame_NonNullStaysKept verifies rows with a game (or no
// channels) are left in the Discoverable list untouched.
func TestPartitionNullGame_NonNullStaysKept(t *testing.T) {
	rows := []dropsRow{
		{Platform: "kick", Game: "Some Game", Channels: []string{"footy"}},
		{Platform: "kick", Game: "", Channels: nil},
	}
	kept, nullGame, promoted := partitionNullGame(rows, nil)

	if len(kept) != 2 {
		t.Fatalf("kept = %d, want 2", len(kept))
	}
	if len(nullGame) != 0 || len(promoted) != 0 {
		t.Fatalf("nullGame=%d promoted=%d, want 0/0", len(nullGame), len(promoted))
	}
}
