package kick

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKickBackend_LookupCategory(t *testing.T) {
	b := withFake(&fakeDoer{resp: map[string]fakeResp{
		"https://kick.com/api/v1/subcategories/valorant": {200, `{"id":64,"name":"VALORANT","slug":"valorant"}`},
		"https://kick.com/api/v1/subcategories/broken":   {500, `{}`},
	}})

	name, ok, err := b.LookupCategory(context.Background(), sess("a"), "valorant")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "VALORANT", name)

	_, ok, err = b.LookupCategory(context.Background(), sess("a"), "nope")
	require.NoError(t, err, "404 is not found, not an error")
	assert.False(t, ok)

	_, _, err = b.LookupCategory(context.Background(), sess("a"), "broken")
	require.Error(t, err)
}
