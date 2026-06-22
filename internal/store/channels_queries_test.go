package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

func TestQueries_AccountChannels(t *testing.T) {
	db := openTest(t)
	q := gen.New(db)
	ctx := context.Background()
	now := time.Now().Unix()

	_, err := q.CreateAccount(ctx, gen.CreateAccountParams{
		ID: "acc-1", Platform: "kick", DisplayName: "k",
		Status: "idle", FingerprintJson: "{}", Enabled: 1,
		CreatedAt: now, UpdatedAt: now,
	})
	require.NoError(t, err)

	require.NoError(t, q.AddAccountChannel(ctx, gen.AddAccountChannelParams{
		AccountID: "acc-1", Channel: "adrianozendejas32", Rank: 0,
	}))
	require.NoError(t, q.AddAccountChannel(ctx, gen.AddAccountChannelParams{
		AccountID: "acc-1", Channel: "xqc", Rank: 0,
	}))
	// Upsert same channel must not duplicate (PK conflict path).
	require.NoError(t, q.AddAccountChannel(ctx, gen.AddAccountChannelParams{
		AccountID: "acc-1", Channel: "xqc", Rank: 0,
	}))

	rows, err := q.ListAccountChannels(ctx, "acc-1")
	require.NoError(t, err)
	got := []string{}
	for _, r := range rows {
		got = append(got, r.Channel)
	}
	assert.ElementsMatch(t, []string{"adrianozendejas32", "xqc"}, got)

	require.NoError(t, q.RemoveAccountChannel(ctx, gen.RemoveAccountChannelParams{
		AccountID: "acc-1", Channel: "xqc",
	}))
	rows, err = q.ListAccountChannels(ctx, "acc-1")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "adrianozendejas32", rows[0].Channel)

	require.NoError(t, q.ClearAccountChannels(ctx, "acc-1"))
	rows, err = q.ListAccountChannels(ctx, "acc-1")
	require.NoError(t, err)
	assert.Empty(t, rows)
}
