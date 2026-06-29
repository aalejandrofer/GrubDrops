package api

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/store"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

// collectTestKey is an age secret key used only in tests.
const collectTestKey = "AGE-SECRET-KEY-1DZCAXYWJM6M42NSX5GR4QWZZ2JXEYKJ9ZKWYFYSNU997775JJ6XSY85FK9"

func platformSessionFixture() platform.Session {
	return platform.Session{AccessToken: "tok"}
}

// TestSessionForPlatform_FallsBackToDisabledAccount proves a drop collected on
// a now-disabled account is still serviceable: sessionForPlatform must return a
// session from a DISABLED account when no enabled account on the platform has
// one, so lazyFetchBenefits can populate the items panel.
func TestSessionForPlatform_FallsBackToDisabledAccount(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	q := gen.New(db)

	c, err := store.NewCryptor(collectTestKey)
	require.NoError(t, err)
	sessions := store.NewSessionStore(db, q, c)

	now := time.Now().Unix()
	_, err = q.CreateAccount(ctx, gen.CreateAccountParams{
		ID: "acc-off", Platform: "twitch", DisplayName: "Phluses",
		Status: "idle", FingerprintJson: "{}", Enabled: 0, // DISABLED
		CreatedAt: now, UpdatedAt: now,
	})
	require.NoError(t, err)
	// Give the disabled account a stored session.
	require.NoError(t, sessions.Put(ctx, "acc-off", platformSessionFixture()))

	d := &dropsDeps{q: q, sessions: sessions}
	sess, ok := d.sessionForPlatform(ctx, "twitch")
	require.True(t, ok, "must fall back to the disabled account's session")
	require.Equal(t, "acc-off", sess.AccountID)
}
