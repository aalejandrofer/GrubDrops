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

	"github.com/alexedwards/scs/v2"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/store"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
	"github.com/aalejandrofer/grubdrops/internal/web"
)

func TestNewPostAllowsAtMostThreeTwitchAccounts(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	q := gen.New(db)
	tmpl, err := web.Templates()
	require.NoError(t, err)
	d := accountsDeps{q: q, db: db, t: tmpl, sm: scs.New()}

	seed := func(id string, enabled int64) {
		t.Helper()
		_, err := q.CreateAccount(ctx, gen.CreateAccountParams{
			ID: id, Platform: "twitch", DisplayName: id, Status: "idle",
			FingerprintJson: "{}", Enabled: enabled,
			CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix(),
		})
		require.NoError(t, err)
	}
	post := func(platform string) *httptest.ResponseRecorder {
		t.Helper()
		form := url.Values{"platform": {platform}, "display_name": {"test"}}
		req := httptest.NewRequest(http.MethodPost, "/accounts/new", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		d.newPost(rec, req)
		return rec
	}

	seed("tw-1", 0) // Disabled accounts still count toward the account limit.
	seed("tw-2", 1)
	third := post("twitch")
	require.Equal(t, http.StatusSeeOther, third.Code)
	accounts, err := q.ListAllAccounts(ctx)
	require.NoError(t, err)
	require.Len(t, accounts, 3)

	fourth := post("twitch")
	require.Equal(t, http.StatusOK, fourth.Code)
	require.Contains(t, fourth.Body.String(), "You can have at most three Twitch accounts")
	accounts, err = q.ListAllAccounts(ctx)
	require.NoError(t, err)
	require.Len(t, accounts, 3)

	get := httptest.NewRecorder()
	d.newGet(get, httptest.NewRequest(http.MethodGet, "/accounts/new", nil))
	require.Equal(t, http.StatusOK, get.Code)
	require.Contains(t, get.Body.String(), `value="twitch" disabled`)

	kick := post("kick")
	require.Equal(t, http.StatusSeeOther, kick.Code, "the Twitch-specific limit must not block Kick")
}
