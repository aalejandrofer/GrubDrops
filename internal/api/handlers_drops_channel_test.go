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

// TestAddChannelWhitelist_WritesBothTables proves that whitelisting a
// channel writes account_channels (v1) AND account_streamer_priority (v2),
// with the platform pulled from the account and rank = next rank (max+1)
// for that account — v2's dropStore.StreamerPriority never saw edits after
// the one-time 0016 migration seed, since it reads account_streamer_priority
// exclusively.
func TestAddChannelWhitelist_WritesBothTables(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	q := gen.New(db)

	now := time.Now().Unix()
	_, err = q.CreateAccount(ctx, gen.CreateAccountParams{
		ID: "acc-1", Platform: "kick", DisplayName: "TTik3r",
		Status: "idle", FingerprintJson: "{}", Enabled: 1,
		CreatedAt: now, UpdatedAt: now,
	})
	require.NoError(t, err)

	// Pre-seed one existing priority row so the new one must land at
	// max+1, not a hardcoded rank.
	require.NoError(t, q.AddStreamerPriority(ctx, gen.AddStreamerPriorityParams{
		AccountID: "acc-1", Platform: "kick", Login: "existingchan", Rank: 0,
	}))

	d := &dropsDeps{q: q}

	form := url.Values{}
	form.Set("account_id", "acc-1")
	form.Add("channel", "adrianozendejas32")

	req := httptest.NewRequest(http.MethodPost, "/drops/whitelist/channel", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	d.addChannelWhitelist(rec, req)

	require.Equal(t, http.StatusSeeOther, rec.Code)

	acRows, err := q.ListAccountChannels(ctx, "acc-1")
	require.NoError(t, err)
	require.Len(t, acRows, 1)
	assert.Equal(t, "adrianozendejas32", acRows[0].Channel)

	prioRows, err := q.ListStreamerPriority(ctx, "acc-1")
	require.NoError(t, err)
	require.Len(t, prioRows, 2)
	var found bool
	for _, r := range prioRows {
		if r.Login == "adrianozendejas32" {
			found = true
			assert.Equal(t, int64(1), r.Rank, "new entry ranks after the existing max (0)")
		}
	}
	assert.True(t, found, "adrianozendejas32 must be written to account_streamer_priority")
}

// TestRemoveChannelWhitelist_DeletesFromBothTables proves the ✕ un-whitelist
// action deletes from account_channels (v1) AND account_streamer_priority
// (v2), so a removed channel actually stops being mined under v2 too.
func TestRemoveChannelWhitelist_DeletesFromBothTables(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	q := gen.New(db)

	now := time.Now().Unix()
	_, err = q.CreateAccount(ctx, gen.CreateAccountParams{
		ID: "acc-1", Platform: "kick", DisplayName: "TTik3r",
		Status: "idle", FingerprintJson: "{}", Enabled: 1,
		CreatedAt: now, UpdatedAt: now,
	})
	require.NoError(t, err)
	require.NoError(t, q.AddAccountChannel(ctx, gen.AddAccountChannelParams{
		AccountID: "acc-1", Channel: "adrianozendejas32", Rank: 0,
	}))
	require.NoError(t, q.AddStreamerPriority(ctx, gen.AddStreamerPriorityParams{
		AccountID: "acc-1", Platform: "kick", Login: "adrianozendejas32", Rank: 0,
	}))

	d := &dropsDeps{q: q}

	form := url.Values{}
	form.Set("account_id", "acc-1")
	form.Add("channel", "adrianozendejas32")

	req := httptest.NewRequest(http.MethodPost, "/drops/whitelist/channel/remove", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	d.removeChannelWhitelist(rec, req)

	require.Equal(t, http.StatusSeeOther, rec.Code)

	acRows, err := q.ListAccountChannels(ctx, "acc-1")
	require.NoError(t, err)
	assert.Empty(t, acRows)

	prioRows, err := q.ListStreamerPriority(ctx, "acc-1")
	require.NoError(t, err)
	assert.Empty(t, prioRows)
}
