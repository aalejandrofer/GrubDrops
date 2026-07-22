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

// TestGenerateMasterKey_IsAcceptedByCryptor proves the `keygen` subcommand
// emits a key the store actually accepts — guarding against the old README
// guidance (head -c32 /dev/urandom | base64), which produced a blob that
// failed age.ParseX25519Identity and crashed the miner at startup.
func TestGenerateMasterKey_IsAcceptedByCryptor(t *testing.T) {
	key, err := generateMasterKey()
	require.NoError(t, err)
	require.NotEmpty(t, key)
	assert.Contains(t, key, "AGE-SECRET-KEY-1", "must be an age X25519 identity")

	// The real gate: store.NewCryptor parses it without error.
	_, err = store.NewCryptor(key)
	require.NoError(t, err, "generated key must be accepted by the session store")

	// Two calls yield distinct keys.
	key2, err := generateMasterKey()
	require.NoError(t, err)
	assert.NotEqual(t, key, key2, "each keygen must be unique")
}
