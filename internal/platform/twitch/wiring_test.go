package twitch

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// TestConstructors_ShareProfileRegistry guards the production constructor
// wiring: the watch keeps its OWN *client (separate cookie jar / identity /
// transport, exactly as before per-session profiles, so legacy Android
// sessions see zero request-surface change) but must share the token ->
// profile registry, or TV-bound Spade heartbeats go out under the Android
// Client-Id.
func TestConstructors_ShareProfileRegistry(t *testing.T) {
	proxied, err := NewWithProxy(&http.Transport{}, "")
	require.NoError(t, err)
	cases := map[string]*Backend{
		"New":              New(),
		"NewWithTransport": NewWithTransport(&http.Transport{}),
		"NewWithProxy":     proxied,
	}
	for name, b := range cases {
		t.Run(name, func(t *testing.T) {
			assert.NotSame(t, b.c, b.watch.c, "watch keeps its own client (legacy request surface)")
			assert.NotSame(t, b.c.http.Jar, b.watch.c.http.Jar, "watch must not share the web cookie jar")
			assert.Nil(t, b.watch.c.http.Transport, "watch client uses the default transport, as before")
			b.c.bind(platform.Session{AccessToken: "tok", ClientID: ClientTV})
			assert.Equal(t, profileTV, b.watch.c.profileForToken("tok"), "bind on b.c must be visible to the watch client")
			assert.Equal(t, profileAndroid, b.watch.c.profileForToken("other"))
		})
	}
}
