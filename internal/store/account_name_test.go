package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

func TestAccountName(t *testing.T) {
	assert.Equal(t, "Phluses", AccountName(gen.Account{DisplayName: "Phluses"}))
	assert.Equal(t, "Main", AccountName(gen.Account{DisplayName: "Phluses", CustomName: "Main"}))
}

func TestUpdateAccountCustomName(t *testing.T) {
	q := gen.New(openTest(t))
	ctx := context.Background()
	now := time.Now().Unix()
	_, err := q.CreateAccount(ctx, gen.CreateAccountParams{
		ID: "a1", Platform: "kick", DisplayName: "Phluses",
		Status: "idle", FingerprintJson: "{}", Enabled: 1, CreatedAt: now, UpdatedAt: now,
	})
	require.NoError(t, err)

	a, err := q.GetAccount(ctx, "a1")
	require.NoError(t, err)
	assert.Equal(t, "", a.CustomName)

	require.NoError(t, q.UpdateAccountCustomName(ctx, gen.UpdateAccountCustomNameParams{CustomName: "Kick main", UpdatedAt: now, ID: "a1"}))
	// A platform-side name refresh leaves the custom name alone.
	require.NoError(t, q.UpdateAccountDisplayName(ctx, gen.UpdateAccountDisplayNameParams{DisplayName: "phluses2", UpdatedAt: now, ID: "a1"}))
	all, err := q.ListAllAccounts(ctx)
	require.NoError(t, err)
	require.Len(t, all, 1)
	assert.Equal(t, "Kick main", all[0].CustomName)
	assert.Equal(t, "phluses2", all[0].DisplayName)
	assert.Equal(t, "Kick main", AccountName(all[0]))
}
