package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chano-fernandez/rust-drops-miner/internal/platform"
	"github.com/chano-fernandez/rust-drops-miner/internal/store/gen"
)

func TestSessionStore_RoundTrip(t *testing.T) {
	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "s.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	q := gen.New(db)
	_, err = q.CreateAccount(context.Background(), gen.CreateAccountParams{
		ID: "acc1", Platform: "twitch", Login: "demo", DisplayName: "demo",
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
