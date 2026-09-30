package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/pipeline/dropstate"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

func seedAccount(t *testing.T, q *gen.Queries, id, platform string) {
	t.Helper()
	now := time.Now().Unix()
	_, err := q.CreateAccount(context.Background(), gen.CreateAccountParams{
		ID: id, Platform: platform, DisplayName: id,
		Status: "idle", FingerprintJson: "{}", Enabled: 1,
		CreatedAt: now, UpdatedAt: now,
	})
	require.NoError(t, err)
}

func TestDropStateStore_RoundTrip(t *testing.T) {
	db := openTest(t)
	q := gen.New(db)
	ctx := context.Background()
	seedAccount(t, q, "acc-1", "twitch")
	s := NewDropStateStore(q)

	at := time.Unix(1_700_000_000, 0)
	in := dropstate.Row{
		AccountID: "acc-1", DropID: "d1", CampaignID: "c1", Platform: "twitch",
		Status: dropstate.Blocked, Reason: dropstate.ClaimFailed,
		Minutes: 60, Required: 60, Source: dropstate.FromPlatform, FailCount: 5,
		RetryAfter: at.Add(time.Hour), SyncedAt: at, UpdatedAt: at,
	}
	require.NoError(t, s.Upsert(ctx, in))
	got, err := s.List(ctx, "acc-1")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, in, got[0])

	in.Status, in.Reason, in.RetryAfter = dropstate.Claimed, dropstate.NoReason, time.Time{}
	require.NoError(t, s.Upsert(ctx, in))
	got, err = s.List(ctx, "acc-1")
	require.NoError(t, err)
	require.Len(t, got, 1, "upsert must not duplicate")
	assert.Equal(t, dropstate.Claimed, got[0].Status)
	assert.True(t, got[0].RetryAfter.IsZero(), "0 maps back to zero time")
}

func TestDropStateStore_StreamerPrioritySeededFromAccountChannels(t *testing.T) {
	db := openTest(t)
	q := gen.New(db)
	ctx := context.Background()
	seedAccount(t, q, "acc-1", "kick")
	s := NewDropStateStore(q)

	// Migration already ran; insert straight into the new table like the
	// seed does, then read ordered by rank.
	_, err := db.ExecContext(ctx, `INSERT INTO account_streamer_priority (account_id, platform, login, rank) VALUES
		('acc-1','kick','zeta',1), ('acc-1','kick','alpha',0)`)
	require.NoError(t, err)
	got, err := s.StreamerPriority(ctx, "acc-1")
	require.NoError(t, err)
	assert.Equal(t, []string{"alpha", "zeta"}, got)
}

func TestMigration0016_SeedCopiesAccountChannels(t *testing.T) {
	db := openTest(t)
	q := gen.New(db)
	ctx := context.Background()
	seedAccount(t, q, "acc-1", "kick")
	require.NoError(t, q.AddAccountChannel(ctx, gen.AddAccountChannelParams{AccountID: "acc-1", Channel: "oilrats", Rank: 0}))
	// Re-run the seed statement from 0016 exactly.
	_, err := db.ExecContext(ctx, `INSERT INTO account_streamer_priority (account_id, platform, login, rank)
		SELECT ac.account_id, a.platform, ac.channel, ac.rank FROM account_channels ac JOIN accounts a ON a.id = ac.account_id`)
	require.NoError(t, err)
	got, err := NewDropStateStore(q).StreamerPriority(ctx, "acc-1")
	require.NoError(t, err)
	assert.Equal(t, []string{"oilrats"}, got)
}
