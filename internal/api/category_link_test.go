package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseCategoryLink(t *testing.T) {
	cases := []struct {
		in, plat, slug string
		ok             bool
	}{
		{"https://www.twitch.tv/directory/category/overwatch-2", "twitch", "overwatch-2", true},
		{"twitch.tv/directory/category/Overwatch-2?sort=VIEWER_COUNT", "twitch", "overwatch-2", true},
		{"https://m.twitch.tv/directory/category/just-chatting/", "twitch", "just-chatting", true},
		{"https://kick.com/category/valorant", "kick", "valorant", true},
		{"www.kick.com/category/just-chatting", "kick", "just-chatting", true},
		{"Overwatch", "", "", false},
		{"Overwatch 2", "", "", false},
		{"https://www.twitch.tv/keshaeuw", "", "", false},
		{"https://www.twitch.tv/directory/category/", "", "", false},
		{"https://evil.example/directory/category/overwatch-2", "", "", false},
		{"https://www.twitch.tv/directory/category/a%20b", "", "", false},
	}
	for _, c := range cases {
		plat, slug, ok := parseCategoryLink(c.in)
		assert.Equal(t, c.ok, ok, c.in)
		assert.Equal(t, c.plat, plat, c.in)
		assert.Equal(t, c.slug, slug, c.in)
	}
}

func TestGames_AddCategoryLinkUsesPlatformName(t *testing.T) {
	g := newGamesEnv(t)
	var gotPlat, gotSlug string
	g.s.d.LookupCategory = func(_ context.Context, plat, slug string) (string, bool, error) {
		gotPlat, gotSlug = plat, slug
		return "Overwatch", true, nil
	}
	resp, b := g.do("POST", "/api/games", g.csrf, map[string]string{"name": "https://www.twitch.tv/directory/category/overwatch-2"})
	require.Equal(t, 201, resp.StatusCode, string(b))
	assert.Equal(t, "twitch", gotPlat)
	assert.Equal(t, "overwatch-2", gotSlug)
	var got gameJSON
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, "Overwatch", got.Name)
	assert.Equal(t, []string{"g_overwatch"}, g.chosenIDs())
	assert.Equal(t, 1, g.changed)
}

func TestGames_AddCategoryLinkErrors(t *testing.T) {
	cases := []struct {
		name   string
		lookup func(context.Context, string, string) (string, bool, error)
		status int
		code   string
	}{
		{"no resolver", nil, 400, "bad_request"},
		{"unknown", func(context.Context, string, string) (string, bool, error) { return "", false, nil }, 400, "unknown_category"},
		{"lookup error", func(context.Context, string, string) (string, bool, error) { return "", false, errors.New("boom") }, 502, "lookup_failed"},
		{"no account", func(context.Context, string, string) (string, bool, error) {
			return "", false, fmt.Errorf("kick: %w", ErrNoCategoryAccount)
		}, 409, "no_account"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := newGamesEnv(t)
			g.s.d.LookupCategory = c.lookup
			resp, b := g.do("POST", "/api/games", g.csrf, map[string]string{"name": "kick.com/category/valorant"})
			require.Equal(t, c.status, resp.StatusCode, string(b))
			assert.Contains(t, string(b), c.code)
			assert.Empty(t, g.chosenIDs())
			assert.Equal(t, 0, g.changed)
		})
	}
}
