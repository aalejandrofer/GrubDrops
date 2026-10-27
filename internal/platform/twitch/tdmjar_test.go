package twitch

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const tdmFixtureToken = "abcdefghijklmnopqrstuvwxyz0123"

func TestTokenFromTDMJar_Formats(t *testing.T) {
	for _, f := range []string{
		"tdm_cookies_json.jar",       // aiohttp JSON, "domain|path" keys
		"tdm_cookies_pickle.jar",     // pickle protocol 5, (domain, path) keys
		"tdm_cookies_pickle_old.jar", // pickle protocol 4, bare domain keys
	} {
		t.Run(f, func(t *testing.T) {
			b, err := os.ReadFile("testdata/" + f)
			require.NoError(t, err)
			tok, err := TokenFromTDMJar(b)
			require.NoError(t, err)
			assert.Equal(t, tdmFixtureToken, tok)
		})
	}
}

func TestTokenFromTDMJar_NoToken(t *testing.T) {
	for name, in := range map[string]string{
		"empty":        "",
		"garbage":      "not a cookie jar",
		"json other":   `{"www.twitch.tv|/": {"unique_id": {"value": "x"}}}`,
		"json foreign": `{"example.com|/": {"auth-token": {"value": "` + tdmFixtureToken + `"}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := TokenFromTDMJar([]byte(in))
			assert.ErrorIs(t, err, ErrNoAccessToken)
		})
	}
}

// A pickle that names any class outside the cookie jar's three is refused,
// so a crafted file cannot reach other constructors.
func TestTokenFromTDMJar_RejectsOtherClasses(t *testing.T) {
	// PROTO 2, GLOBAL "os system", EMPTY_TUPLE, REDUCE, STOP.
	evil := []byte("\x80\x02cos\nsystem\n)R.")
	_, err := TokenFromTDMJar(evil)
	assert.ErrorIs(t, err, ErrNoAccessToken)
}
