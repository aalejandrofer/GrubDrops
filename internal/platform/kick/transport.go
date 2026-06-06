package kick

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	utls "github.com/refraction-networking/utls"
	"github.com/aalejandrofer/dropsminer/internal/platform"
	"golang.org/x/net/http2"
)

// kickHost is the API origin. All drops/channel endpoints live here.
const kickHost = "kick.com"

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

// do sends method+path to kick.com with the session's cookies and returns the
// body + status. path is an absolute path like "/api/v1/drops/campaigns".
func (d *httpDoer) do(ctx context.Context, sess platform.Session, method, path string, body []byte) ([]byte, int, error) {
	ks, err := decodeSession(sess)
	if err != nil {
		return nil, 0, fmt.Errorf("decode kick session: %w", err)
	}
	cookieHeader, xsrf := cookieHeaderFor(ks)
	if cookieHeader == "" {
		return nil, 0, fmt.Errorf("kick session has no cookies")
	}

	dialCtx, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()
	var dialer net.Dialer
	tcp, err := dialer.DialContext(dialCtx, "tcp", kickHost+":443")
	if err != nil {
		return nil, 0, fmt.Errorf("dial: %w", err)
	}
	uconn := utls.UClient(tcp, &utls.Config{ServerName: kickHost, NextProtos: []string{"h2", "http/1.1"}}, utls.HelloChrome_Auto)
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
	req, err := http.NewRequestWithContext(ctx, method, "https://"+kickHost+path, bodyRdr)
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

// cookieHeaderFor builds the Cookie header (kick_session + XSRF-TOKEN, plus any
// others present) and returns the XSRF token value for the X-XSRF-TOKEN header.
func cookieHeaderFor(ks kickSession) (string, string) {
	var pairs []string
	var xsrf string
	for _, c := range ks.Cookies {
		if c.Value == "" {
			continue
		}
		pairs = append(pairs, c.Name+"="+c.Value)
		if c.Name == "XSRF-TOKEN" {
			xsrf = c.Value
		}
	}
	if xsrf == "" {
		xsrf = ks.XSRFToken
	}
	return strings.Join(pairs, "; "), xsrf
}
