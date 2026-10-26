package api

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

type fakeKick struct {
	platform.Backend
	gotURL string
}

func (f *fakeKick) Name() string { return "kick" }
func (f *fakeKick) FetchImage(_ context.Context, u string) ([]byte, string, int, error) {
	f.gotURL = u
	return []byte("IMG"), "", http.StatusOK, nil
}

func TestImage_RejectsOtherPaths(t *testing.T) {
	e := newHealthEnv(t, nil)
	for _, u := range []string{"https://evil/x", "users/x", ""} {
		resp, _ := e.do("GET", "/api/img/kick?u="+url.QueryEscape(u), "", nil)
		assert.Equal(t, 400, resp.StatusCode, u)
	}
}

func TestImage_NoKickBackend503(t *testing.T) {
	e := newHealthEnv(t, func(d *Deps) { d.Registry = platform.NewRegistry() })
	resp, _ := e.do("GET", "/api/img/kick?u=drops/a.png", "", nil)
	assert.Equal(t, 503, resp.StatusCode)
}

func TestImage_ProxiesRewardAndAvatar(t *testing.T) {
	fk := &fakeKick{}
	e := newHealthEnv(t, func(d *Deps) {
		d.Registry = platform.NewRegistry()
		d.Registry.Register(fk)
	})
	resp, b := e.do("GET", "/api/img/kick?u="+url.QueryEscape("https://files.kick.com/drops/a.png"), "", nil)
	require.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, "IMG", string(b))
	assert.Equal(t, "image/webp", resp.Header.Get("Content-Type"))
	assert.Equal(t, "https://ext.cdn.kick.com/drops/a.png?width=384,format=webp,quality=75", fk.gotURL)

	e.do("GET", "/api/img/kick?u=/images/user/1/p.png", "", nil)
	assert.Equal(t, "https://files.kick.com/images/user/1/p.png", fk.gotURL)
}
