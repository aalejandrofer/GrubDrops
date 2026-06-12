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

	got := rewardClaimsFromRing(lines, map[string]string{"acc1": "@nori"})

	assert.Len(t, got, 1, "only the title-bearing entry should survive")
	assert.Equal(t, "Kick + Rust Wallpaper Pattern", got[0].Title)
	assert.Equal(t, "", got[0].Game, "Kick reward has no game")
	assert.Equal(t, "@nori", got[0].Account)
}

// Non-claim ring entries are ignored.
func TestRewardClaimsFromRing_SkipsNonClaimKinds(t *testing.T) {
	lines := []mlog.LogLine{
		{Kind: "progress", Fields: map[string]any{"title": "ignored"}},
		{Kind: "discovery", Fields: map[string]any{"title": "ignored"}},
	}
	assert.Empty(t, rewardClaimsFromRing(lines, nil))
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

// Genuinely distinct claims (different title or time) are kept.
func TestDedupeClaims_KeepsDistinct(t *testing.T) {
	claims := []historyClaim{
		{When: "2026-06-12 10:00", Platform: "twitch", Title: "A", Account: "@nori"},
		{When: "2026-06-12 11:00", Platform: "twitch", Title: "A", Account: "@nori"},
		{When: "2026-06-12 10:00", Platform: "twitch", Title: "B", Account: "@nori"},
	}
	assert.Len(t, dedupeClaims(claims), 3)
}
