package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

func TestSessionStore_RoundTrip(t *testing.T) {
	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "s.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	q := gen.New(db)
	_, err = q.CreateAccount(context.Background(), gen.CreateAccountParams{
		ID: "acc1", Platform: "twitch", DisplayName: "demo",
		Status: "idle", FingerprintJson: "{}", Enabled: 1,
		CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix(),
	})
	require.NoError(t, err)

	c, err := NewCryptor(testKey)
	require.NoError(t, err)
	ss := NewSessionStore(db, q, c)

	in := platform.Session{
		AccessToken:  "secret",
		RefreshToken: "ref",
		ExpiresAt:    time.Now().Add(time.Hour).UTC().Truncate(time.Second),
	}
	require.NoError(t, ss.Put(context.Background(), "acc1", in))

	out, ok, err := ss.Get(context.Background(), "acc1")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "secret", out.AccessToken)
	assert.Equal(t, "ref", out.RefreshToken)
	assert.True(t, out.ExpiresAt.Equal(in.ExpiresAt))
}

func TestSessionStore_MissingReturnsFalse(t *testing.T) {
	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "s.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	q := gen.New(db)
	c, err := NewCryptor(testKey)
	require.NoError(t, err)
	ss := NewSessionStore(db, q, c)

	_, ok, err := ss.Get(context.Background(), "missing")
	require.NoError(t, err)
	assert.False(t, ok)
}

// TestSessionStore_ClientIDRoundTrip: the Twitch client a token was minted
// by must survive the encrypted blob round-trip (a TV token replayed under
// the Android Client-Id is rejected), and legacy blobs written before the
// field existed must decode to "" (the Android profile) so irreplaceable
// legacy sessions keep their behavior.
func TestSessionStore_ClientIDRoundTrip(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "s.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	q := gen.New(db)
	for _, id := range []string{"acc_tv", "acc_legacy"} {
		_, err = q.CreateAccount(ctx, gen.CreateAccountParams{
			ID: id, Platform: "twitch", DisplayName: id,
			Status: "idle", FingerprintJson: "{}", Enabled: 1,
			CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix(),
		})
		require.NoError(t, err)
	}
	c, err := NewCryptor(testKey)
	require.NoError(t, err)
	ss := NewSessionStore(db, q, c)

	require.NoError(t, ss.Put(ctx, "acc_tv", platform.Session{
		AccessToken: "tv-tok", ClientID: "tv",
		ExpiresAt: time.Now().Add(time.Hour).UTC().Truncate(time.Second),
	}))
	out, ok, err := ss.Get(ctx, "acc_tv")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "tv", out.ClientID)

	// A blob persisted before ClientID existed: no client_id key at all.
	legacy := []byte(`{"access_token":"old-tok","refresh_token":"old-ref"}`)
	ct, err := c.Encrypt(legacy)
	require.NoError(t, err)
	require.NoError(t, q.UpsertSession(ctx, gen.UpsertSessionParams{
		AccountID: "acc_legacy", Ciphertext: ct, ExpiresAt: time.Now().Add(time.Hour).Unix(),
	}))
	out, ok, err = ss.Get(ctx, "acc_legacy")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "", out.ClientID)
}
