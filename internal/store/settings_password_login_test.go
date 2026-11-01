package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

func TestSettings_PasswordLoginDisabled(t *testing.T) {
	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "t.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	s := NewSettings(gen.New(db))
	ctx := context.Background()

	off, err := s.PasswordLoginDisabled(ctx)
	require.NoError(t, err)
	assert.False(t, off, "password sign-in stays on by default")

	require.NoError(t, s.SetPasswordLoginDisabled(ctx, true))
	off, err = s.PasswordLoginDisabled(ctx)
	require.NoError(t, err)
	assert.True(t, off)

	// The README recovery: deleting the KV row turns password sign-in back on.
	_, err = db.ExecContext(ctx, "DELETE FROM kv WHERE key = ?", KeyPasswordLoginDisabled)
	require.NoError(t, err)
	off, err = s.PasswordLoginDisabled(ctx)
	require.NoError(t, err)
	assert.False(t, off)

	require.NoError(t, s.SetPasswordLoginDisabled(ctx, true))
	require.NoError(t, s.SetPasswordLoginDisabled(ctx, false))
	off, err = s.PasswordLoginDisabled(ctx)
	require.NoError(t, err)
	assert.False(t, off)
}
