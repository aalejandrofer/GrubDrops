package main

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/store"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

func TestLoadAccountChannels(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, t.TempDir()+"/t.db")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	q := gen.New(db)
	now := time.Now().Unix()

	_, err = q.CreateAccount(ctx, gen.CreateAccountParams{
		ID: "acc-1", Platform: "kick", DisplayName: "k",
		Status: "idle", FingerprintJson: "{}", Enabled: 1,
		CreatedAt: now, UpdatedAt: now,
	})
	require.NoError(t, err)

	// No channels yet -> nil closure.
	allow, err := loadAccountChannels(ctx, q, "acc-1")
	require.NoError(t, err)
	assert.Nil(t, allow)

	require.NoError(t, q.AddAccountChannel(ctx, gen.AddAccountChannelParams{
		AccountID: "acc-1", Channel: "adrianozendejas32", Rank: 0,
	}))

	allow, err = loadAccountChannels(ctx, q, "acc-1")
	require.NoError(t, err)
	require.NotNil(t, allow)
	// Case-insensitive match against a campaign's AllowedChannels.
	assert.True(t, allow([]string{"Adrianozendejas32"}))
	assert.False(t, allow([]string{"xqc"}))
	assert.False(t, allow(nil))
}
