package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

func openV2(t *testing.T) (*sql.DB, *gen.Queries) {
	t.Helper()
	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "v2.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db, gen.New(db)
}

func seedAccount(t *testing.T, q *gen.Queries, id string) {
	t.Helper()
	now := time.Now().Unix()
	_, err := q.CreateAccount(context.Background(), gen.CreateAccountParams{
		ID: id, Platform: "twitch", DisplayName: id, Status: "idle",
		FingerprintJson: "{}", Enabled: 1, CreatedAt: now, UpdatedAt: now,
	})
	require.NoError(t, err)
}

func TestClaimLog_RecordKeepsFirstClaimAndSource(t *testing.T) {
	_, q := openV2(t)
	l := NewClaimLog(q)
	ctx := context.Background()
	t1 := time.Unix(1_700_000_000, 0)
	t2 := t1.Add(time.Hour)
	base := ClaimEntry{AccountID: "a1", Platform: "twitch", Game: "G", CampaignID: "c1", BenefitID: "b1", BenefitName: "B", Source: ClaimSourceGrubDrops, ClaimedAt: t1}
	require.NoError(t, l.Record(ctx, base))
	second := base
	second.Source, second.ClaimedAt, second.Code = ClaimSourceInventory, t2, "ABC"
	require.NoError(t, l.Record(ctx, second))
	got, err := l.Recent(ctx, 10)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, ClaimSourceGrubDrops, got[0].Source)
	require.True(t, got[0].ClaimedAt.Equal(t1))
	require.Equal(t, "ABC", got[0].Code)

	third := second
	third.Code = ""
	require.NoError(t, l.Record(ctx, third))
	got, err = l.Recent(ctx, 10)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "ABC", got[0].Code)
}

func TestClaimLog_ZeroTimeAndEmptyPlatform(t *testing.T) {
	_, q := openV2(t)
	l := NewClaimLog(q)
	ctx := context.Background()
	before := time.Now().Add(-time.Second)
	require.NoError(t, l.Record(ctx, ClaimEntry{AccountID: "a1", Platform: "kick", BenefitID: "b1", Source: ClaimSourceGrubDrops}))
	require.NoError(t, l.Record(ctx, ClaimEntry{AccountID: "a1", BenefitID: "b1", Source: ClaimSourceInventory}))
	got, err := l.Recent(ctx, 10)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "kick", got[0].Platform)
	require.False(t, got[0].ClaimedAt.Before(before), "zero ClaimedAt records now, got %v", got[0].ClaimedAt)
}

func TestClaimLog_ClaimedIDsPerAccount(t *testing.T) {
	_, q := openV2(t)
	l := NewClaimLog(q)
	ctx := context.Background()
	now := time.Now()
	require.NoError(t, l.Record(ctx, ClaimEntry{AccountID: "a1", Platform: "twitch", BenefitID: "b1", Source: ClaimSourceGrubDrops, ClaimedAt: now}))
	require.NoError(t, l.Record(ctx, ClaimEntry{AccountID: "a2", Platform: "twitch", BenefitID: "b2", Source: ClaimSourceGrubDrops, ClaimedAt: now}))
	got, err := l.ClaimedBenefitIDs(ctx, "a1")
	require.NoError(t, err)
	require.Equal(t, map[string]bool{"b1": true}, got)
}

func TestClaimLog_CountSince(t *testing.T) {
	_, q := openV2(t)
	l := NewClaimLog(q)
	ctx := context.Background()
	now := time.Now()
	for i, at := range []time.Time{now.Add(-25 * time.Hour), now.Add(-time.Hour), now} {
		require.NoError(t, l.Record(ctx, ClaimEntry{AccountID: "a1", Platform: "twitch", BenefitID: string(rune('a' + i)), Source: ClaimSourceGrubDrops, ClaimedAt: at}))
	}
	n, err := l.CountSince(ctx, now.Add(-2*time.Hour))
	require.NoError(t, err)
	require.Equal(t, 2, n)
}

func TestDropStates_ReplaceIsPerAccount(t *testing.T) {
	db, q := openV2(t)
	seedAccount(t, q, "a1")
	seedAccount(t, q, "a2")
	s := NewDropStates(db, q)
	ctx := context.Background()
	now := time.Now()
	row := func(acc, ben string) DropState {
		return DropState{AccountID: acc, Platform: "twitch", Game: "G", CampaignID: "c", BenefitID: ben, State: "eligible", UpdatedAt: now}
	}
	require.NoError(t, s.Replace(ctx, "a1", []DropState{row("a1", "b1"), row("a1", "b2")}))
	require.NoError(t, s.Replace(ctx, "a2", []DropState{row("a2", "b1")}))
	require.NoError(t, s.Replace(ctx, "a1", []DropState{row("a1", "b3")}))
	a1, err := s.List(ctx, "a1")
	require.NoError(t, err)
	require.Len(t, a1, 1)
	require.Equal(t, "b3", a1[0].BenefitID)
	a2, err := s.List(ctx, "a2")
	require.NoError(t, err)
	require.Len(t, a2, 1)
	all, err := s.ListAll(ctx)
	require.NoError(t, err)
	require.Len(t, all, 2)
}

func TestDropStates_RoundTrip(t *testing.T) {
	db, q := openV2(t)
	seedAccount(t, q, "a1")
	s := NewDropStates(db, q)
	ctx := context.Background()
	in := DropState{
		AccountID: "a1", Platform: "kick", Game: "Game", CampaignID: "c1", CampaignName: "Camp",
		BenefitID: "b1", BenefitName: "Ben", ImageURL: "http://img", State: "blocked", Reason: "not_linked",
		Minutes: 12, RequiredMinutes: 60,
		EndsAt: time.Unix(1_800_000_000, 0), UpdatedAt: time.Unix(1_700_000_000, 0),
	}
	require.NoError(t, s.Upsert(ctx, in))
	got, err := s.List(ctx, "a1")
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, in.EndsAt.Unix(), got[0].EndsAt.Unix())
	require.Equal(t, in.UpdatedAt.Unix(), got[0].UpdatedAt.Unix())
	in.EndsAt, in.UpdatedAt = got[0].EndsAt, got[0].UpdatedAt
	require.Equal(t, in, got[0])
}

func TestWatchMinutes_TotalAndDay(t *testing.T) {
	_, q := openV2(t)
	w := NewWatchMinutes(q)
	ctx := context.Background()
	require.NoError(t, w.Add(ctx, "a1", "2026-10-25", 3))
	require.NoError(t, w.Add(ctx, "a2", "2026-10-25", 2))
	require.NoError(t, w.Add(ctx, "a1", "", 10))
	require.NoError(t, w.Add(ctx, "a1", "2026-10-24", 1))
	require.NoError(t, w.Add(ctx, "a1", "2026-10-25", 0))
	total, err := w.Total(ctx)
	require.NoError(t, err)
	require.Equal(t, 16, total)
	day, err := w.Day(ctx, "2026-10-25")
	require.NoError(t, err)
	require.Equal(t, 5, day)
}

func TestAccountScans_ZeroKeepsReconciled(t *testing.T) {
	_, q := openV2(t)
	seedAccount(t, q, "a1")
	a := NewAccountScans(q)
	ctx := context.Background()
	t1 := time.Unix(1_700_000_000, 0)
	t2 := t1.Add(time.Hour)
	require.NoError(t, a.Put(ctx, AccountScan{AccountID: "a1", ScannedAt: t1, ReconciledAt: t1}))
	require.NoError(t, a.Put(ctx, AccountScan{AccountID: "a1", ScannedAt: t2, Error: "boom"}))
	got, err := a.List(ctx)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, t2.Unix(), got[0].ScannedAt.Unix())
	require.Equal(t, t1.Unix(), got[0].ReconciledAt.Unix())
	require.Equal(t, "boom", got[0].Error)

	require.NoError(t, a.Put(ctx, AccountScan{AccountID: "a1"}))
	got, err = a.List(ctx)
	require.NoError(t, err)
	require.Equal(t, t2.Unix(), got[0].ScannedAt.Unix())
	require.Equal(t, "", got[0].Error)
}

func TestChosenGames_SetReplacesInOrder(t *testing.T) {
	db, q := openV2(t)
	ctx := context.Background()
	for _, g := range []string{"g1", "g2"} {
		require.NoError(t, q.UpsertGame(ctx, gen.UpsertGameParams{ID: g, Name: "Name " + g, Slug: "slug-" + g, Priority: 100}))
	}
	c := NewChosenGames(db, q)
	require.NoError(t, c.Set(ctx, []string{"g2", "g1"}))
	got, err := c.List(ctx)
	require.NoError(t, err)
	require.Equal(t, []ChosenGame{{ID: "g2", Name: "Name g2", Slug: "slug-g2"}, {ID: "g1", Name: "Name g1", Slug: "slug-g1"}}, got)

	require.Error(t, c.Set(ctx, []string{"g1", "nope"}))
	got, err = c.List(ctx)
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, "g2", got[0].ID)
	require.Equal(t, "g1", got[1].ID)
}

func TestSkips_SameKeysAs1x(t *testing.T) {
	_, q := openV2(t)
	s := NewSkips(q)
	require.NoError(t, s.RecordSkip("a1", "b1"))
	v, err := q.GetSettingString(context.Background(), "skip_override:b1:a1")
	require.NoError(t, err)
	require.Equal(t, "1", string(v))
	got, err := s.Skips("a1")
	require.NoError(t, err)
	require.Equal(t, map[string]bool{"b1": true}, got)
	other, err := s.Skips("a2")
	require.NoError(t, err)
	require.Empty(t, other)
	require.NoError(t, s.ClearSkip("a1", "b1"))
	got, err = s.Skips("a1")
	require.NoError(t, err)
	require.Empty(t, got)
}
