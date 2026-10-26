package kick

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

func TestCatalog_ActiveCampaignsWithRewards(t *testing.T) {
	f := &fakeDoer{resp: map[string]fakeResp{
		"https://web.kick.com/api/v1/drops/campaigns": {200, `{"data":[
			{"id":"c-rust","category":{"banner_image_url":"","id":13,"image_url":"https://files.kick.com/images/subcategories/13/banner/coajjj.jpg","name":"Rust","slug":"rust"},"name":"Rust Drops","status":"active",
			 "ends_at":"2099-01-01T00:00:00Z","connect_url":"https://example.com/link",
			 "rewards":[{"id":"r1","name":"Crate","required_units":90,"image_url":"drops/reward-image/a.png"}]},
			{"id":"c-old","game":"Rust","name":"Old","status":"expired"},
			{"id":"c-soon","game":"Rust","name":"Soon","starts_at":"2099-01-01T00:00:00Z"}
		]}`},
	}}
	b := withFake(f)

	camps, err := b.Catalog(context.Background(), platform.Session{})
	require.NoError(t, err)
	require.Len(t, camps, 1)
	c := camps[0]
	assert.Equal(t, "Rust", c.Game)
	assert.Equal(t, "https://files.kick.com/images/subcategories/13/banner/coajjj.jpg", c.BoxArtURL, "category image")
	assert.Equal(t, "active", c.Status)
	assert.Equal(t, "https://example.com/link", c.AccountLinkURL)
	require.Len(t, c.Benefits, 1)
	assert.Equal(t, 90, c.Benefits[0].RequiredMinutes)
	assert.Equal(t, "https://ext.cdn.kick.com/drops/reward-image/a.png", c.Benefits[0].ImageURL)
	assert.Empty(t, b.campaignChannels, "catalog must not touch the mining caches")
}

func TestGameImage(t *testing.T) {
	cases := []struct {
		name string
		m    map[string]any
		want string
	}{
		{"category image_url", map[string]any{"category": map[string]any{"image_url": "https://files.kick.com/images/subcategories/14138/banner/b049272d-6811-4c3d-b19c-8f6cc170c5ac", "banner_image_url": ""}},
			"https://files.kick.com/images/subcategories/14138/banner/b049272d-6811-4c3d-b19c-8f6cc170c5ac"},
		{"banner when no image", map[string]any{"category": map[string]any{"image_url": "", "banner_image_url": "https://files.kick.com/images/subcategories/5/banner/coc99o.jpg"}},
			"https://files.kick.com/images/subcategories/5/banner/coc99o.jpg"},
		{"no category", map[string]any{"category": nil, "game": "Rust"}, ""},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, gameImage(tc.m), tc.name)
	}
}

// anonDoer records whether each request was marked anonymous.
type anonDoer struct {
	fakeDoer
	anon []bool
}

func (d *anonDoer) do(ctx context.Context, s platform.Session, method, path string, body []byte) ([]byte, int, error) {
	d.anon = append(d.anon, isAnonymous(ctx))
	return d.fakeDoer.do(ctx, s, method, path, body)
}

func TestCatalog_AnonymousOnlyWithoutCookies(t *testing.T) {
	d := &anonDoer{fakeDoer: fakeDoer{resp: map[string]fakeResp{
		"https://web.kick.com/api/v1/drops/campaigns": {200, `{"data":[]}`},
	}}}
	b := withFake(&fakeDoer{})
	b.api = &api{d: d}

	_, err := b.Catalog(context.Background(), platform.Session{})
	require.NoError(t, err)
	authed := platform.Session{Cookies: map[string]string{"kick": `{"cookies":[{"name":"session_token","value":"1|x"}]}`}}
	_, err = b.Catalog(context.Background(), authed)
	require.NoError(t, err)
	assert.Equal(t, []bool{true, false}, d.anon)
}

func TestHTTPDoer_NoCookiesNeedsAnonymous(t *testing.T) {
	_, _, err := newHTTPDoer(nil).do(context.Background(), platform.Session{}, "GET", "https://web.kick.com/x", nil)
	assert.ErrorContains(t, err, "no cookies")
}

func TestCatalog_Error(t *testing.T) {
	b := withFake(&fakeDoer{resp: map[string]fakeResp{}})
	_, err := b.Catalog(context.Background(), platform.Session{})
	assert.Error(t, err)
}
