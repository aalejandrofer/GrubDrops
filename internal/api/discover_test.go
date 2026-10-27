package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
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
		Discover: svc, Chosen: store.NewChosenGames(db, gen.New(db)),
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
	assert.Equal(t, "https://static-cdn.jtvnw.net/ttv-boxart/263490_IGDB-144x192.jpg", rust.BoxArtURL, "id fallback")
	require.NotNil(t, rust.EndsAt)
	assert.Equal(t, "2099-01-02T03:04:05Z", *rust.EndsAt)
	assert.Equal(t, []discoverRewardJSON{{Name: "Crate", ImageURL: "https://static-cdn.jtvnw.net/a.png", Minutes: 60}}, rust.Rewards, "duplicate rewards collapse")
}

func discoverByID(t *testing.T, svc *discover.Service) map[string]discoverCampaignJSON {
	t.Helper()
	e := newDiscoverEnv(t, svc)
	_, b := e.do("GET", "/api/discover", "", nil)
	var got discoverJSON
	require.NoError(t, json.Unmarshal(b, &got))
	out := map[string]discoverCampaignJSON{}
	for _, c := range got.Campaigns {
		out[c.Platform+"/"+c.ID] = c
	}
	return out
}

func TestDiscover_BoxArtResolution(t *testing.T) {
	twitchCamps := []platform.Campaign{
		// GQL game.boxArtURL, as the twitch backend fills it.
		{ID: "t-ak", Platform: "twitch", Game: "ARKNIGHTS: ENDFIELD", GameID: "1448964995",
			BoxArtURL: "https://static-cdn.jtvnw.net/ttv-boxart/1448964995_IGDB-144x192.jpg"},
		// No boxArtURL in the payload: IGDB id form.
		{ID: "t-cc", Platform: "twitch", Game: "Captain's Call", GameID: "856072301"},
	}
	kickCamps := []platform.Campaign{
		// Category image wins, through the proxy.
		{ID: "k-cod", Platform: "kick", Game: "Call of Duty: Modern Warfare 4",
			BoxArtURL: "https://files.kick.com/images/subcategories/14138/banner/b049272d-6811-4c3d-b19c-8f6cc170c5ac"},
		// No category image: the same game's Twitch art, matched by slug.
		{ID: "k-ak", Platform: "kick", Game: "Arknights: Endfield"},
		// Neither: name-keyed Twitch URL.
		{ID: "k-rust", Platform: "kick", Game: "Rust"},
	}
	got := discoverByID(t, discover.New(
		discover.Source{Platform: "twitch", Timeout: time.Second, Fetch: fixed(twitchCamps, nil)},
		discover.Source{Platform: "kick", Timeout: time.Second, Fetch: fixed(kickCamps, nil)},
	))
	assert.Equal(t, "https://static-cdn.jtvnw.net/ttv-boxart/1448964995_IGDB-144x192.jpg", got["twitch/t-ak"].BoxArtURL)
	assert.Equal(t, "https://static-cdn.jtvnw.net/ttv-boxart/856072301_IGDB-144x192.jpg", got["twitch/t-cc"].BoxArtURL)
	assert.Equal(t, "/api/img/kick?u="+url.QueryEscape("https://files.kick.com/images/subcategories/14138/banner/b049272d-6811-4c3d-b19c-8f6cc170c5ac"),
		got["kick/k-cod"].BoxArtURL)
	assert.Equal(t, "https://static-cdn.jtvnw.net/ttv-boxart/1448964995_IGDB-144x192.jpg", got["kick/k-ak"].BoxArtURL, "twitch art by name")
	assert.Equal(t, "https://static-cdn.jtvnw.net/ttv-boxart/Rust-144x192.jpg", got["kick/k-rust"].BoxArtURL)
}

// A Kick-only game with no category image uses the Twitch id the collector
// looked up for the chosen game, not the name-keyed URL IGDB games never
// answer (GRUB-33).
func TestDiscover_KickArtFromChosenTwitchID(t *testing.T) {
	e := newDiscoverEnv(t, discover.New(discover.Source{Platform: "kick", Timeout: time.Second,
		Fetch: fixed([]platform.Campaign{{ID: "k-wd", Platform: "kick", Game: "WARDOGS"}}, nil)}))
	ctx := context.Background()
	q := e.s.d.Q
	require.NoError(t, q.UpsertGame(ctx, gen.UpsertGameParams{ID: "g_wardogs", Name: "WARDOGS", Slug: "wardogs"}))
	require.NoError(t, q.SetGameTwitchID(ctx, gen.SetGameTwitchIDParams{TwitchGameID: "743893402", ID: "g_wardogs"}))
	require.NoError(t, e.s.d.Chosen.Set(ctx, []string{"g_wardogs"}))
	_, b := e.do("GET", "/api/discover", "", nil)
	var got discoverJSON
	require.NoError(t, json.Unmarshal(b, &got))
	require.Len(t, got.Campaigns, 1)
	assert.Equal(t, "https://static-cdn.jtvnw.net/ttv-boxart/743893402_IGDB-144x192.jpg", got.Campaigns[0].BoxArtURL)
}

func TestDiscover_TwitchNeedsLinkOnlyForPublisherLink(t *testing.T) {
	camps := []platform.Campaign{
		// Badge/emote campaign: Twitch says not connected, link page is twitch.tv.
		{ID: "badge", Platform: "twitch", Game: "Castlevania: Belmont's Curse", AccountLinkChecked: true,
			AccountLinkURL: "https://www.twitch.tv/drops/campaigns?dropID=0f3c2a71-5b1e-4f0e-9d0a-6a2f1c9b8e47"},
		{ID: "nourl", Platform: "twitch", Game: "Cash is King!", AccountLinkChecked: true},
		{ID: "riot", Platform: "twitch", Game: "VALORANT", AccountLinkChecked: true,
			AccountLinkURL: "https://account.riotgames.com/#connected-accounts"},
		{ID: "riot-ok", Platform: "twitch", Game: "VALORANT", AccountLinkChecked: true, AccountLinked: true,
			AccountLinkURL: "https://account.riotgames.com/#connected-accounts"},
	}
	got := discoverByID(t, discover.New(discover.Source{Platform: "twitch", Timeout: time.Second, Fetch: fixed(camps, nil)}))
	assert.False(t, got["twitch/badge"].NeedsLink, "twitch.tv link page")
	assert.False(t, got["twitch/nourl"].NeedsLink, "no link page")
	assert.True(t, got["twitch/riot"].NeedsLink, "publisher link, not connected")
	assert.False(t, got["twitch/riot-ok"].NeedsLink, "publisher link, connected")
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
