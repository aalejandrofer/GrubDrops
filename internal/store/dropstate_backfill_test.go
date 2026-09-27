package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/pipeline/dropstate"
	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

func TestBackfillDropState(t *testing.T) {
	db := openTest(t)
	q := gen.New(db)
	ctx := context.Background()
	seedAccount(t, q, "acc-1", "twitch")
	require.NoError(t, NewCampaignPersister(q).PersistCampaigns(ctx, []platform.Campaign{{
		ID: "c1", Platform: "twitch", Game: "G", Name: "C", Status: "active",
		Benefits: []platform.DropBenefit{
			{ID: "claimed1", CampaignID: "c1", Name: "A", RequiredMinutes: 60},
			{ID: "marked1", CampaignID: "c1", Name: "B", RequiredMinutes: 60},
			{ID: "skipped1", CampaignID: "c1", Name: "C", RequiredMinutes: 60},
			{ID: "both1", CampaignID: "c1", Name: "D", RequiredMinutes: 60},
		},
	}}))
	rec := NewClaimRecorder(q)
	require.NoError(t, rec.RecordClaim(ctx, "acc-1", platform.DropBenefit{ID: "claimed1"}))
	require.NoError(t, rec.RecordClaim(ctx, "acc-1", platform.DropBenefit{ID: "both1"}))
	for _, k := range []string{
		CollectOverridePrefix + "marked1:acc-1",
		SkipOverridePrefix + "skipped1:acc-1",
		SkipOverridePrefix + "both1:acc-1", // claim history must win over a skip
		SkipOverridePrefix + "gone:acc-1",  // benefit row missing: ignored
	} {
		require.NoError(t, q.UpsertSettingString(ctx, gen.UpsertSettingStringParams{Key: k, Value: []byte("1")}))
	}

	now := time.Unix(1_700_000_000, 0)
	n, err := BackfillDropState(ctx, q, now)
	require.NoError(t, err)
	assert.Equal(t, 4, n)

	rows, err := NewDropStateStore(q).List(ctx, "acc-1")
	require.NoError(t, err)
	by := map[string]dropstate.Row{}
	for _, r := range rows {
		by[r.DropID] = r
	}
	assert.Equal(t, dropstate.Claimed, by["claimed1"].Status)
	assert.Equal(t, dropstate.FromPlatform, by["claimed1"].Source)
	assert.Equal(t, dropstate.Claimed, by["marked1"].Status)
	assert.Equal(t, dropstate.FromUser, by["marked1"].Source)
	assert.Equal(t, dropstate.Blocked, by["skipped1"].Status)
	assert.Equal(t, dropstate.NotEnrolled, by["skipped1"].Reason)
	assert.Equal(t, now, by["skipped1"].RetryAfter, "skips get one fresh evaluation")
	assert.Equal(t, dropstate.Claimed, by["both1"].Status)
	assert.Equal(t, "c1", by["claimed1"].CampaignID)

	// Idempotent: second run is a no-op.
	n, err = BackfillDropState(ctx, q, now)
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}
