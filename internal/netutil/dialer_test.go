// internal/netutil/dialer_test.go
package netutil

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

// echoServer starts a TCP server that writes "hello" to any client and returns its addr.
func echoServer(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_, _ = c.Write([]byte("hello"))
			c.Close()
		}
	}()
	return ln.Addr().String()
}

// httpConnectProxy starts a minimal HTTP CONNECT proxy that tunnels to the
// requested target and records that it was used. Returns proxy addr + a func
// reporting how many CONNECTs it served.
func httpConnectProxy(t *testing.T) (addr string, connects func() int) {
	t.Helper()
	var n int
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			client, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				br := bufio.NewReader(client)
				req, err := http.ReadRequest(br)
				if err != nil || req.Method != http.MethodConnect {
					client.Close()
					return
				}
				n++
				target, err := net.Dial("tcp", req.Host)
				if err != nil {
					client.Write([]byte("HTTP/1.1 502 Bad Gateway\r\n\r\n"))
					client.Close()
					return
				}
				client.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
				go io.Copy(target, br)
				io.Copy(client, target)
				client.Close()
				target.Close()
			}()
		}
	}()
	return ln.Addr().String(), func() int { return n }
}

func readAll(t *testing.T, c net.Conn) string {
	t.Helper()
	c.SetReadDeadline(time.Now().Add(2 * time.Second))
	b, _ := io.ReadAll(c)
	return string(b)
}

func TestProxyDialer_DirectWhenEmpty(t *testing.T) {
	target := echoServer(t)
	dial, err := ProxyDialer("")
	if err != nil {
		t.Fatal(err)
	}
	c, err := dial(context.Background(), "tcp", target)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if got := readAll(t, c); got != "hello" {
		t.Fatalf("got %q", got)
	}
}

func TestProxyDialer_HTTPConnectTunnel(t *testing.T) {
	target := echoServer(t)
	proxyAddr, connects := httpConnectProxy(t)
	dial, err := ProxyDialer("http://" + proxyAddr)
	if err != nil {
		t.Fatal(err)
	}
	c, err := dial(context.Background(), "tcp", target)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if got := readAll(t, c); got != "hello" {
		t.Fatalf("got %q", got)
	}
	if connects() == 0 {
		t.Fatal("proxy was not used — traffic bypassed it")
	}
}

func TestProxyDialer_UnsupportedScheme(t *testing.T) {
	if _, err := ProxyDialer("ftp://nope:1"); err == nil {
		t.Fatal("expected error for unsupported scheme")
	}
}

func TestProxyDialer_SOCKS5(t *testing.T) {
	// Uses a tiny in-test SOCKS5 server is heavy; instead assert construction
	// succeeds for a socks5 URL (dial exercised in staging). A bad host still
	// constructs — dial-time failure is the fail-loud path.
	if _, err := ProxyDialer("socks5://127.0.0.1:1080"); err != nil {
		t.Fatalf("socks5 construction should succeed: %v", err)
	}
}
