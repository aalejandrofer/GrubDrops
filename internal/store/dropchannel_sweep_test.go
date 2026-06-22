package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

func TestSweepStaleDropChannels(t *testing.T) {
	db := openTest(t)
	q := gen.New(db)
	ctx := context.Background()
	now := time.Now()
	nowUnix := now.Unix()

	_, err := q.CreateAccount(ctx, gen.CreateAccountParams{
		ID: "acc-1", Platform: "kick", DisplayName: "k",
		Status: "idle", FingerprintJson: "{}", Enabled: 1,
		CreatedAt: nowUnix, UpdatedAt: nowUnix,
	})
	require.NoError(t, err)

	// Two drop channels whitelisted: one for a live campaign, one stale.
	require.NoError(t, q.AddAccountChannel(ctx, gen.AddAccountChannelParams{AccountID: "acc-1", Channel: "livechan", Rank: 0}))
	require.NoError(t, q.AddAccountChannel(ctx, gen.AddAccountChannelParams{AccountID: "acc-1", Channel: "deadchan", Rank: 0}))

	// Persist one ACTIVE null-game campaign whose only channel is "livechan".
	p := NewCampaignPersister(q)
	require.NoError(t, p.PersistCampaigns(ctx, []platform.Campaign{{
		ID: "c-live", Platform: "kick", Game: "", Name: "Live Football",
		Status: "active", StartsAt: now.Add(-time.Hour), EndsAt: now.Add(time.Hour),
		AllowedChannels: []string{"livechan"},
	}}))

	removed, err := SweepStaleDropChannels(ctx, q, nowUnix)
	require.NoError(t, err)
	assert.Equal(t, 1, removed, "the stale channel should be removed")

	got, err := q.ListAccountChannels(ctx, "acc-1")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "livechan", got[0].Channel)
}

func TestSweepStaleDropChannels_SkipsWhenNoCampaigns(t *testing.T) {
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
	require.NoError(t, q.AddAccountChannel(ctx, gen.AddAccountChannelParams{AccountID: "acc-1", Channel: "keepme", Rank: 0}))

	// No campaigns persisted → sweep must be a no-op (don't wipe on empty).
	removed, err := SweepStaleDropChannels(ctx, q, now)
	require.NoError(t, err)
	assert.Equal(t, 0, removed)
	got, err := q.ListAccountChannels(ctx, "acc-1")
	require.NoError(t, err)
	require.Len(t, got, 1)
}
