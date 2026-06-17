package kick

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	neturl "net/url"
	"strings"
	"sync"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/platform"
	utls "github.com/refraction-networking/utls"
	"golang.org/x/net/http2"
)

// Kick serves different features from different hosts (from the Next.js config):
//   - web.kick.com  : the drops API (/api/v1/drops/*)
//   - kick.com      : channel/category discovery (/api/v2/channels, /stream/livestreams) + watch ping
//
// do() dials whatever host the passed URL names.
const (
	dropsBase     = "https://web.kick.com"
	discoveryBase = "https://kick.com"
)

// chromeUA matches the TLS fingerprint we present (utls HelloChrome). Kick's
// Cloudflare bot-management 403s Go's default TLS fingerprint and any
// CDP-driven browser, but accepts a real Chrome JA3 + HTTP/2 fingerprint from a
// pure-HTTP client (see project_kick_breakthrough_utls memory / kick_issues.md).
const chromeUA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"

// httpDoer performs Kick API calls over a Chrome-fingerprinted utls/HTTP2
// connection. Connections are pooled per host to avoid repeated TLS handshakes.
type httpDoer struct {
	timeout time.Duration
	mu      sync.Mutex
	conns   map[string]*cachedConn // host -> cached connection
}

type cachedConn struct {
	uconn   *utls.UConn
	tr      *http2.Transport
	cc      *http2.ClientConn
	lastUse time.Time
}

func newHTTPDoer() *httpDoer {
	return &httpDoer{
		timeout: 20 * time.Second,
		conns:   map[string]*cachedConn{},
	}
}

// connFor returns a cached connection for the host, or creates a new one.
func (d *httpDoer) connFor(ctx context.Context, host string) (*cachedConn, error) {
	d.mu.Lock()
	if cc, ok := d.conns[host]; ok && time.Since(cc.lastUse) < 5*time.Minute {
		cc.lastUse = time.Now()
		d.mu.Unlock()
		return cc, nil
	}
	d.mu.Unlock()

	dialCtx, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()
	var dialer net.Dialer
	tcp, err := dialer.DialContext(dialCtx, "tcp", host+":443")
	if err != nil {
		return nil, fmt.Errorf("dial: %w", err)
	}
	uconn := utls.UClient(tcp, &utls.Config{ServerName: host, NextProtos: []string{"h2", "http/1.1"}}, utls.HelloChrome_Auto)
	if err := uconn.HandshakeContext(dialCtx); err != nil {
		uconn.Close()
		return nil, fmt.Errorf("tls handshake: %w", err)
	}
	if uconn.ConnectionState().NegotiatedProtocol != "h2" {
		uconn.Close()
		return nil, fmt.Errorf("kick did not negotiate h2")
	}

	tr := &http2.Transport{}
	cc, err := tr.NewClientConn(uconn)
	if err != nil {
		uconn.Close()
		return nil, fmt.Errorf("h2 clientconn: %w", err)
	}

	cached := &cachedConn{
		uconn:   uconn,
		tr:      tr,
		cc:      cc,
		lastUse: time.Now(),
	}

	d.mu.Lock()
	d.conns[host] = cached
	d.mu.Unlock()

	return cached, nil
}

// closeConn closes and removes a cached connection.
func (d *httpDoer) closeConn(host string) {
	d.mu.Lock()
	if cc, ok := d.conns[host]; ok {
		cc.uconn.Close()
		cc.tr.CloseIdleConnections()
		delete(d.conns, host)
	}
	d.mu.Unlock()
}

// getRaw fetches a URL with the Chrome utls fingerprint but NO session
// cookies — used to pull Kick CDN assets (reward images on files.kick.com),
// which are public but 403 Go's default TLS fingerprint / plain hotlinks.
// Returns the body, Content-Type, and status. Sends image-request headers.
func (d *httpDoer) getRaw(ctx context.Context, rawURL string) ([]byte, string, int, error) {
	u, err := neturl.Parse(rawURL)
	if err != nil || u.Host == "" {
		return nil, "", 0, fmt.Errorf("bad url %q", rawURL)
	}
	host := u.Host

	cached, err := d.connFor(ctx, host)
	if err != nil {
		return nil, "", 0, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", 0, err
	}
	req.Header.Set("User-Agent", chromeUA)
	req.Header.Set("Accept", "image/avif,image/webp,image/apng,image/*,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Referer", "https://kick.com/")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	req.Header.Set("Sec-Fetch-Mode", "no-cors")
	req.Header.Set("Sec-Fetch-Dest", "image")

	resp, err := cached.cc.RoundTrip(req)
	if err != nil {
		d.closeConn(host)
		return nil, "", 0, fmt.Errorf("roundtrip: %w", err)
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", resp.StatusCode, fmt.Errorf("read body: %w", err)
	}
	return out, resp.Header.Get("Content-Type"), resp.StatusCode, nil
}

// do sends method+url with the session's cookies and returns the body + status.
// url is a full URL (e.g. https://web.kick.com/api/v1/drops/campaigns); the host
// is dialed from it so drops (web.kick.com) and discovery (kick.com) both work.
func (d *httpDoer) do(ctx context.Context, sess platform.Session, method, rawURL string, body []byte) ([]byte, int, error) {
	ks, err := decodeSession(sess)
	if err != nil {
		return nil, 0, fmt.Errorf("decode kick session: %w", err)
	}
	cookieHeader, xsrf, bearer := cookieHeaderFor(ks)
	if cookieHeader == "" {
		return nil, 0, fmt.Errorf("kick session has no cookies")
	}
	u, err := neturl.Parse(rawURL)
	if err != nil || u.Host == "" {
		return nil, 0, fmt.Errorf("bad url %q", rawURL)
	}
	host := u.Host

	cached, err := d.connFor(ctx, host)
	if err != nil {
		return nil, 0, err
	}

	var bodyRdr io.Reader
	if body != nil {
		bodyRdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, bodyRdr)
	if err != nil {
		return nil, 0, err
	}
	ua := ks.UserAgent
	if ua == "" {
		ua = chromeUA
	}
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Cookie", cookieHeader)
	req.Header.Set("Referer", "https://kick.com/")
	req.Header.Set("Origin", "https://kick.com")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Sec-Fetch-Dest", "empty")
	if xsrf != "" {
		req.Header.Set("X-XSRF-TOKEN", xsrf)
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := cached.cc.RoundTrip(req)
	if err != nil {
		d.closeConn(host)
		return nil, 0, fmt.Errorf("roundtrip: %w", err)
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read body: %w", err)
	}
	return out, resp.StatusCode, nil
}

// cookieHeaderFor builds the Cookie header (all session cookies) and returns the
// XSRF token (for X-XSRF-TOKEN) and the Sanctum bearer token (from the
// session_token cookie "id|token" → token, for Authorization: Bearer).
func cookieHeaderFor(ks kickSession) (cookieHeader, xsrf, bearer string) {
	var pairs []string
	for _, c := range ks.Cookies {
		if c.Value == "" {
			continue
		}
		pairs = append(pairs, c.Name+"="+c.Value)
		switch c.Name {
		case "XSRF-TOKEN":
			xsrf = c.Value
		case "session_token":
			// Kick's web app sends Authorization: Bearer = the ENTIRE
			// session_token (Laravel Sanctum "id|token"), NOT just the part
			// after "|". Sending only the tail 403s authed endpoints like
			// /drops/progress (verified via live DevTools capture 2026-06-12).
			bearer = strings.ReplaceAll(c.Value, "%7C", "|")
		}
	}
	if xsrf == "" {
		xsrf = ks.XSRFToken
	}
	return strings.Join(pairs, "; "), xsrf, bearer
}
