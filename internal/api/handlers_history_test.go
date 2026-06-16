package api

import (
	"testing"
	"time"

	mlog "github.com/aalejandrofer/grubdrops/internal/log"
	"github.com/stretchr/testify/assert"
)

// A real reward emit carries a title (game may be absent for Kick); the
// double-emit twin carries only benefit_name and no title and must be
// dropped so it never renders "reward · — · —".
func TestRewardClaimsFromRing_FiltersTitlelessEntry(t *testing.T) {
	ts := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	lines := []mlog.LogLine{
		// Good: sweep emit, title present, no game (Kick reward).
		{TS: ts, Kind: "claim", Fields: map[string]any{
			"account": "acc1", "title": "Kick + Rust Wallpaper Pattern",
		}},
		// Malformed: benefit-complete emit, no title field.
		{TS: ts, Kind: "claim", Fields: map[string]any{
			"account": "acc1", "benefit": "b1", "benefit_name": "Kick + Rust Wallpaper Pattern",
		}},
		// Malformed: title is the "—" sentinel.
		{TS: ts, Kind: "claim", Fields: map[string]any{
			"account": "acc1", "title": "—",
		}},
		// Malformed: blank title.
		{TS: ts, Kind: "claim", Fields: map[string]any{
			"account": "acc1", "title": "  ",
		}},
	}

	got := rewardClaimsFromRing(lines, map[string]string{"acc1": "@nori"}, map[string]string{"acc1": "kick"}, time.UTC)

	assert.Len(t, got, 1, "only the title-bearing entry should survive")
	assert.Equal(t, "Kick + Rust Wallpaper Pattern", got[0].Title)
	assert.Equal(t, "", got[0].Game, "Kick reward has no game")
	assert.Equal(t, "@nori", got[0].Account)
}

// A reward claim owned by a Kick account must render Platform "kick" so
// the template colors the account label green, not Twitch purple. A
// Twitch-owned claim stays "twitch", and an account with no resolvable
// platform falls back to "twitch" (the old default).
func TestRewardClaimsFromRing_PlatformFromAccount(t *testing.T) {
	ts := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	lines := []mlog.LogLine{
		{TS: ts, Kind: "claim", Fields: map[string]any{
			"account": "acc_kick", "title": "Kick + Rust Wallpaper Logo",
		}},
		{TS: ts, Kind: "claim", Fields: map[string]any{
			"account": "acc_twitch", "title": "Twitch + Rust Crate",
		}},
		{TS: ts, Kind: "claim", Fields: map[string]any{
			"account": "acc_unknown", "title": "Orphan Reward",
		}},
	}
	platformByID := map[string]string{"acc_kick": "kick", "acc_twitch": "twitch"}

	got := rewardClaimsFromRing(lines, nil, platformByID, time.UTC)

	assert.Len(t, got, 3)
	byTitle := map[string]string{}
	for _, c := range got {
		byTitle[c.Title] = c.Platform
	}
	assert.Equal(t, "kick", byTitle["Kick + Rust Wallpaper Logo"], "Kick account reward must be kick")
	assert.Equal(t, "twitch", byTitle["Twitch + Rust Crate"], "Twitch account reward stays twitch")
	assert.Equal(t, "twitch", byTitle["Orphan Reward"], "unresolved account falls back to twitch")
}

// Non-claim ring entries are ignored.
func TestRewardClaimsFromRing_SkipsNonClaimKinds(t *testing.T) {
	lines := []mlog.LogLine{
		{Kind: "progress", Fields: map[string]any{"title": "ignored"}},
		{Kind: "discovery", Fields: map[string]any{"title": "ignored"}},
	}
	assert.Empty(t, rewardClaimsFromRing(lines, nil, nil, time.UTC))
}

// The watcher's sweep + benefit-complete double-emit (same account,
// title, timestamp) collapses to one row even though one emit has a
// game and the other does not.
func TestDedupeClaims_CollapsesDoubleEmit(t *testing.T) {
	claims := []historyClaim{
		{When: "2026-06-12 10:00", Platform: "twitch", Game: "Rust", Title: "Wallpaper", Account: "@nori", Source: "reward"},
		{When: "2026-06-12 10:00", Platform: "twitch", Game: "", Title: "Wallpaper", Account: "@nori", Source: "reward"},
	}
	got := dedupeClaims(claims)
	assert.Len(t, got, 1, "double-emit with differing game must collapse")
}

// A Kick claim that reached the claims table (DROP row, has game) and
// its redundant reward-reaper ring row (REWARD, no game, "@"-less
// account, off-by-seconds time) must collapse to the single DROP row.
// A reaper-only reward with no DROP counterpart survives.
func TestSuppressDuplicateRewardRows(t *testing.T) {
	claims := []historyClaim{
		// Claims-table DROP row: green, carries game + @account.
		{When: "2026-06-12 10:00", Platform: "kick", Game: "Rust", Title: "Kick + Rust Wallpaper Pattern", Account: "@Phluses", Source: "drop"},
		// Ring REWARD row for the SAME claim: no game, no "@", +1 min.
		{When: "2026-06-12 10:01", Platform: "kick", Game: "", Title: "Kick + Rust Wallpaper Pattern", Account: "Phluses", Source: "reward"},
		// Reaper-only REWARD with no DROP counterpart — must survive.
		{When: "2026-06-12 09:30", Platform: "twitch", Game: "", Title: "Legacy Reaper Reward", Account: "@nori", Source: "reward"},
	}

	got := suppressDuplicateRewardRows(claims)

	assert.Len(t, got, 2, "the duplicate reward row is suppressed; drop + orphan reward remain")
	bySource := map[string]int{}
	for _, c := range got {
		bySource[c.Source+"|"+c.Title]++
	}
	assert.Equal(t, 1, bySource["drop|Kick + Rust Wallpaper Pattern"], "the DROP row is kept")
	assert.Equal(t, 0, bySource["reward|Kick + Rust Wallpaper Pattern"], "the duplicate REWARD row is gone")
	assert.Equal(t, 1, bySource["reward|Legacy Reaper Reward"], "the reaper-only REWARD survives")
}

// Genuinely distinct claims (different title or time) are kept.
func TestDedupeClaims_KeepsDistinct(t *testing.T) {
	claims := []historyClaim{
		{When: "2026-06-12 10:00", Platform: "twitch", Title: "A", Account: "@nori"},
		{When: "2026-06-12 11:00", Platform: "twitch", Title: "A", Account: "@nori"},
		{When: "2026-06-12 10:00", Platform: "twitch", Title: "B", Account: "@nori"},
	}
	assert.Len(t, dedupeClaims(claims), 3)
}
