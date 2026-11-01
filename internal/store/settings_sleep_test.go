package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

func TestSettings_SleepWindow(t *testing.T) {
	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "t.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	s := NewSettings(gen.New(db))
	ctx := context.Background()

	w, err := s.SleepWindow(ctx)
	require.NoError(t, err)
	assert.Equal(t, SleepWindow{Enabled: false, Start: "02:00", End: "08:00"}, w, "off by default, 02:00-08:00")

	want := SleepWindow{Enabled: true, Start: "23:30", End: "07:00"}
	require.NoError(t, s.SetSleepWindow(ctx, want))
	w, err = s.SleepWindow(ctx)
	require.NoError(t, err)
	assert.Equal(t, want, w)

	for _, bad := range []SleepWindow{
		{Enabled: true, Start: "25:00", End: "07:00"},
		{Enabled: true, Start: "23:30", End: "7:00"},
		{Enabled: true, Start: "06:00", End: "06:00"},
		{Enabled: false, Start: "", End: "07:00"},
	} {
		require.Error(t, s.SetSleepWindow(ctx, bad), "%+v", bad)
		w, _ = s.SleepWindow(ctx)
		assert.Equal(t, want, w, "a rejected window leaves the saved one")
	}

	// A corrupt stored value reads back as the default.
	require.NoError(t, s.setString(ctx, keySleepStart, "nope"))
	w, _ = s.SleepWindow(ctx)
	assert.Equal(t, "02:00", w.Start)
}

func TestAccounts_SkipSleepColumn(t *testing.T) {
	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "t.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	q := gen.New(db)
	ctx := context.Background()
	a, err := q.CreateAccount(ctx, gen.CreateAccountParams{
		ID: "a1", Platform: "kick", DisplayName: "n", Status: "idle",
		FingerprintJson: "{}", Enabled: 1, CreatedAt: 1, UpdatedAt: 1,
	})
	require.NoError(t, err)
	assert.EqualValues(t, 0, a.SkipSleep, "off by default")
	require.NoError(t, q.SetAccountSkipSleep(ctx, gen.SetAccountSkipSleepParams{
		SkipSleep: 1, UpdatedAt: time.Now().Unix(), ID: "a1",
	}))
	a, err = q.GetAccount(ctx, "a1")
	require.NoError(t, err)
	assert.EqualValues(t, 1, a.SkipSleep)
}

func TestAccounts_PointsModeColumn(t *testing.T) {
	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "t.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	q := gen.New(db)
	ctx := context.Background()
	a, err := q.CreateAccount(ctx, gen.CreateAccountParams{
		ID: "a1", Platform: "twitch", DisplayName: "n", Status: "idle",
		FingerprintJson: "{}", Enabled: 1, CreatedAt: 1, UpdatedAt: 1,
	})
	require.NoError(t, err)
	assert.Equal(t, "off", a.PointsMode, "off by default")
	for _, mode := range []string{"drops_points", "points_only", "off"} {
		require.NoError(t, q.SetAccountPointsMode(ctx, gen.SetAccountPointsModeParams{
			PointsMode: mode, UpdatedAt: time.Now().Unix(), ID: "a1",
		}))
		a, err = q.GetAccount(ctx, "a1")
		require.NoError(t, err)
		assert.Equal(t, mode, a.PointsMode)
	}
}
