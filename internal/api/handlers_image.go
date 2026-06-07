package api

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/aalejandrofer/dropsminer/internal/platform"
)

// imageFetcher is satisfied by the Kick backend — pulls a CDN asset over
// the utls transport, bypassing Cloudflare's hotlink 403.
type imageFetcher interface {
	FetchImage(ctx context.Context, rawURL string) ([]byte, string, int, error)
}

type imageProxyDeps struct {
	registry *platform.Registry
}

// allowedImageHosts gates the proxy to Kick's asset hosts so it can't be
// turned into an open SSRF relay.
var allowedImageHosts = map[string]bool{
	"files.kick.com":  true,
	"images.kick.com": true,
	"assets.kick.com": true,
}

const kickFilesBaseAPI = "https://files.kick.com/"

// kick proxies a Kick reward image. ?u= is the stored image value (an
// absolute files.kick.com URL or a host-relative path). Relative paths are
// resolved against files.kick.com; the resolved host must be on the
// allow-list. The bytes are streamed back with a long cache TTL.
func (d *imageProxyDeps) kick(w http.ResponseWriter, r *http.Request) {
	raw := strings.TrimSpace(r.URL.Query().Get("u"))
	if raw == "" {
		http.Error(w, "missing u", http.StatusBadRequest)
		return
	}
	if !strings.HasPrefix(raw, "http://") && !strings.HasPrefix(raw, "https://") {
		raw = kickFilesBaseAPI + strings.TrimPrefix(raw, "/")
	}
	u, err := url.Parse(raw)
	if err != nil || !allowedImageHosts[u.Host] {
		http.Error(w, "bad image url", http.StatusBadRequest)
		return
	}

	b, ok := d.registry.Get("kick")
	if !ok {
		http.Error(w, "kick backend unavailable", http.StatusServiceUnavailable)
		return
	}
	fetcher, ok := b.(imageFetcher)
	if !ok {
		http.Error(w, "kick backend cannot fetch images", http.StatusServiceUnavailable)
		return
	}

	data, ct, status, err := fetcher.FetchImage(r.Context(), raw)
	if err != nil || status != http.StatusOK {
		// Upstream miss — let the browser fall back to its broken-image
		// placeholder rather than caching a bad body.
		http.Error(w, "image fetch failed", http.StatusBadGateway)
		return
	}
	if ct == "" {
		ct = "image/png"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}
