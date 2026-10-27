package twitch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// Twitch renamed "Overwatch 2" to "Overwatch" and kept the overwatch-2
// slug, so the name-derived "overwatch" finds no directory (issue #76).
func TestDirSlug_UsesTwitchSlugAndCaches(t *testing.T) {
	var calls atomic.Int32
	srv := fakeGQL(t, map[string]func(map[string]any) string{
		"GameSlug": func(v map[string]any) string {
			calls.Add(1)
			assert.Equal(t, "515025", v["id"])
			return `{"data":{"game":{"slug":"overwatch-2"}}}`
		},
	})
	defer srv.Close()

	ch := &channels{c: newTestClient(srv.URL)}
	sess := platform.Session{AccessToken: "tok"}
	assert.Equal(t, "overwatch-2", ch.dirSlug(context.Background(), sess, "515025", "Overwatch"))
	assert.Equal(t, "overwatch-2", ch.dirSlug(context.Background(), sess, "515025", "Overwatch"))
	assert.EqualValues(t, 1, calls.Load(), "slug is cached per game")
}

func TestDirSlug_ByNameWhenNoID(t *testing.T) {
	srv := fakeGQL(t, map[string]func(map[string]any) string{
		"GameSlug": func(v map[string]any) string {
			assert.Equal(t, "overwatch 2", v["name"])
			_, hasID := v["id"]
			assert.False(t, hasID)
			return `{"data":{"game":{"slug":"overwatch-2"}}}`
		},
	})
	defer srv.Close()

	ch := &channels{c: newTestClient(srv.URL)}
	assert.Equal(t, "overwatch-2", ch.dirSlug(context.Background(), platform.Session{}, "", " overwatch 2 "))
}

func TestDirSlug_UnknownGameFallsBackToNameSlug(t *testing.T) {
	srv := fakeGQL(t, map[string]func(map[string]any) string{
		"GameSlug": func(map[string]any) string { return `{"data":{"game":null}}` },
	})
	defer srv.Close()

	ch := &channels{c: newTestClient(srv.URL)}
	assert.Equal(t, "dead-by-daylight", ch.dirSlug(context.Background(), platform.Session{}, "", "Dead by Daylight"))
}

func TestDirSlug_ErrorFallsBackAndRetries(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(`{"data":{"game":{"slug":"overwatch-2"}}}`))
	}))
	defer srv.Close()

	ch := &channels{c: newTestClient(srv.URL)}
	sess := platform.Session{AccessToken: "tok"}
	assert.Equal(t, "overwatch", ch.dirSlug(context.Background(), sess, "515025", "Overwatch"))
	require.Equal(t, "overwatch-2", ch.dirSlug(context.Background(), sess, "515025", "Overwatch"), "a failed lookup is not cached")
}

func TestListEligibleChannels_DirectoryUsesTwitchSlug(t *testing.T) {
	var dirSlug string
	srv := fakeGQL(t, map[string]func(map[string]any) string{
		"GameSlug": func(map[string]any) string { return `{"data":{"game":{"slug":"overwatch-2"}}}` },
		"DirectoryPage_Game": func(v map[string]any) string {
			dirSlug, _ = v["slug"].(string)
			return `{"data":{"game":{"streams":{"edges":[]}}}}`
		},
	})
	defer srv.Close()

	b := newForTest(srv.URL)
	camp := platform.Campaign{ID: "ow", Platform: "twitch", Game: "Overwatch", GameID: "515025"}
	_, err := b.ListEligibleChannels(context.Background(), platform.Session{AccessToken: "tok"}, camp)
	require.NoError(t, err)
	assert.Equal(t, "overwatch-2", dirSlug)
}

func TestLookupCategory_NameFromSlug(t *testing.T) {
	srv := fakeGQL(t, map[string]func(map[string]any) string{
		"GameBySlug": func(v map[string]any) string {
			if v["slug"] == "overwatch-2" {
				return `{"data":{"game":{"displayName":"Overwatch"}}}`
			}
			return `{"data":{"game":null}}`
		},
	})
	defer srv.Close()

	b := newForTest(srv.URL)
	name, ok, err := b.LookupCategory(context.Background(), platform.Session{AccessToken: "tok"}, "overwatch-2")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "Overwatch", name)

	_, ok, err = b.LookupCategory(context.Background(), platform.Session{AccessToken: "tok"}, "nope")
	require.NoError(t, err)
	assert.False(t, ok)
}
