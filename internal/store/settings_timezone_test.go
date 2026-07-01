package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

func TestSettings_Timezone(t *testing.T) {
	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "t.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	s := NewSettings(gen.New(db))
	ctx := context.Background()

	// Default is empty (unset → caller falls back to TZ env / UTC).
	tz, err := s.Timezone(ctx)
	require.NoError(t, err)
	assert.Equal(t, "", tz)

	require.NoError(t, s.SetTimezone(ctx, "Asia/Shanghai"))
	tz, err = s.Timezone(ctx)
	require.NoError(t, err)
	assert.Equal(t, "Asia/Shanghai", tz)

	// Whitespace is trimmed.
	require.NoError(t, s.SetTimezone(ctx, "  Europe/Madrid  "))
	tz, _ = s.Timezone(ctx)
	assert.Equal(t, "Europe/Madrid", tz)

	// Invalid zone is rejected and does not overwrite the stored value.
	require.Error(t, s.SetTimezone(ctx, "Not/AZone"))
	tz, _ = s.Timezone(ctx)
	assert.Equal(t, "Europe/Madrid", tz)

	// Empty clears back to unset.
	require.NoError(t, s.SetTimezone(ctx, ""))
	tz, _ = s.Timezone(ctx)
	assert.Equal(t, "", tz)
}
