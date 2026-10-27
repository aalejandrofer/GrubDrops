package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

func TestAccountGames_SetFlagAndList(t *testing.T) {
	db, q := openV2(t)
	ctx := context.Background()
	for _, g := range []string{"g1", "g2"} {
		require.NoError(t, q.UpsertGame(ctx, gen.UpsertGameParams{ID: g, Name: "Name " + g, Slug: "slug-" + g, Priority: 100}))
	}
	now := time.Now().Unix()
	_, err := q.CreateAccount(ctx, gen.CreateAccountParams{ID: "a1", Platform: "twitch", Status: "idle", FingerprintJson: "{}", Enabled: 1, CreatedAt: now, UpdatedAt: now})
	require.NoError(t, err)
	a, err := q.GetAccount(ctx, "a1")
	require.NoError(t, err)
	require.EqualValues(t, 0, a.OwnGames, "new accounts use the global list")

	ag := NewAccountGames(db, q)
	require.NoError(t, ag.Set(ctx, "a1", true, []string{"g2", "g1", "g2"}))
	got, err := ag.List(ctx, "a1")
	require.NoError(t, err)
	require.Equal(t, []ChosenGame{{ID: "g2", Name: "Name g2", Slug: "slug-g2"}, {ID: "g1", Name: "Name g1", Slug: "slug-g1"}}, got)
	a, _ = q.GetAccount(ctx, "a1")
	require.EqualValues(t, 1, a.OwnGames)

	// Off with nil keeps the list for the next time it is turned on.
	require.NoError(t, ag.Set(ctx, "a1", false, nil))
	a, _ = q.GetAccount(ctx, "a1")
	require.EqualValues(t, 0, a.OwnGames)
	got, _ = ag.List(ctx, "a1")
	require.Len(t, got, 2)

	// An unknown game fails and leaves flag and list alone.
	require.Error(t, ag.Set(ctx, "a1", true, []string{"nope"}))
	a, _ = q.GetAccount(ctx, "a1")
	require.EqualValues(t, 0, a.OwnGames)
	got, _ = ag.List(ctx, "a1")
	require.Len(t, got, 2)
}
