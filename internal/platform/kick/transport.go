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
	"time"

	"github.com/aalejandrofer/dropsminer/internal/platform"
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
// connection. One connection per request (the drops call rate is low). No CDP,
// no headless browser — that's the whole point.
type httpDoer struct {
	timeout time.Duration
}

func newHTTPDoer() *httpDoer { return &httpDoer{timeout: 20 * time.Second} }

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

	dialCtx, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()
	var dialer net.Dialer
	tcp, err := dialer.DialContext(dialCtx, "tcp", host+":443")
	if err != nil {
		return nil, "", 0, fmt.Errorf("dial: %w", err)
	}
	uconn := utls.UClient(tcp, &utls.Config{ServerName: host, NextProtos: []string{"h2", "http/1.1"}}, utls.HelloChrome_Auto)
	if err := uconn.HandshakeContext(dialCtx); err != nil {
		uconn.Close()
		return nil, "", 0, fmt.Errorf("tls handshake: %w", err)
	}
	if uconn.ConnectionState().NegotiatedProtocol != "h2" {
		uconn.Close()
		return nil, "", 0, fmt.Errorf("kick cdn did not negotiate h2")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		uconn.Close()
		return nil, "", 0, err
	}
	req.Header.Set("User-Agent", chromeUA)
	req.Header.Set("Accept", "image/avif,image/webp,image/apng,image/*,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Referer", "https://kick.com/")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	req.Header.Set("Sec-Fetch-Mode", "no-cors")
	req.Header.Set("Sec-Fetch-Dest", "image")

	tr := &http2.Transport{}
	cc, err := tr.NewClientConn(uconn)
	if err != nil {
		uconn.Close()
		return nil, "", 0, fmt.Errorf("h2 clientconn: %w", err)
	}
	resp, err := cc.RoundTrip(req)
	if err != nil {
		uconn.Close()
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

	dialCtx, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()
	var dialer net.Dialer
	tcp, err := dialer.DialContext(dialCtx, "tcp", host+":443")
	if err != nil {
		return nil, 0, fmt.Errorf("dial: %w", err)
	}
	uconn := utls.UClient(tcp, &utls.Config{ServerName: host, NextProtos: []string{"h2", "http/1.1"}}, utls.HelloChrome_Auto)
	if err := uconn.HandshakeContext(dialCtx); err != nil {
		uconn.Close()
		return nil, 0, fmt.Errorf("tls handshake: %w", err)
	}
	if uconn.ConnectionState().NegotiatedProtocol != "h2" {
		uconn.Close()
		return nil, 0, fmt.Errorf("kick did not negotiate h2")
	}

	var bodyRdr io.Reader
	if body != nil {
		bodyRdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, bodyRdr)
	if err != nil {
		uconn.Close()
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

	tr := &http2.Transport{}
	cc, err := tr.NewClientConn(uconn)
	if err != nil {
		uconn.Close()
		return nil, 0, fmt.Errorf("h2 clientconn: %w", err)
	}
	resp, err := cc.RoundTrip(req)
	if err != nil {
		uconn.Close()
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
			st := strings.ReplaceAll(c.Value, "%7C", "|")
			if i := strings.IndexByte(st, '|'); i >= 0 {
				bearer = st[i+1:]
			} else {
				bearer = st
			}
		}
	}
	if xsrf == "" {
		xsrf = ks.XSRFToken
	}
	return strings.Join(pairs, "; "), xsrf, bearer
}
