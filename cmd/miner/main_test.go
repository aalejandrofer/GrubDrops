package main

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/api"
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

func TestPipelineModeFor(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, t.TempDir()+"/t.db")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	q := gen.New(db)

	t.Setenv("GRUB_PIPELINE", "")
	assert.Equal(t, "v1", pipelineModeFor(ctx, q, "acc"))
	t.Setenv("GRUB_PIPELINE", "v2")
	assert.Equal(t, "v2", pipelineModeFor(ctx, q, "acc"))
	require.NoError(t, q.UpsertSettingString(ctx, gen.UpsertSettingStringParams{Key: store.PipelineOverridePrefix + "acc", Value: []byte("v1")}))
	assert.Equal(t, "v1", pipelineModeFor(ctx, q, "acc"), "per-account override beats env")
	require.NoError(t, q.UpsertSettingString(ctx, gen.UpsertSettingStringParams{Key: store.PipelineOverridePrefix + "acc", Value: []byte("junk")}))
	assert.Equal(t, "v2", pipelineModeFor(ctx, q, "acc"), "invalid override falls through")
}

func TestMatchAnyChannel(t *testing.T) {
	assert.Nil(t, matchAnyChannel(nil))
	m := matchAnyChannel([]string{"Fav"})
	assert.True(t, m([]string{"x", "fav"}))
	assert.False(t, m([]string{"x"}))
}

// TestForceWatchEnabled proves the extracted helper reads the same
// force_watch:<accountID> KV flag forceWatchStore.Next checks, so v1 and v2
// agree on whether force-watch is on for an account.
func TestForceWatchEnabled(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, t.TempDir()+"/t.db")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	q := gen.New(db)

	assert.False(t, forceWatchEnabled(ctx, q, "acc-1"), "flag unset -> disabled")

	require.NoError(t, q.UpsertSettingString(ctx, gen.UpsertSettingStringParams{
		Key: api.ForceWatchEnabledKey("acc-1"), Value: []byte("1"),
	}))
	assert.True(t, forceWatchEnabled(ctx, q, "acc-1"), "flag=1 -> enabled")

	assert.False(t, forceWatchEnabled(ctx, q, "acc-2"), "different account unaffected")
}

// TestV2ForceChannels proves the v2 wiring only surfaces ListForceChannels
// rows when the account's force-watch flag is enabled — matching v1's
// forceWatchStore.Next gate (previously v2 passed the list unconditionally).
func TestV2ForceChannels(t *testing.T) {
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
	require.NoError(t, q.AddForceChannel(ctx, gen.AddForceChannelParams{
		AccountID: "acc-1", Channel: "somechannel", Rank: 0, CreatedAt: now,
	}))

	assert.Empty(t, v2ForceChannels(ctx, q, "acc-1"), "flag off -> no force channels even though rows exist")

	require.NoError(t, q.UpsertSettingString(ctx, gen.UpsertSettingStringParams{
		Key: api.ForceWatchEnabledKey("acc-1"), Value: []byte("1"),
	}))
	assert.Equal(t, []string{"somechannel"}, v2ForceChannels(ctx, q, "acc-1"), "flag on -> rows surfaced")
}
