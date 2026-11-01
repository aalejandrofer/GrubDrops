package store

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

func TestMigration0028RewardCodesUpDown(t *testing.T) {
	ctx := context.Background()
	db := migratedTo(t, 28)
	_, err := db.ExecContext(ctx, `SELECT id, account_id, reward_key, name, game, code, redeem_url, expires_at,
		received_at, first_seen, notified_at, reminded_at, redeemed_at, dismissed_at FROM reward_codes`)
	require.NoError(t, err)
	require.NoError(t, goose.DownToContext(ctx, db, "migrations", 27))
	_, err = db.ExecContext(ctx, `SELECT 1 FROM reward_codes`)
	require.Error(t, err, "down drops the table")
}

func TestMigration0029RewardCodesNotifySkippedUpDown(t *testing.T) {
	ctx := context.Background()
	db := migratedTo(t, 29)
	_, err := db.ExecContext(ctx, `SELECT notify_skipped_at FROM reward_codes`)
	require.NoError(t, err)
	require.NoError(t, goose.DownToContext(ctx, db, "migrations", 28))
	_, err = db.ExecContext(ctx, `SELECT notify_skipped_at FROM reward_codes`)
	require.Error(t, err, "down drops the column")
	_, err = db.ExecContext(ctx, `SELECT notified_at FROM reward_codes`)
	require.NoError(t, err, "down keeps the table")
}

func rewardCodesFixture(t *testing.T) *RewardCodes {
	t.Helper()
	_, q := openV2(t)
	seedPlatformAccount(t, q, "a1", "twitch")
	seedPlatformAccount(t, q, "a2", "twitch")
	c, err := NewCryptor(testKey)
	require.NoError(t, err)
	return NewRewardCodes(q, c)
}

func TestRewardCodes_SaveRoundTripEncrypted(t *testing.T) {
	s := rewardCodesFixture(t)
	ctx := context.Background()
	now := time.Unix(1_800_000_000, 0)
	exp := now.Add(72 * time.Hour)
	rc := platform.RewardCode{
		Key: "n1", Name: "Blue Creeper Cape", Game: "Minecraft", Code: "AB12-CD34-EF56",
		RedeemURL: "https://www.twitch.tv/drops/inventory", ExpiresAt: exp, ReceivedAt: now.Add(-time.Hour),
	}
	got, isNew, err := s.Save(ctx, "a1", rc, now)
	require.NoError(t, err)
	assert.True(t, isNew)
	assert.Positive(t, got.ID)
	assert.Equal(t, "AB12-CD34-EF56", got.Code)
	assert.Equal(t, exp, got.ExpiresAt)
	assert.Equal(t, now, got.FirstSeen)
	assert.True(t, got.Open())
	assert.True(t, got.NotifiedAt.IsZero())

	// At rest the code is ciphertext, never the plain text.
	row, err := s.Q.GetRewardCode(ctx, got.ID)
	require.NoError(t, err)
	require.NotEmpty(t, row.Code)
	assert.False(t, bytes.Contains(row.Code, []byte("AB12-CD34-EF56")), "code stored encrypted")

	// A later read without code or expiry keeps both; first_seen stays.
	again, isNew, err := s.Save(ctx, "a1", platform.RewardCode{Key: "n1", Name: "Blue Creeper Cape"}, now.Add(time.Hour))
	require.NoError(t, err)
	assert.False(t, isNew)
	assert.Equal(t, got.ID, again.ID)
	assert.Equal(t, "AB12-CD34-EF56", again.Code)
	assert.Equal(t, exp, again.ExpiresAt)
	assert.Equal(t, "https://www.twitch.tv/drops/inventory", again.RedeemURL)
	assert.Equal(t, now, again.FirstSeen)

	// The same key on another account is another reward.
	_, isNew, err = s.Save(ctx, "a2", rc, now)
	require.NoError(t, err)
	assert.True(t, isNew)

	_, _, err = s.Save(ctx, "a1", platform.RewardCode{Name: "x"}, now)
	assert.Error(t, err, "empty key refused")
}

func TestRewardCodes_FlagsAndList(t *testing.T) {
	s := rewardCodesFixture(t)
	ctx := context.Background()
	now := time.Unix(1_800_000_000, 0)
	a, _, err := s.Save(ctx, "a1", platform.RewardCode{Key: "k1", Name: "One", Code: "AAAA-BBBB-CCCC"}, now)
	require.NoError(t, err)
	b, _, err := s.Save(ctx, "a1", platform.RewardCode{Key: "k2", Name: "Two"}, now.Add(time.Minute))
	require.NoError(t, err)
	assert.Empty(t, b.Code, "no code stays empty")

	require.NoError(t, s.MarkNotified(ctx, a.ID, now.Add(time.Second)))
	require.NoError(t, s.MarkReminded(ctx, a.ID, now.Add(2*time.Second)))
	got, ok, err := s.Get(ctx, a.ID)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, now.Add(time.Second), got.NotifiedAt)
	assert.Equal(t, now.Add(2*time.Second), got.RemindedAt)
	assert.True(t, got.NotifySkippedAt.IsZero())

	require.NoError(t, s.MarkNotifySkipped(ctx, b.ID, now.Add(3*time.Second)))
	got, ok, err = s.Get(ctx, b.ID)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, now.Add(3*time.Second), got.NotifySkippedAt)
	assert.True(t, got.NotifiedAt.IsZero())

	ok, err = s.SetRedeemed(ctx, a.ID, now)
	require.NoError(t, err)
	assert.True(t, ok)
	ok, err = s.SetDismissed(ctx, 9999, now)
	require.NoError(t, err)
	assert.False(t, ok, "unknown id")

	open, err := s.List(ctx, true)
	require.NoError(t, err)
	require.Len(t, open, 1)
	assert.Equal(t, "k2", open[0].Key)
	all, err := s.List(ctx, false)
	require.NoError(t, err)
	require.Len(t, all, 2)
	assert.Equal(t, "k2", all[0].Key, "newest first")
	assert.False(t, all[1].Open())

	_, ok, err = s.Get(ctx, 9999)
	require.NoError(t, err)
	assert.False(t, ok)
}
