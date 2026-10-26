package api

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// imageFetchTimeout bounds the upstream fetch.
const imageFetchTimeout = 15 * time.Second

// imageFetcher is satisfied by the Kick backend: it pulls a CDN asset over the
// utls transport, bypassing Cloudflare's hotlink 403.
type imageFetcher interface {
	FetchImage(ctx context.Context, rawURL string) ([]byte, string, int, error)
}

const (
	// kickCDNHost serves reward images (ext.cdn.kick.com, not files.kick.com).
	kickCDNHost = "ext.cdn.kick.com"
	// kickFilesHost serves profile pictures and category covers, full size only.
	kickFilesHost = "files.kick.com"
	// kickImageTransform mirrors Kick's resize params: a small webp.
	kickImageTransform = "width=384,format=webp,quality=75"
)

// getKickImage proxies a Kick image. ?u= is the stored image value (absolute
// URL or host-relative path). Only the path is trusted; the host is always
// chosen here, and the path must be a reward, avatar or category path, so this is not an
// open relay.
func (s *server) getKickImage(w http.ResponseWriter, r *http.Request) {
	raw := strings.TrimSpace(r.URL.Query().Get("u"))
	if raw == "" {
		writeErr(w, http.StatusBadRequest, "bad_request")
		return
	}
	path := raw
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		if u, err := url.Parse(raw); err == nil {
			path = u.Path
		}
	}
	path = strings.TrimPrefix(path, "/")
	if !strings.HasPrefix(path, "drops/") && !strings.HasPrefix(path, "images/") {
		writeErr(w, http.StatusBadRequest, "bad_request")
		return
	}
	var target string
	if strings.HasPrefix(path, "images/user/") || strings.HasPrefix(path, "images/avatars/") ||
		strings.HasPrefix(path, "images/subcategories/") {
		target = "https://" + kickFilesHost + "/" + path
	} else {
		target = "https://" + kickCDNHost + "/" + path + "?" + kickImageTransform
	}

	if s.d.Registry == nil {
		writeErr(w, http.StatusBadGateway, "upstream")
		return
	}
	b, ok := s.d.Registry.Get("kick")
	if !ok {
		writeErr(w, http.StatusBadGateway, "upstream")
		return
	}
	fetcher, ok := b.(imageFetcher)
	if !ok {
		writeErr(w, http.StatusBadGateway, "upstream")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), imageFetchTimeout)
	defer cancel()
	data, ct, status, err := fetcher.FetchImage(ctx, target)
	if ct == "" {
		ct = "image/webp" // 1.x default for a missing type
	}
	if err != nil || status != http.StatusOK || !strings.HasPrefix(strings.ToLower(ct), "image/") {
		writeErr(w, http.StatusBadGateway, "upstream")
		return
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}
