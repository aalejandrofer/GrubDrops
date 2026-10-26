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
	ct     string
}

func (f *fakeKick) Name() string { return "kick" }
func (f *fakeKick) FetchImage(_ context.Context, u string) ([]byte, string, int, error) {
	f.gotURL = u
	return []byte("IMG"), f.ct, http.StatusOK, nil
}

func TestImage_RejectsOtherPaths(t *testing.T) {
	e := newHealthEnv(t, nil)
	for _, u := range []string{"https://evil/x", "users/x", ""} {
		resp, _ := e.do("GET", "/api/img/kick?u="+url.QueryEscape(u), "", nil)
		assert.Equal(t, 400, resp.StatusCode, u)
	}
}

func TestImage_NoKickBackend502(t *testing.T) {
	e := newHealthEnv(t, func(d *Deps) { d.Registry = platform.NewRegistry() })
	resp, b := e.do("GET", "/api/img/kick?u=drops/a.png", "", nil)
	assert.Equal(t, 502, resp.StatusCode)
	assert.JSONEq(t, `{"error":"upstream"}`, string(b))
}

func TestImage_ProxiesRewardAndAvatar(t *testing.T) {
	fk := &fakeKick{ct: "image/webp"}
	e := newHealthEnv(t, func(d *Deps) {
		d.Registry = platform.NewRegistry()
		d.Registry.Register(fk)
	})
	resp, b := e.do("GET", "/api/img/kick?u="+url.QueryEscape("https://files.kick.com/drops/a.png"), "", nil)
	require.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, "IMG", string(b))
	assert.Equal(t, "image/webp", resp.Header.Get("Content-Type"))
	assert.Equal(t, "nosniff", resp.Header.Get("X-Content-Type-Options"))
	assert.Equal(t, "https://ext.cdn.kick.com/drops/a.png?width=384,format=webp,quality=75", fk.gotURL)

	e.do("GET", "/api/img/kick?u=/images/user/1/p.png", "", nil)
	assert.Equal(t, "https://files.kick.com/images/user/1/p.png", fk.gotURL)

	cover := "https://files.kick.com/images/subcategories/14138/banner/b049272d-6811-4c3d-b19c-8f6cc170c5ac"
	resp, _ = e.do("GET", "/api/img/kick?u="+url.QueryEscape(cover), "", nil)
	require.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, cover, fk.gotURL, "category covers come from files.kick.com")
}

func TestImage_EmptyContentTypeIsWebp(t *testing.T) {
	fk := &fakeKick{}
	e := newHealthEnv(t, func(d *Deps) {
		d.Registry = platform.NewRegistry()
		d.Registry.Register(fk)
	})
	resp, _ := e.do("GET", "/api/img/kick?u=drops/a.png", "", nil)
	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, "image/webp", resp.Header.Get("Content-Type"))
}

func TestImage_NonImageContentType502(t *testing.T) {
	for _, ct := range []string{"text/html", "application/json"} {
		fk := &fakeKick{ct: ct}
		e := newHealthEnv(t, func(d *Deps) {
			d.Registry = platform.NewRegistry()
			d.Registry.Register(fk)
		})
		resp, b := e.do("GET", "/api/img/kick?u=drops/a.png", "", nil)
		assert.Equal(t, 502, resp.StatusCode, ct)
		assert.JSONEq(t, `{"error":"upstream"}`, string(b))
	}
}
