package kick

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// probeLive checks at most 12 channels per discovery, however large the
// campaign's pool: a big campaign must not fan out into one livestream
// request per participating channel. A live 13th channel is not reached.
func TestKickBackend_ProbeLiveCapsAtTwelve(t *testing.T) {
	f := &fakeDoer{resp: map[string]fakeResp{}}
	var pool []kickChannel
	for i := 1; i <= 20; i++ {
		slug := fmt.Sprintf("channel-%02d", i)
		body := `{"data":null}`
		if i == 13 {
			body = `{"data":{"id":13,"viewer_count":5,"categories":[{"name":"Rust","slug":"rust"}]}}`
		}
		f.resp["https://kick.com/api/v2/channels/"+slug+"/livestream"] = fakeResp{200, body}
		pool = append(pool, kickChannel{Slug: slug, ID: fmt.Sprint(i)})
	}
	b := withFake(f)
	b.campaignChannels["c1"] = pool
	out, err := b.ListEligibleChannels(context.Background(), sess("acc1"), platform.Campaign{ID: "c1", Game: "Rust"})
	require.NoError(t, err)
	assert.Empty(t, out, "the live channel past the cap is not probed")
	probes := 0
	for _, c := range f.calls {
		if strings.Contains(c.path, "/api/v2/channels/") && strings.HasSuffix(c.path, "/livestream") {
			probes++
		}
	}
	assert.Equal(t, 12, probes)
}
