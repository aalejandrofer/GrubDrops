package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/store"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

func accountGamesEnv(t *testing.T) (*env, *calls, string) {
	t.Helper()
	e, c := accountsEnv(t)
	e.s.d.AccountGames = store.NewAccountGames(e.s.d.DB, e.s.d.Q)
	ctx := context.Background()
	for _, g := range []string{"g1", "g2"} {
		require.NoError(t, e.s.d.Q.UpsertGame(ctx, gen.UpsertGameParams{ID: g, Name: "Game " + g, Slug: "game-" + g}))
	}
	return e, c, e.createAccount("twitch", "Alpha")
}

func TestAccountGames_SetAndRead(t *testing.T) {
	e, c, id := accountGamesEnv(t)
	_, tok := e.session()

	resp, b := e.do("GET", "/api/accounts/"+id+"/games", "", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.JSONEq(t, `{"own":false,"games":[]}`, string(b))

	before := len(c.changedList())
	resp, b = e.do("PUT", "/api/accounts/"+id+"/games", tok, map[string]any{
		"own": true, "games": []map[string]string{{"id": "g2"}, {"name": "  Brand New Game "}, {"id": "g1"}},
	})
	require.Equal(t, http.StatusOK, resp.StatusCode, string(b))
	var got accountGamesJSON
	unmarshal(t, b, &got)
	require.True(t, got.Own)
	require.Len(t, got.Games, 3)
	assert.Equal(t, []string{"g2", "Brand New Game", "g1"}, []string{got.Games[0].ID, got.Games[1].Name, got.Games[2].ID})
	assert.Equal(t, []string{id}, c.changedList()[before:], "the runner reloads with the new list")

	_, b = e.do("GET", "/api/accounts", "", nil)
	assert.Contains(t, string(b), `"own_games":true,"own_game_count":3`)

	chosen, err := store.NewChosenGames(e.s.d.DB, e.s.d.Q).List(context.Background())
	require.NoError(t, err)
	assert.Empty(t, chosen, "a typed game is not added to the global list")

	// Off without games keeps the list.
	resp, _ = e.do("PUT", "/api/accounts/"+id+"/games", tok, map[string]any{"own": false})
	require.Equal(t, http.StatusOK, resp.StatusCode)
	_, b = e.do("GET", "/api/accounts/"+id+"/games", "", nil)
	unmarshal(t, b, &got)
	assert.False(t, got.Own)
	assert.Len(t, got.Games, 3)

	_, b = e.do("GET", "/api/accounts", "", nil)
	assert.Contains(t, string(b), `"own_games":false,"own_game_count":0`)
}

func TestAccountGames_TypedNameMatchesStoredRow(t *testing.T) {
	e, _, id := accountGamesEnv(t)
	_, tok := e.session()
	resp, b := e.do("PUT", "/api/accounts/"+id+"/games", tok, map[string]any{
		"own": true, "games": []map[string]string{{"name": "game g1"}},
	})
	require.Equal(t, http.StatusOK, resp.StatusCode, string(b))
	var got accountGamesJSON
	unmarshal(t, b, &got)
	require.Len(t, got.Games, 1)
	assert.Equal(t, "g1", got.Games[0].ID, "same slug reuses the stored row")
}

func TestAccountGames_Rejects(t *testing.T) {
	e, c, id := accountGamesEnv(t)
	_, tok := e.session()
	before := len(c.changedList())
	for name, body := range map[string]map[string]any{
		"unknown id": {"own": true, "games": []map[string]string{{"id": "nope"}}},
		"empty name": {"own": true, "games": []map[string]string{{"name": "  "}}},
	} {
		resp, _ := e.do("PUT", "/api/accounts/"+id+"/games", tok, body)
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode, name)
	}
	resp, _ := e.do("PUT", "/api/accounts/acc_missing/games", tok, map[string]any{"own": true})
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	a, err := e.s.d.Q.GetAccount(context.Background(), id)
	require.NoError(t, err)
	assert.EqualValues(t, 0, a.OwnGames)
	assert.Len(t, c.changedList(), before)
}
