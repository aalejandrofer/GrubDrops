package store

import (
	"context"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMigration0027PointsBalancesUpDown(t *testing.T) {
	ctx := context.Background()
	db := migratedTo(t, 27)
	_, err := db.ExecContext(ctx, `SELECT account_id, channel, balance, fetched_at, checked_at, error, failures FROM points_balances`)
	require.NoError(t, err)
	require.NoError(t, goose.DownToContext(ctx, db, "migrations", 26))
	_, err = db.ExecContext(ctx, `SELECT 1 FROM points_balances`)
	require.Error(t, err, "down drops the table")
}

func TestPointsBalances_RecordGetList(t *testing.T) {
	f, q, _ := forceFixture(t)
	ctx := context.Background()
	p := NewPointsBalances(q)
	require.NoError(t, f.Set(ctx, forceTwoChannels))

	_, ok, err := p.Get(ctx, "a1", "keshaeuw")
	require.NoError(t, err)
	assert.False(t, ok)

	t1 := time.Unix(1000, 0)
	require.NoError(t, p.RecordOK(ctx, "a1", "keshaeuw", 450, t1))
	got, ok, err := p.Get(ctx, "a1", "keshaeuw")
	require.NoError(t, err)
	require.True(t, ok)
	require.NotNil(t, got.Balance)
	assert.Equal(t, int64(450), *got.Balance)
	assert.Equal(t, t1, got.FetchedAt)
	assert.Equal(t, t1, got.CheckedAt)
	assert.Empty(t, got.Error)

	// A failure keeps the last good balance and fetched_at.
	t2 := t1.Add(time.Hour)
	require.NoError(t, p.RecordErr(ctx, "a1", "keshaeuw", PointsErrFailed, 2, t2))
	got, _, err = p.Get(ctx, "a1", "keshaeuw")
	require.NoError(t, err)
	assert.Equal(t, int64(450), *got.Balance)
	assert.Equal(t, t1, got.FetchedAt)
	assert.Equal(t, t2, got.CheckedAt)
	assert.Equal(t, PointsErrFailed, got.Error)
	assert.Equal(t, 2, got.Failures)

	// A success clears the error and the failure count.
	require.NoError(t, p.RecordOK(ctx, "a1", "keshaeuw", 500, t2.Add(time.Minute)))
	got, _, err = p.Get(ctx, "a1", "keshaeuw")
	require.NoError(t, err)
	assert.Equal(t, int64(500), *got.Balance)
	assert.Empty(t, got.Error)
	assert.Zero(t, got.Failures)

	// A first read with no balance stores no balance.
	require.NoError(t, p.RecordErr(ctx, "a3", "keshaeuw", PointsErrUnavailable, 0, t2))
	list, err := p.List(ctx)
	require.NoError(t, err)
	require.Len(t, list, 2)
	assert.Equal(t, "a3", list[1].AccountID)
	assert.Nil(t, list[1].Balance)
	assert.True(t, list[1].FetchedAt.IsZero())
}

func TestPointsBalances_PruneKeepsOnlyAssignedTwitchChannels(t *testing.T) {
	f, q, _ := forceFixture(t)
	ctx := context.Background()
	p := NewPointsBalances(q)
	require.NoError(t, f.Set(ctx, forceTwoChannels))
	now := time.Unix(1000, 0)
	for _, r := range [][2]string{{"a1", "keshaeuw"}, {"a3", "keshaeuw"}, {"a1", "gone"}, {"a2", "xqc"}} {
		require.NoError(t, p.RecordOK(ctx, r[0], r[1], 1, now))
	}
	n, err := p.Prune(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(2), n, "unassigned channel and the Kick row go")

	// Unassigning a3 removes its row on the next prune.
	require.NoError(t, f.Set(ctx, []ForceChannel{{Platform: "twitch", Channel: "keshaeuw", Accounts: []string{"a1"}}}))
	_, err = p.Prune(ctx)
	require.NoError(t, err)
	list, err := p.List(ctx)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "a1", list[0].AccountID)

	// Deleting the account cascades.
	require.NoError(t, q.DeleteAccount(ctx, "a1"))
	list, err = p.List(ctx)
	require.NoError(t, err)
	assert.Empty(t, list)
}
