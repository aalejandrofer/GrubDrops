package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/alexedwards/scs/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/store"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

// seedAccount opens a fresh DB, inserts one account row, and returns the
// *gen.Queries and the account ID. Mirrors the seeding pattern in
// handlers_accounts_toggle_test.go.
func seedAccount(t *testing.T, ctx context.Context, enabled bool) (*gen.Queries, string) {
	t.Helper()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	q := gen.New(db)

	const id = "acc_api_seed"
	enabledVal := int64(0)
	if enabled {
		enabledVal = 1
	}
	_, err = db.ExecContext(ctx,
		`INSERT INTO accounts (id, platform, display_name, status, fingerprint_json, enabled, created_at, updated_at)
		VALUES (?, 'twitch', 'Test', 'idle', '{}', ?, 1, 1)`, id, enabledVal)
	require.NoError(t, err)
	return q, id
}

func TestDoToggleEnabledFlipsFlag(t *testing.T) {
	ctx := context.Background()
	q, id := seedAccount(t, ctx, true)

	d := accountsDeps{q: q}
	enabled, err := d.doToggleEnabled(ctx, id)
	require.NoError(t, err)
	assert.False(t, enabled, "was enabled -> now disabled")

	acc, err := q.GetAccount(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, int64(0), acc.Enabled)
}

func TestAPIToggle_RequiresCSRF(t *testing.T) {
	t.Setenv("GRUB_AUTHBYPASS", "true")
	s, q := newTestSettings(t)
	h := NewRouter(Deps{Q: q, Session: scs.New(), SettingsStore: s, SecureCookies: false})
	// POST without an X-CSRF-Token → nosurf 403 JSON (csrf), proving the route
	// is CSRF-protected.
	req := httptest.NewRequest(http.MethodPost, "/api/accounts/x/toggle", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusForbidden, rec.Code)
	assert.Contains(t, rec.Body.String(), `"code":"csrf"`)
}
