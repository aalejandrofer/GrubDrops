// Package netutil provides shared network utilities for HTTP clients.
package netutil

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"time"

	"golang.org/x/net/proxy"
)

// NewTransport creates an http.Transport with optional proxy support.
// If proxyURL is empty, returns a default transport.
// Supports http://, https://, and socks5:// proxy protocols.
func NewTransport(proxyURL string) *http.Transport {
	transport := &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
	}

	if proxyURL == "" {
		return transport
	}

	parsed, err := url.Parse(proxyURL)
	if err != nil {
		return transport
	}

	switch parsed.Scheme {
	case "socks5":
		// Use golang.org/x/net/proxy for SOCKS5
		dialer, err := proxy.SOCKS5("tcp", parsed.Host, socksAuth(parsed), proxy.Direct)
		if err != nil {
			return transport
		}
		// Wrap the SOCKS5 dialer to implement DialContext with context support.
		// Run the dial in a goroutine so ctx cancellation is respected.
		transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			type result struct {
				conn net.Conn
				err  error
			}
			ch := make(chan result, 1)
			go func() {
				conn, err := dialer.Dial(network, addr)
				ch <- result{conn, err}
			}()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case r := <-ch:
				return r.conn, r.err
			}
		}
	default:
		// http:// or https://
		transport.Proxy = http.ProxyURL(parsed)
	}

	return transport
}
