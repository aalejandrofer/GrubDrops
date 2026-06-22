package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/store"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

// TestAddChannelWhitelist_InsertsRows proves that POSTing account_id + channel
// to /drops/whitelist/channel inserts the expected account_channels rows.
func TestAddChannelWhitelist_InsertsRows(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	q := gen.New(db)

	// Seed account "acc-1".
	now := time.Now().Unix()
	_, err = q.CreateAccount(ctx, gen.CreateAccountParams{
		ID: "acc-1", Platform: "kick", DisplayName: "TTik3r",
		Status: "idle", FingerprintJson: "{}", Enabled: 1,
		CreatedAt: now, UpdatedAt: now,
	})
	require.NoError(t, err)

	d := &dropsDeps{q: q}

	form := url.Values{}
	form.Set("account_id", "acc-1")
	form.Add("channel", "adrianozendejas32")

	req := httptest.NewRequest(http.MethodPost, "/drops/whitelist/channel", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	d.addChannelWhitelist(rec, req)

	// Handler should redirect to /drops on success.
	require.Equal(t, http.StatusSeeOther, rec.Code)

	rows, err := q.ListAccountChannels(ctx, "acc-1")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "adrianozendejas32", rows[0].Channel)
}
