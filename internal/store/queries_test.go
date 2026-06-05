package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/dropsminer/internal/store/gen"
)

func openTest(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "t.db")
	db, err := Open(context.Background(), path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// TestQueries_CountClaimedForCampaign exercises the new aggregate the
// dashboard uses to fill the Active Campaigns "Claimed X / Y" badge.
// Two accounts each claim the same benefit (must dedupe to one) plus
// one account claims a second benefit — total = 2 distinct benefits.
func TestQueries_CountClaimedForCampaign(t *testing.T) {
	db := openTest(t)
	q := gen.New(db)
	ctx := context.Background()
	now := time.Now().Unix()

	mkAcc := func(id, login string) {
		_, err := q.CreateAccount(ctx, gen.CreateAccountParams{
			ID: id, Platform: "twitch", Login: login, DisplayName: login,
			Status: "idle", FingerprintJson: "{}", Enabled: 1,
			CreatedAt: now, UpdatedAt: now,
		})
		require.NoError(t, err)
	}
	mkAcc("acc-a", "a")
	mkAcc("acc-b", "b")

	require.NoError(t, q.UpsertCampaign(ctx, gen.UpsertCampaignParams{
		ID: "camp-1", Platform: "twitch", Game: "Rust", Name: "Camp One",
		StartsAt: now - 3600, EndsAt: now + 3600, Status: "active",
		RawJson: "{}", DiscoveredAt: now,
	}))
	require.NoError(t, q.UpsertCampaign(ctx, gen.UpsertCampaignParams{
		ID: "camp-2", Platform: "twitch", Game: "Rust", Name: "Camp Two",
		StartsAt: now - 3600, EndsAt: now + 3600, Status: "active",
		RawJson: "{}", DiscoveredAt: now,
	}))
	mkBenefit := func(id, campID string) {
		require.NoError(t, q.UpsertBenefit(ctx, gen.UpsertBenefitParams{
			ID: id, CampaignID: campID, Name: id, RequiredMinutes: 60,
		}))
	}
	mkBenefit("b1", "camp-1")
	mkBenefit("b2", "camp-1")
	mkBenefit("b3", "camp-1")
	mkBenefit("b4", "camp-2") // belongs to a different campaign, must not leak

	insertClaim := func(id, acc, ben string) {
		require.NoError(t, q.InsertClaim(ctx, gen.InsertClaimParams{
			ID: id, AccountID: acc, BenefitID: ben, ClaimedAt: now, ValueMetaJson: "{}",
		}))
	}
	// acc-a + acc-b both claim b1 — must collapse to a single distinct benefit.
	insertClaim("clm-1", "acc-a", "b1")
	insertClaim("clm-2", "acc-b", "b1")
	// acc-a also claims b2 — adds one more distinct benefit.
	insertClaim("clm-3", "acc-a", "b2")
	// b4 belongs to camp-2; must not bump camp-1's count.
	insertClaim("clm-4", "acc-a", "b4")

	got, err := q.CountClaimedForCampaign(ctx, "camp-1")
	require.NoError(t, err)
	assert.Equal(t, int64(2), got, "two distinct benefits claimed in camp-1")

	got, err = q.CountClaimedForCampaign(ctx, "camp-2")
	require.NoError(t, err)
	assert.Equal(t, int64(1), got, "only b4 claimed in camp-2")

	got, err = q.CountClaimedForCampaign(ctx, "camp-empty")
	require.NoError(t, err)
	assert.Equal(t, int64(0), got, "no claims for unknown campaign")
}

func TestQueries_AccountRoundtrip(t *testing.T) {
	db := openTest(t)
	q := gen.New(db)
	now := time.Now().Unix()

	acc, err := q.CreateAccount(context.Background(), gen.CreateAccountParams{
		ID:              "acc1",
		Platform:        "twitch",
		Login:           "user1",
		DisplayName:     "User One",
		Status:          "idle",
		FingerprintJson: "{}",
		Enabled:         1,
		CreatedAt:       now,
		UpdatedAt:       now,
	})
	require.NoError(t, err)
	assert.Equal(t, "acc1", acc.ID)

	list, err := q.ListEnabledAccounts(context.Background())
	require.NoError(t, err)
	found := false
	for _, a := range list {
		if a.ID == "acc1" {
			found = true
			break
		}
	}
	assert.True(t, found, "acc1 not present in ListEnabledAccounts result")
}
