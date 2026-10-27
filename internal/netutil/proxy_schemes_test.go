package netutil

import (
	"context"
	"crypto/x509"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
)

// socks5Proxy is a minimal SOCKS5 server that requires a user and password
// and records the ones it was sent.
func socks5Proxy(t *testing.T) (addr string, creds func() string) {
	t.Helper()
	var mu sync.Mutex
	var got string
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
			go func() {
				defer c.Close()
				hdr := make([]byte, 2)
				if _, err := io.ReadFull(c, hdr); err != nil {
					return
				}
				methods := make([]byte, hdr[1])
				if _, err := io.ReadFull(c, methods); err != nil {
					return
				}
				c.Write([]byte{5, 2}) // user and password
				ver := make([]byte, 2)
				if _, err := io.ReadFull(c, ver); err != nil {
					return
				}
				user := make([]byte, ver[1])
				io.ReadFull(c, user)
				pl := make([]byte, 1)
				io.ReadFull(c, pl)
				pass := make([]byte, pl[0])
				io.ReadFull(c, pass)
				mu.Lock()
				got = string(user) + ":" + string(pass)
				mu.Unlock()
				c.Write([]byte{1, 0})
				req := make([]byte, 4)
				if _, err := io.ReadFull(c, req); err != nil || req[3] != 1 {
					return
				}
				ap := make([]byte, 6)
				io.ReadFull(c, ap)
				target := net.JoinHostPort(net.IP(ap[:4]).String(), strconv.Itoa(int(binary.BigEndian.Uint16(ap[4:]))))
				up, err := net.Dial("tcp", target)
				if err != nil {
					c.Write([]byte{5, 1, 0, 1, 0, 0, 0, 0, 0, 0})
					return
				}
				defer up.Close()
				c.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0})
				go io.Copy(up, c)
				io.Copy(c, up)
			}()
		}
	}()
	return ln.Addr().String(), func() string {
		mu.Lock()
		defer mu.Unlock()
		return got
	}
}

func TestProxyDialer_SOCKS5SendsCredentials(t *testing.T) {
	target := echoServer(t)
	addr, creds := socks5Proxy(t)
	dial, err := ProxyDialer("socks5://bob:s3cret@" + addr)
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
	if got := creds(); got != "bob:s3cret" {
		t.Fatalf("proxy got credentials %q", got)
	}
}

func TestProxyDialer_HTTPSProxySpeaksTLS(t *testing.T) {
	target := echoServer(t)
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect {
			http.Error(w, "connect only", http.StatusMethodNotAllowed)
			return
		}
		up, err := net.Dial("tcp", r.Host)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			up.Close()
			return
		}
		go func() { io.Copy(up, conn); up.Close() }()
		io.Copy(conn, up)
		conn.Close()
	}))
	srv.StartTLS()
	t.Cleanup(srv.Close)
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	old := proxyRoots
	proxyRoots = pool
	t.Cleanup(func() { proxyRoots = old })

	dial, err := ProxyDialer("https://" + srv.Listener.Addr().String())
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
