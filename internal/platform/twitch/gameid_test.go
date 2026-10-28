package twitch

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// Games added by name (often from Kick) have no Twitch id until a Twitch
// campaign names them, so their box art fell back to the name-keyed URL,
// which IGDB games never answer (GRUB-33).
func TestLookupGameID_ByName(t *testing.T) {
	srv := fakeGQL(t, map[string]func(map[string]any) string{
		"GameID": func(v map[string]any) string {
			assert.Equal(t, "Battlefield 6", v["name"])
			return `{"data":{"game":{"id":"168648543","name":"Battlefield 6"}}}`
		},
	})
	defer srv.Close()

	id, name, ok, err := newForTest(srv.URL).LookupGameID(context.Background(), platform.Session{AccessToken: "tok"}, " Battlefield 6 ")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "168648543", id)
	assert.Equal(t, "Battlefield 6", name)
}

// Twitch renamed "Overwatch 2" to "Overwatch" and still answers the old
// name, so the lookup reports Twitch's current name for matching (#78).
func TestLookupGameID_ReportsTwitchName(t *testing.T) {
	srv := fakeGQL(t, map[string]func(map[string]any) string{
		"GameID": func(map[string]any) string { return `{"data":{"game":{"id":"515025","name":"Overwatch"}}}` },
	})
	defer srv.Close()

	id, name, ok, err := newForTest(srv.URL).LookupGameID(context.Background(), platform.Session{AccessToken: "tok"}, "Overwatch 2")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "515025", id)
	assert.Equal(t, "Overwatch", name)
}

// Kick says "Counter-Strike 2", Twitch says "Counter-Strike": the exact
// name misses, so the search picks the hit the name extends by a number.
func TestLookupGameID_SearchFallback(t *testing.T) {
	srv := fakeGQL(t, map[string]func(map[string]any) string{
		"GameID": func(map[string]any) string { return `{"data":{"game":null}}` },
		"GameSearch": func(v map[string]any) string {
			assert.Equal(t, "Counter-Strike 2", v["query"])
			return `{"data":{"searchCategories":{"edges":[
				{"node":{"id":"495359","name":"Counter-Strike Online 2"}},
				{"node":{"id":"32399","name":"Counter-Strike"}}]}}}`
		},
	})
	defer srv.Close()

	id, name, ok, err := newForTest(srv.URL).LookupGameID(context.Background(), platform.Session{AccessToken: "tok"}, "Counter-Strike 2")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "32399", id)
	assert.Equal(t, "Counter-Strike", name)
}

func TestLookupGameID_UnknownGame(t *testing.T) {
	srv := fakeGQL(t, map[string]func(map[string]any) string{
		"GameID": func(map[string]any) string { return `{"data":{"game":null}}` },
		"GameSearch": func(map[string]any) string {
			return `{"data":{"searchCategories":{"edges":[{"node":{"id":"1","name":"Rust"}}]}}}`
		},
	})
	defer srv.Close()

	_, _, ok, err := newForTest(srv.URL).LookupGameID(context.Background(), platform.Session{AccessToken: "tok"}, "Trust Me")
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestSearchHitFor(t *testing.T) {
	hits := []searchHit{{ID: "a", Name: "Diablo IV"}, {ID: "b", Name: "Overwatch"}, {ID: "c", Name: "Overwatch 2 Lore"}}
	for name, want := range map[string]string{
		"overwatch":       "b", // exact, any case
		"Overwatch 2":     "b", // name plus a trailing number
		"Overwatch: 2":    "",  // not a bare number
		"Overwatchers 2":  "",  // not a word boundary
		"Diablo 4":        "",  // Twitch spells it IV
		"Overwatch 2 Pro": "",
	} {
		got, ok := searchHitFor(name, hits)
		assert.Equal(t, want != "", ok, name)
		assert.Equal(t, want, got.ID, name)
	}
}
