package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/dropsminer/internal/platform"
	"github.com/aalejandrofer/dropsminer/internal/store/gen"
)

func TestCampaignPersister_UpsertsCampaignsAndBenefits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "drops.db")
	db, err := Open(context.Background(), path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	q := gen.New(db)
	p := NewCampaignPersister(q)

	now := time.Now()
	camps := []platform.Campaign{
		{
			ID: "camp-active", Platform: "twitch", Game: "Rust",
			Name: "Rust Active", Status: "active",
			StartsAt: now.Add(-time.Hour), EndsAt: now.Add(time.Hour),
			Benefits: []platform.DropBenefit{
				{ID: "drop1", CampaignID: "camp-active", Name: "Helmet", RequiredMinutes: 60},
			},
		},
		{
			ID: "camp-past", Platform: "twitch", Game: "Rust",
			Name: "Rust Past", Status: "expired",
			StartsAt: now.Add(-48 * time.Hour), EndsAt: now.Add(-24 * time.Hour),
		},
		{
			ID: "camp-up", Platform: "twitch", Game: "Rust",
			Name: "Rust Upcoming", Status: "upcoming",
			StartsAt: now.Add(24 * time.Hour), EndsAt: now.Add(48 * time.Hour),
		},
	}

	require.NoError(t, p.PersistCampaigns(context.Background(), camps))

	ctx := context.Background()
	nowUnix := now.Unix()

	past, err := q.ListPastCampaigns(ctx, gen.ListPastCampaignsParams{EndsAt: nowUnix, Limit: 10})
	require.NoError(t, err)
	require.Len(t, past, 1)
	assert.Equal(t, "camp-past", past[0].ID)

	cur, err := q.ListCurrentCampaigns(ctx, gen.ListCurrentCampaignsParams{StartsAt: nowUnix, EndsAt: nowUnix, Limit: 10})
	require.NoError(t, err)
	require.Len(t, cur, 1)
	assert.Equal(t, "camp-active", cur[0].ID)

	up, err := q.ListUpcomingCampaigns(ctx, gen.ListUpcomingCampaignsParams{StartsAt: nowUnix, Limit: 10})
	require.NoError(t, err)
	require.Len(t, up, 1)
	assert.Equal(t, "camp-up", up[0].ID)

	// Benefit was upserted under the active campaign.
	bens, err := q.ListBenefitsForCampaign(ctx, "camp-active")
	require.NoError(t, err)
	require.Len(t, bens, 1)
	assert.Equal(t, "Helmet", bens[0].Name)

	// Re-persisting the same set must be idempotent (no duplicate rows).
	require.NoError(t, p.PersistCampaigns(ctx, camps))
	cur2, err := q.ListCurrentCampaigns(ctx, gen.ListCurrentCampaignsParams{StartsAt: nowUnix, EndsAt: nowUnix, Limit: 10})
	require.NoError(t, err)
	require.Len(t, cur2, 1)
}

func TestCampaignPersister_NoopOnNilOrEmpty(t *testing.T) {
	var p *CampaignPersister
	require.NoError(t, p.PersistCampaigns(context.Background(), []platform.Campaign{{ID: "x"}}))

	path := filepath.Join(t.TempDir(), "drops.db")
	db, err := Open(context.Background(), path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	q := gen.New(db)
	require.NoError(t, NewCampaignPersister(q).PersistCampaigns(context.Background(), nil))
}
