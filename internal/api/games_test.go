package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/collect"
	"github.com/aalejandrofer/grubdrops/internal/gameslug"
	"github.com/aalejandrofer/grubdrops/internal/plan"
	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/platform/twitch"
	"github.com/aalejandrofer/grubdrops/internal/runner"
	"github.com/aalejandrofer/grubdrops/internal/store"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

type gamesFakePool struct {
	snaps   []runner.Snapshot
	rescans *int
}

func (p gamesFakePool) Snapshots() []runner.Snapshot { return p.snaps }
func (gamesFakePool) SetOrder(string, []string)      {}
func (gamesFakePool) ResetOrder(string)              {}
func (p gamesFakePool) RescanAll() {
	if p.rescans != nil {
		*p.rescans++
	}
}
func (gamesFakePool) OrderFunc(string) func() []string { return nil }
func (gamesFakePool) MineNow(string) (string, error)   { return "", nil }
func (gamesFakePool) RecheckLinks(context.Context, string) (runner.LinkRecheck, error) {
	return runner.LinkRecheck{}, nil
}

type gamesEnv struct {
	*env
	chosen  *store.ChosenGames
	board   *collect.Board
	q       *gen.Queries
	changed int
	rescans int
	csrf    string
}

func newGamesEnv(t *testing.T, snaps ...runner.Snapshot) *gamesEnv {
	t.Helper()
	t.Setenv("GRUB_AUTHBYPASS", "1")
	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "t.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	// Start from an empty catalogue: migrations seed some games.
	_, err = db.Exec(`DELETE FROM games`)
	require.NoError(t, err)
	q := gen.New(db)
	sm := scs.New()
	sm.Store = NewKVSessionStore(db)
	sm.Lifetime = time.Hour
	ge := &gamesEnv{chosen: store.NewChosenGames(db, q), board: collect.NewBoard(), q: q}
	s := newServer(Deps{
		DB: db, Q: q, Session: sm, Version: "test", Hub: NewHub(),
		SPA:    fstest.MapFS{"index.html": {Data: []byte("x")}},
		Chosen: ge.chosen, Board: ge.board, Pool: gamesFakePool{snaps, &ge.rescans},
		GamesChanged: func() { ge.changed++ },
	})
	s.routesGames(s.protected)
	srv := httptest.NewServer(s.handler())
	t.Cleanup(srv.Close)
	jar, _ := cookiejar.New(nil)
	ge.env = &env{t: t, srv: srv, cl: &http.Client{Jar: jar}, s: s}
	_, ge.csrf = ge.session()
	return ge
}

func (g *gamesEnv) seed(names ...string) []string {
	g.t.Helper()
	ids := make([]string, len(names))
	for i, n := range names {
		ids[i] = gameslug.ID(n)
		require.NoError(g.t, g.q.UpsertGame(context.Background(),
			gen.UpsertGameParams{ID: ids[i], Name: n, Slug: gameslug.Slug(n)}))
	}
	return ids
}

func (g *gamesEnv) chosenIDs() []string {
	g.t.Helper()
	l, err := g.chosen.List(context.Background())
	require.NoError(g.t, err)
	ids := []string{}
	for _, c := range l {
		ids = append(ids, c.ID)
	}
	return ids
}

// A campaign with nothing left to mine is done; one with a queued drop waits.
func TestGames_ListDoneVersusWaiting(t *testing.T) {
	g := newGamesEnv(t)
	ids := g.seed("Rust", "Overwatch")
	require.NoError(t, g.chosen.Set(context.Background(), ids))
	active := func(id, game string) platform.Campaign {
		return platform.Campaign{
			ID: id, Platform: "twitch", Game: game, Name: game + " Drops", Status: "active",
			EndsAt: time.Now().Add(24 * time.Hour), Benefits: []platform.DropBenefit{{ID: id + "b", RequiredMinutes: 30}},
		}
	}
	rust, ow := active("c1", "Rust"), active("c2", "Overwatch")
	g.board.Observe(runner.Scan{AccountID: "a1", Platform: "twitch", At: time.Now(),
		Campaigns: []platform.Campaign{rust, ow},
		Result:    plan.Result{Queue: []plan.Target{{Campaign: ow, Benefit: ow.Benefits[0]}}},
	})

	resp, b := g.do("GET", "/api/games", "", nil)
	require.Equal(t, 200, resp.StatusCode)
	var got gamesJSON
	require.NoError(t, json.Unmarshal(b, &got))
	require.Len(t, got.Games, 2)
	assert.Equal(t, "done", got.Games[0].State)
	assert.Len(t, got.Games[0].Campaigns, 1, "a done campaign is still listed")
	assert.Equal(t, "waiting", got.Games[1].State)
}

func TestGames_ListStatesAndOrder(t *testing.T) {
	snap := runner.Snapshot{AccountID: "a1", Current: &plan.Target{Campaign: platform.Campaign{Game: "rust"}}}
	g := newGamesEnv(t, snap)
	ids := g.seed("Rust", "Overwatch", "Dead by Daylight")
	require.NoError(t, g.chosen.Set(context.Background(), ids[:2]))
	plus1 := time.FixedZone("plus1", 3600)
	ends := time.Date(2026, 11, 1, 13, 0, 0, 0, plus1)
	g.board.Observe(runner.Scan{AccountID: "a1", Platform: "twitch", At: time.Date(2026, 10, 25, 11, 0, 0, 0, plus1), Campaigns: []platform.Campaign{{
		ID: "c1", Platform: "twitch", Game: "Rust", Name: "Rust Drops", Status: "active",
		StartsAt: time.Now().Add(-time.Hour), EndsAt: ends,
		Benefits: []platform.DropBenefit{{ID: "b1", RequiredMinutes: 30}},
	}}})

	resp, b := g.do("GET", "/api/games", "", nil)
	require.Equal(t, 200, resp.StatusCode)
	var got gamesJSON
	require.NoError(t, json.Unmarshal(b, &got))
	require.Len(t, got.Games, 2)
	rust, ow := got.Games[0], got.Games[1]
	assert.Equal(t, "Rust", rust.Name)
	assert.Equal(t, 1, rust.Priority)
	assert.Equal(t, "mining", rust.State)
	require.Len(t, rust.Campaigns, 1)
	assert.Equal(t, "c1", rust.Campaigns[0].ID)
	assert.Equal(t, 1, rust.Campaigns[0].Drops)
	assert.Equal(t, 2, ow.Priority)
	assert.Equal(t, "no_campaign", ow.State)
	require.NotNil(t, rust.Campaigns[0].EndsAt)
	assert.Equal(t, "2026-11-01T12:00:00Z", *rust.Campaigns[0].EndsAt)
	require.NotNil(t, got.LastScanAt)
	assert.Equal(t, "2026-10-25T10:00:00Z", *got.LastScanAt)
	assert.Equal(t, "https://static-cdn.jtvnw.net/ttv-boxart/Dead%20by%20Daylight-144x192.jpg", boxArtURL("Dead by Daylight", ""))
	assert.Equal(t, "https://static-cdn.jtvnw.net/ttv-boxart/Rust-144x192.jpg", rust.BoxArtURL)
}

func TestBoxArtURL_PrefersTwitchID(t *testing.T) {
	assert.Equal(t, "https://static-cdn.jtvnw.net/ttv-boxart/1264310518_IGDB-144x192.jpg", boxArtURL("Marvel Rivals", "1264310518"))
	assert.Equal(t, "https://static-cdn.jtvnw.net/ttv-boxart/Rust-144x192.jpg", boxArtURL("Rust", ""))
}

func TestGames_ListUsesTwitchGameIDBoxArt(t *testing.T) {
	g := newGamesEnv(t)
	ids := g.seed("Marvel Rivals")
	require.NoError(t, g.q.SetGameTwitchID(context.Background(), gen.SetGameTwitchIDParams{TwitchGameID: "1264310518", ID: ids[0]}))
	require.NoError(t, g.chosen.Set(context.Background(), ids))
	resp, b := g.do("GET", "/api/games", "", nil)
	require.Equal(t, 200, resp.StatusCode)
	var got gamesJSON
	require.NoError(t, json.Unmarshal(b, &got))
	require.Len(t, got.Games, 1)
	assert.Equal(t, "https://static-cdn.jtvnw.net/ttv-boxart/1264310518_IGDB-144x192.jpg", got.Games[0].BoxArtURL)
}

func TestGames_AddAppendsAndNotifies(t *testing.T) {
	g := newGamesEnv(t)
	ids := g.seed("Overwatch")
	require.NoError(t, g.chosen.Set(context.Background(), ids))
	resp, b := g.do("POST", "/api/games", g.csrf, map[string]string{"name": "Rust"})
	require.Equal(t, 201, resp.StatusCode, string(b))
	var got gameJSON
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, "Rust", got.Name)
	assert.Equal(t, 2, got.Priority)
	assert.Equal(t, []string{"g_overwatch", "g_rust"}, g.chosenIDs())
	assert.Equal(t, 1, g.changed)
}

func TestGames_AddKeepsStoredName(t *testing.T) {
	g := newGamesEnv(t)
	g.seed("Rust")
	resp, b := g.do("POST", "/api/games", g.csrf, map[string]string{"name": " rust "})
	require.Equal(t, 201, resp.StatusCode, string(b))
	var got gameJSON
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, "Rust", got.Name)
	assert.Equal(t, "rust", got.Slug)
	all, err := g.q.ListAllGames(context.Background())
	require.NoError(t, err)
	require.Len(t, all, 1)
	assert.Equal(t, "Rust", all[0].Name)
}

func TestGames_AddReusesSeededRowWithOtherID(t *testing.T) {
	g := newGamesEnv(t)
	require.NoError(t, g.q.UpsertGame(context.Background(),
		gen.UpsertGameParams{ID: "g_apex", Name: "Apex Legends", Slug: "apex-legends"}))
	resp, b := g.do("POST", "/api/games", g.csrf, map[string]string{"name": "Apex Legends"})
	require.Equal(t, 201, resp.StatusCode, string(b))
	assert.Equal(t, []string{"g_apex"}, g.chosenIDs())
}

func TestGames_AddDuplicateConflict(t *testing.T) {
	g := newGamesEnv(t)
	ids := g.seed("Rust")
	require.NoError(t, g.chosen.Set(context.Background(), ids))
	resp, b := g.do("POST", "/api/games", g.csrf, map[string]string{"name": " rust "})
	assert.Equal(t, 409, resp.StatusCode)
	assert.JSONEq(t, `{"error":"conflict","priority":1}`, string(b))
	assert.Equal(t, []string{"g_rust"}, g.chosenIDs())
	assert.Equal(t, 0, g.changed)
}

func TestGames_AddEmpty400(t *testing.T) {
	g := newGamesEnv(t)
	for _, name := range []string{"", "   ", "!!!"} {
		resp, _ := g.do("POST", "/api/games", g.csrf, map[string]string{"name": name})
		assert.Equal(t, 400, resp.StatusCode, name)
	}
	assert.Empty(t, g.chosenIDs())
	assert.Equal(t, 0, g.changed)
}

func TestGames_PutReorderAndRemove(t *testing.T) {
	g := newGamesEnv(t)
	ids := g.seed("Alpha", "Bravo", "Charlie")
	a, b := ids[0], ids[1]
	require.NoError(t, g.chosen.Set(context.Background(), ids))

	resp, _ := g.do("PUT", "/api/games", g.csrf, map[string]any{"ids": []string{b, a}})
	assert.Equal(t, 204, resp.StatusCode)
	assert.Equal(t, []string{b, a}, g.chosenIDs())
	assert.Equal(t, 1, g.changed)

	resp, _ = g.do("PUT", "/api/games", g.csrf, map[string]any{"ids": []string{a, "g_nope"}})
	assert.Equal(t, 400, resp.StatusCode)
	assert.Equal(t, []string{b, a}, g.chosenIDs())
	assert.Equal(t, 1, g.changed)

	resp, _ = g.do("PUT", "/api/games", g.csrf, map[string]any{})
	assert.Equal(t, 400, resp.StatusCode)

	resp, _ = g.do("PUT", "/api/games", g.csrf, map[string]any{"ids": []string{}})
	assert.Equal(t, 204, resp.StatusCode)
	assert.Empty(t, g.chosenIDs())
}

func TestGames_Scan(t *testing.T) {
	g := newGamesEnv(t)
	resp, _ := g.do("POST", "/api/games/scan", g.csrf, nil)
	assert.Equal(t, 202, resp.StatusCode)
	assert.Equal(t, 1, g.rescans)

	resp, _ = g.do("POST", "/api/games/scan", "", nil)
	assert.Equal(t, 403, resp.StatusCode)
	assert.Equal(t, 1, g.rescans)
}

func TestGames_Search(t *testing.T) {
	g := newGamesEnv(t)
	ids := g.seed("Rust", "Rusty Lake", "Overwatch")
	require.NoError(t, g.chosen.Set(context.Background(), []string{ids[2], ids[0]}))

	resp, b := g.do("GET", "/api/games/search?q=RUS", "", nil)
	require.Equal(t, 200, resp.StatusCode)
	var hits []gameHitJSON
	require.NoError(t, json.Unmarshal(b, &hits))
	require.Len(t, hits, 2)
	assert.Equal(t, "Rust", hits[0].Name)
	assert.True(t, hits[0].Chosen)
	require.NotNil(t, hits[0].Priority)
	assert.Equal(t, 2, *hits[0].Priority)
	assert.Equal(t, "Rusty Lake", hits[1].Name)
	assert.False(t, hits[1].Chosen)
	assert.Equal(t, twitch.BoxArtByName("Rust"), hits[0].BoxArtURL)

	_, b = g.do("GET", "/api/games/search?q=", "", nil)
	assert.JSONEq(t, `[]`, string(b))

	var names []string
	for i := 0; i < 15; i++ {
		names = append(names, "Zed "+string(rune('A'+i)))
	}
	g.seed(names...)
	_, b = g.do("GET", "/api/games/search?q=zed", "", nil)
	require.NoError(t, json.Unmarshal(b, &hits))
	assert.Len(t, hits, 10)
}

func TestGames_NeedsLink(t *testing.T) {
	g := newGamesEnv(t)
	ctx := context.Background()
	_, err := g.q.CreateAccount(ctx, gen.CreateAccountParams{
		ID: "a1", Platform: "kick", DisplayName: "kicker", Status: "active", FingerprintJson: "{}", Enabled: 1,
	})
	require.NoError(t, err)
	ids := g.seed("VALORANT")
	require.NoError(t, g.chosen.Set(ctx, ids))
	c := platform.Campaign{
		ID: "k1", Platform: "kick", Game: "VALORANT", Name: "Valo", Status: "active",
		Benefits:      []platform.DropBenefit{{ID: "b1", RequiredMinutes: 60}},
		AccountLinked: true, ClaimNeedsLink: true, AccountLinkURL: "https://auth.riotgames.com/authorize?x=1",
	}
	g.board.Observe(runner.Scan{AccountID: "a1", Platform: "kick", At: time.Now(), Campaigns: []platform.Campaign{c}})
	// A deleted account's scan is not listed.
	g.board.Observe(runner.Scan{AccountID: "gone", Platform: "kick", At: time.Now(), Campaigns: []platform.Campaign{c}})

	resp, b := g.do("GET", "/api/games", "", nil)
	require.Equal(t, 200, resp.StatusCode)
	var got gamesJSON
	require.NoError(t, json.Unmarshal(b, &got))
	require.Len(t, got.Games, 1)
	assert.Equal(t, []needsLinkJSON{{
		AccountID: "a1", AccountName: "kicker", Platform: "kick", URL: "https://auth.riotgames.com/authorize?x=1",
	}}, got.Games[0].NeedsLink)
}

func TestGames_NeedsLinkEmptyArray(t *testing.T) {
	g := newGamesEnv(t)
	ids := g.seed("Rust")
	require.NoError(t, g.chosen.Set(context.Background(), ids))
	_, b := g.do("GET", "/api/games", "", nil)
	assert.Contains(t, string(b), `"needs_link":[]`)
}

func TestGames_NextScanAt(t *testing.T) {
	t0 := time.Date(2026, 10, 25, 10, 0, 0, 0, time.UTC)
	g := newGamesEnv(t,
		runner.Snapshot{AccountID: "a1", NextScanAt: t0.Add(5 * time.Minute)},
		runner.Snapshot{AccountID: "a2", NextScanAt: t0.Add(2 * time.Minute)},
	)
	resp, b := g.do("GET", "/api/games", "", nil)
	require.Equal(t, 200, resp.StatusCode)
	var got gamesJSON
	require.NoError(t, json.Unmarshal(b, &got))
	require.NotNil(t, got.NextScanAt)
	assert.Equal(t, "2026-10-25T10:02:00Z", *got.NextScanAt)
}
