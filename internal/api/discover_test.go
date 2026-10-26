package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/discover"
	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/store"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

type discoverEnv struct {
	*env
	csrf string
}

func newDiscoverEnv(t *testing.T, svc *discover.Service) *discoverEnv {
	t.Helper()
	t.Setenv("GRUB_AUTHBYPASS", "1")
	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "t.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	sm := scs.New()
	sm.Store = NewKVSessionStore(db)
	sm.Lifetime = time.Hour
	s := newServer(Deps{
		DB: db, Q: gen.New(db), Session: sm, Version: "test", Hub: NewHub(),
		SPA:      fstest.MapFS{"index.html": {Data: []byte("x")}},
		Discover: svc,
	})
	s.routesDiscover(s.protected)
	srv := httptest.NewServer(s.handler())
	t.Cleanup(srv.Close)
	jar, _ := cookiejar.New(nil)
	de := &discoverEnv{env: &env{t: t, srv: srv, cl: &http.Client{Jar: jar}, s: s}}
	_, de.csrf = de.session()
	return de
}

func fixed(camps []platform.Campaign, err error) discover.Fetcher {
	return func(context.Context) ([]platform.Campaign, error) { return camps, err }
}

func TestDiscover_Unavailable(t *testing.T) {
	e := newDiscoverEnv(t, nil)
	resp, _ := e.do("GET", "/api/discover", "", nil)
	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
}

func TestDiscover_ShapeAndPartialFailure(t *testing.T) {
	ends := time.Date(2099, 1, 2, 3, 4, 5, 0, time.UTC)
	twitchCamps := []platform.Campaign{
		{ID: "t2", Platform: "twitch", Game: "Rust", GameID: "263490", Name: "Rust Drops", EndsAt: ends,
			AccountLinked: true, AccountLinkURL: "https://example.com/linked",
			Benefits: []platform.DropBenefit{
				{Name: "Crate", ImageURL: "https://static-cdn.jtvnw.net/a.png", RequiredMinutes: 60},
				{Name: "Crate", ImageURL: "https://static-cdn.jtvnw.net/a.png", RequiredMinutes: 60},
			}},
		{ID: "t1", Platform: "twitch", Game: "Apex Legends", Name: "Apex", AccountLinkURL: "https://example.com/link"},
	}
	svc := discover.New(
		discover.Source{Platform: "twitch", Timeout: time.Second, Fetch: fixed(twitchCamps, nil)},
		discover.Source{Platform: "kick", Timeout: time.Second, Fetch: fixed(nil, errors.New("down"))},
	)
	e := newDiscoverEnv(t, svc)
	resp, b := e.do("GET", "/api/discover", "", nil)
	require.Equal(t, 200, resp.StatusCode, string(b))

	var got discoverJSON
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, discoverPlatformJSON{OK: true, Count: 2}, got.Platforms["twitch"])
	assert.Equal(t, discoverPlatformJSON{OK: false, Error: "failed"}, got.Platforms["kick"])
	require.NotNil(t, got.FetchedAt)
	require.Len(t, got.Campaigns, 2)

	apex, rust := got.Campaigns[0], got.Campaigns[1]
	assert.Equal(t, "Apex Legends", apex.Game, "sorted by game")
	assert.True(t, apex.NeedsLink, "unlinked twitch campaign with a link url")
	assert.Equal(t, []discoverRewardJSON{}, apex.Rewards)

	assert.False(t, rust.NeedsLink, "linked twitch campaign")
	assert.Equal(t, "https://static-cdn.jtvnw.net/ttv-boxart/263490-144x192.jpg", rust.BoxArtURL)
	require.NotNil(t, rust.EndsAt)
	assert.Equal(t, "2099-01-02T03:04:05Z", *rust.EndsAt)
	assert.Equal(t, []discoverRewardJSON{{Name: "Crate", ImageURL: "https://static-cdn.jtvnw.net/a.png", Minutes: 60}}, rust.Rewards, "duplicate rewards collapse")
}

func TestDiscover_KickRewardsProxiedAndLinkSafe(t *testing.T) {
	svc := discover.New(discover.Source{Platform: "kick", Timeout: time.Second, Fetch: fixed([]platform.Campaign{
		{ID: "k1", Platform: "kick", Game: "Rust", AccountLinkURL: "https://kick.com/connect",
			Benefits: []platform.DropBenefit{{Name: "Hat", ImageURL: "https://ext.cdn.kick.com/drops/a.png"}}},
		{ID: "k2", Platform: "kick", Game: "Rust", AccountLinkURL: "javascript:alert(1)"},
	}, nil)})
	e := newDiscoverEnv(t, svc)
	_, b := e.do("GET", "/api/discover", "", nil)
	var got discoverJSON
	require.NoError(t, json.Unmarshal(b, &got))
	require.Len(t, got.Campaigns, 2)
	assert.True(t, got.Campaigns[0].NeedsLink)
	assert.Equal(t, "/api/img/kick?u=https%3A%2F%2Fext.cdn.kick.com%2Fdrops%2Fa.png", got.Campaigns[0].Rewards[0].ImageURL)
	assert.Empty(t, got.Campaigns[1].LinkURL, "unsafe link dropped")
	assert.False(t, got.Campaigns[1].NeedsLink)
}

func TestDiscover_RefreshBypassesCacheAfterMinRefresh(t *testing.T) {
	var n atomic.Int32
	now := time.Unix(1000, 0)
	svc := discover.New(discover.Source{Platform: "kick", Timeout: time.Second, Fetch: func(context.Context) ([]platform.Campaign, error) {
		n.Add(1)
		return nil, nil
	}})
	svc.Now = func() time.Time { return now }
	e := newDiscoverEnv(t, svc)

	e.do("GET", "/api/discover", "", nil)
	e.do("GET", "/api/discover", "", nil)
	assert.EqualValues(t, 1, n.Load())

	resp, _ := e.do("POST", "/api/discover/refresh", e.csrf, nil)
	assert.Equal(t, 200, resp.StatusCode)
	assert.EqualValues(t, 1, n.Load(), "refresh is rate-limited")

	now = now.Add(discover.DefaultMinRefresh)
	resp, b := e.do("POST", "/api/discover/refresh", e.csrf, nil)
	assert.Equal(t, 200, resp.StatusCode)
	assert.EqualValues(t, 2, n.Load())
	var got discoverJSON
	require.NoError(t, json.Unmarshal(b, &got))
	require.NotNil(t, got.NextRefreshAt)
	assert.Equal(t, now.Add(discover.DefaultMinRefresh).UTC().Format(time.RFC3339), *got.NextRefreshAt)
}
