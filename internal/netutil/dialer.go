// internal/netutil/dialer.go
package netutil

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
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

// proxyRoots verifies an https:// proxy's certificate; nil uses the system
// roots. Tests swap it.
var proxyRoots *x509.CertPool

// socksAuth is the user:pass in a socks5:// URL, or nil without one.
func socksAuth(u *url.URL) *proxy.Auth {
	if u.User == nil {
		return nil
	}
	pw, _ := u.User.Password()
	return &proxy.Auth{User: u.User.Username(), Password: pw}
}

// ProxyDialer returns a dialer for proxyURL. Empty → direct. socks5:// →
// golang.org/x/net/proxy. http(s):// → HTTP CONNECT tunnel, over TLS to the
// proxy for https://. Unsupported schemes return an error so startup can
// surface a misconfiguration.
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
			d, err := proxy.SOCKS5("tcp", parsed.Host, socksAuth(parsed), base)
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
			if parsed.Scheme == "https" {
				tc := tls.Client(conn, &tls.Config{ServerName: parsed.Hostname(), RootCAs: proxyRoots, MinVersion: tls.VersionTLS12})
				if err := tc.HandshakeContext(ctx); err != nil {
					conn.Close()
					return nil, fmt.Errorf("proxy tls: %w", err)
				}
				conn = tc
			}
			req := &http.Request{
				Method: http.MethodConnect,
				URL:    &url.URL{Opaque: addr},
				Host:   addr,
				Header: make(http.Header),
			}
			if parsed.User != nil {
				if pw, ok := parsed.User.Password(); ok {
					auth := base64.StdEncoding.EncodeToString([]byte(parsed.User.Username() + ":" + pw))
					req.Header.Set("Proxy-Authorization", "Basic "+auth)
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
			// http.ReadResponse reads through br, which may buffer bytes that
			// arrived after the CONNECT 200 (the start of the tunneled stream).
			// Returning the bare conn would discard them, so wrap it to drain
			// the buffer first.
			return &connectConn{Conn: conn, r: br}, nil
		}, nil
	default:
		return nil, fmt.Errorf("unsupported proxy scheme %q", parsed.Scheme)
	}
}

// connectConn wraps a proxied connection whose CONNECT response was read via a
// bufio.Reader. Reads drain any bytes the reader buffered past the response
// (the leading bytes of the tunneled stream) before continuing from the conn;
// all other operations delegate to the underlying conn.
type connectConn struct {
	net.Conn
	r *bufio.Reader
}

func (c *connectConn) Read(p []byte) (int, error) { return c.r.Read(p) }
