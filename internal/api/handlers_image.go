package api

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/aalejandrofer/grubdrops/internal/i18n"
	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// imageFetcher is satisfied by the Kick backend — pulls a CDN asset over
// the utls transport, bypassing Cloudflare's hotlink 403.
type imageFetcher interface {
	FetchImage(ctx context.Context, rawURL string) ([]byte, string, int, error)
}

type imageProxyDeps struct {
	registry *platform.Registry
}

// kickCDNHost is the real Kick reward-image CDN (verified from the browser:
// ext.cdn.kick.com, NOT files.kick.com). Cloudflare-fronted, so a plain
// hotlink can 403 — we fetch it over the utls transport.
const kickCDNHost = "ext.cdn.kick.com"

// kickFilesHost serves user profile pictures (images/user/...). Unlike the
// reward-image CDN it has no resize endpoint, so we proxy these full-size.
const kickFilesHost = "files.kick.com"

// kickImageTransform mirrors Kick's own on-the-fly resize/format params so
// we serve a small webp instead of the full-size png.
const kickImageTransform = "width=384,format=webp,quality=75"

// kick proxies a Kick reward image. ?u= is the stored image value (absolute
// URL or host-relative path). We trust only the PATH and always fetch from
// ext.cdn.kick.com — robust regardless of whatever host was persisted
// (older rows stored files.kick.com, which 502s). The path must be a Kick
// reward-image path to avoid turning this into an open relay.
func (d *imageProxyDeps) kick(w http.ResponseWriter, r *http.Request) {
	lang := i18n.DetectLang(r)
	raw := strings.TrimSpace(r.URL.Query().Get("u"))
	if raw == "" {
		http.Error(w, i18n.T(lang, "error.missing_param"), http.StatusBadRequest)
		return
	}
	path := raw
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		if u, err := url.Parse(raw); err == nil {
			path = u.Path
		}
	}
	path = strings.TrimPrefix(path, "/")
	// SSRF guard: only Kick reward/drop image paths and user avatars.
	if !strings.HasPrefix(path, "drops/") && !strings.HasPrefix(path, "images/") {
		http.Error(w, i18n.T(lang, "error.bad_image_path"), http.StatusBadRequest)
		return
	}
	// User profile pictures live on files.kick.com under images/user/... and
	// are not served by the ext.cdn resize endpoint, so fetch them full-size
	// from the files host. Reward/drop images keep the ext.cdn resize path.
	var target string
	if strings.HasPrefix(path, "images/user/") || strings.HasPrefix(path, "images/avatars/") {
		target = "https://" + kickFilesHost + "/" + path
	} else {
		target = "https://" + kickCDNHost + "/" + path + "?" + kickImageTransform
	}

	b, ok := d.registry.Get("kick")
	if !ok {
		http.Error(w, i18n.T(lang, "error.kick_unavailable"), http.StatusServiceUnavailable)
		return
	}
	fetcher, ok := b.(imageFetcher)
	if !ok {
		http.Error(w, i18n.T(lang, "error.kick_no_image"), http.StatusServiceUnavailable)
		return
	}

	data, ct, status, err := fetcher.FetchImage(r.Context(), target)
	if err != nil || status != http.StatusOK {
		http.Error(w, i18n.T(lang, "error.image_fetch_failed"), http.StatusBadGateway)
		return
	}
	if ct == "" {
		ct = "image/webp"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}
