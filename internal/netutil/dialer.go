// internal/netutil/dialer.go
package netutil

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"

	"golang.org/x/net/proxy"
)

// DialContextFunc dials a TCP connection to addr, optionally tunneled through a
// proxy. The returned conn speaks directly to the target host, so callers layer
// utls/TLS/HTTP on top exactly as they would for a direct connection.
type DialContextFunc func(ctx context.Context, network, addr string) (net.Conn, error)

// ProxyDialer returns a dialer for proxyURL. Empty → direct. socks5:// →
// golang.org/x/net/proxy. http(s):// → HTTP CONNECT tunnel. Unsupported schemes
// return an error so startup can surface a misconfiguration.
func ProxyDialer(proxyURL string) (DialContextFunc, error) {
	base := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
	if proxyURL == "" {
		return base.DialContext, nil
	}
	parsed, err := url.Parse(proxyURL)
	if err != nil {
		return nil, fmt.Errorf("parse proxy url: %w", err)
	}
	switch parsed.Scheme {
	case "socks5":
		return func(ctx context.Context, network, addr string) (net.Conn, error) {
			d, err := proxy.SOCKS5("tcp", parsed.Host, nil, base)
			if err != nil {
				return nil, fmt.Errorf("socks5 dialer: %w", err)
			}
			if cd, ok := d.(proxy.ContextDialer); ok {
				return cd.DialContext(ctx, network, addr)
			}
			// Fallback: run blocking Dial with ctx cancellation.
			type res struct {
				c net.Conn
				e error
			}
			ch := make(chan res, 1)
			go func() { c, e := d.Dial(network, addr); ch <- res{c, e} }()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case r := <-ch:
				return r.c, r.e
			}
		}, nil
	case "http", "https":
		return func(ctx context.Context, network, addr string) (net.Conn, error) {
			conn, err := base.DialContext(ctx, "tcp", parsed.Host)
			if err != nil {
				return nil, fmt.Errorf("dial proxy: %w", err)
			}
			req := &http.Request{
				Method: http.MethodConnect,
				URL:    &url.URL{Opaque: addr},
				Host:   addr,
				Header: make(http.Header),
			}
			if parsed.User != nil {
				if pw, ok := parsed.User.Password(); ok {
					req.SetBasicAuth(parsed.User.Username(), pw)
				}
			}
			if err := req.Write(conn); err != nil {
				conn.Close()
				return nil, fmt.Errorf("write CONNECT: %w", err)
			}
			br := bufio.NewReader(conn)
			resp, err := http.ReadResponse(br, req)
			if err != nil {
				conn.Close()
				return nil, fmt.Errorf("read CONNECT response: %w", err)
			}
			if resp.StatusCode != http.StatusOK {
				conn.Close()
				return nil, fmt.Errorf("proxy CONNECT failed: %s", resp.Status)
			}
			return conn, nil
		}, nil
	default:
		return nil, fmt.Errorf("unsupported proxy scheme %q", parsed.Scheme)
	}
}
