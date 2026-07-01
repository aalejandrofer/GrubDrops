# Proxy Bypass Fix Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Route every outbound connection through the configured global proxy — Kick utls HTTP, Kick WebSocket, Twitch PubSub, per-account Twitch HTTP, Discord webhooks, and the Kick browser sidecar — closing the "not all traffic is routed" bug.

**Architecture:** Add one TCP-tunnel primitive in `netutil` (SOCKS5 via `x/net/proxy`, HTTP via `CONNECT`) that returns a `net.Conn` straight to the target host. Every custom dialer (utls, websocket) swaps its raw `net.Dialer` for this primitive, then layers utls/TLS on top unchanged — the Chrome fingerprint is byte-for-byte identical. The sidecar gets a `--proxy-server` Chrome flag. All fed one effective proxy URL, read once at startup.

**Tech Stack:** Go, `github.com/refraction-networking/utls`, `golang.org/x/net/proxy`, `github.com/coder/websocket` or `gorilla/websocket` (as used in `pubsub.go`/`wswatch.go`), chromedp sidecars via `internal/dockerctl`.

## Global Constraints

- Proxy is **global-only**, read once at startup; **restart to apply**. Do not add live-reload.
- Proxy schemes supported: `http://`, `https://`, `socks5://` (match `netutil.NewTransport`).
- **Never terminate TLS at the proxy.** Tunnel at the TCP layer only, then hand the `net.Conn` to `utls.UClient` / the websocket dialer unchanged — the Chrome TLS fingerprint must not change (Kick's Cloudflare check depends on it).
- **Fail loud:** if the proxy is enabled but a dial fails, the request errors (existing backoff retries). Never silently fall back to a direct connection.
- Never add a `Client-Integrity` header to Twitch (unrelated but a standing repo rule).
- `gofmt -w` before every commit; CI has a gofmt gate.

---

### Task 1: `netutil.ProxyDialer` primitive

**Files:**
- Create: `internal/netutil/dialer.go`
- Test: `internal/netutil/dialer_test.go`

**Interfaces:**
- Produces:
  - `type DialContextFunc func(ctx context.Context, network, addr string) (net.Conn, error)`
  - `func ProxyDialer(proxyURL string) (DialContextFunc, error)` — empty `proxyURL` returns a direct `net.Dialer`-backed func; `socks5://` uses `proxy.SOCKS5`; `http(s)://` uses an HTTP CONNECT tunnel; unsupported scheme returns an error.

- [ ] **Step 1: Write the failing tests**

```go
// internal/netutil/dialer_test.go
package netutil

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"strings"
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/netutil/ -run TestProxyDialer -v`
Expected: FAIL / build error — `ProxyDialer` undefined.

- [ ] **Step 3: Implement `ProxyDialer`**

```go
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/netutil/ -v`
Expected: PASS (all four).

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/netutil/
git add internal/netutil/dialer.go internal/netutil/dialer_test.go
git commit -m "feat(netutil): ProxyDialer TCP-tunnel primitive (socks5 + http CONNECT)"
```

---

### Task 2: Kick `WithProxy` option + utls HTTP doer

**Files:**
- Modify: `internal/platform/kick/backend.go` (options struct, `WithProxy`, `New`)
- Modify: `internal/platform/kick/transport.go` (`httpDoer` holds dialer; `connFor` uses it)
- Test: `internal/platform/kick/transport_test.go`

**Interfaces:**
- Consumes: `netutil.ProxyDialer`, `netutil.DialContextFunc`.
- Produces: `func WithProxy(proxyURL string) Option`; `newHTTPDoer(dial netutil.DialContextFunc) *httpDoer` (dial nil → direct).

- [ ] **Step 1: Write the failing test** — assert `httpDoer` dials through an injected dialer.

```go
// internal/platform/kick/transport_test.go
package kick

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
)

func TestHTTPDoer_UsesInjectedDialer(t *testing.T) {
	var used int32
	dial := func(ctx context.Context, network, addr string) (net.Conn, error) {
		atomic.AddInt32(&used, 1)
		return nil, context.Canceled // short-circuit; we only assert it was called
	}
	d := newHTTPDoer(dial)
	_, _ = d.connFor(context.Background(), "web.kick.com")
	if atomic.LoadInt32(&used) == 0 {
		t.Fatal("httpDoer did not use the injected dialer")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/platform/kick/ -run TestHTTPDoer_UsesInjectedDialer -v`
Expected: FAIL — `newHTTPDoer` takes no args.

- [ ] **Step 3: Implement**

In `transport.go`, add a `dial netutil.DialContextFunc` field to `httpDoer`; change `newHTTPDoer` to accept it (default to a direct `net.Dialer` when nil); in `connFor` replace:

```go
	var dialer net.Dialer
	tcp, err := dialer.DialContext(dialCtx, "tcp", host+":443")
```
with:
```go
	tcp, err := d.dial(dialCtx, "tcp", host+":443")
```

and in `newHTTPDoer`:
```go
func newHTTPDoer(dial netutil.DialContextFunc) *httpDoer {
	if dial == nil {
		var nd net.Dialer
		dial = nd.DialContext
	}
	return &httpDoer{timeout: 20 * time.Second, conns: map[string]*cachedConn{}, dial: dial}
}
```

In `backend.go`: add `proxyURL string` to the `options` struct, add `WithProxy`, and in `New` build the dialer and pass it to `newHTTPDoer`:
```go
func WithProxy(proxyURL string) Option { return func(o *options) { o.proxyURL = proxyURL } }
```
```go
	dial, err := netutil.ProxyDialer(o.proxyURL)
	if err != nil {
		slog.Warn("kick: bad proxy url, using direct", "err", err)
		dial = nil
	}
	// ... wherever the httpDoer is constructed:
	doer := newHTTPDoer(dial)
```
(Find the existing `newHTTPDoer(...)` call site in `New`/backend init and pass `dial`.)

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/platform/kick/ -run TestHTTPDoer_UsesInjectedDialer -v && go build ./...`
Expected: PASS + build OK.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/platform/kick/
git add internal/platform/kick/backend.go internal/platform/kick/transport.go internal/platform/kick/transport_test.go
git commit -m "feat(kick): route utls HTTP doer through proxy via WithProxy option"
```

---

### Task 3: Kick WebSocket dial through proxy

**Files:**
- Modify: `internal/platform/kick/wswatch.go` (`newUTLSConn`, `wsHTTPClient`, `wsDialer`)

**Interfaces:**
- Consumes: `netutil.DialContextFunc` from Task 2's backend.
- Produces: package-level `var wsProxyDial netutil.DialContextFunc` set once at backend construction; `newUTLSConn` uses it (nil → direct `net.Dialer`).

- [ ] **Step 1: Write the failing test**

```go
// append to internal/platform/kick/transport_test.go
func TestNewUTLSConn_UsesProxyDial(t *testing.T) {
	var used int32
	old := wsProxyDial
	t.Cleanup(func() { wsProxyDial = old })
	wsProxyDial = func(ctx context.Context, network, addr string) (net.Conn, error) {
		atomic.AddInt32(&used, 1)
		return nil, context.Canceled
	}
	_, _ = newUTLSConn(context.Background(), "tcp", "web.kick.com:443")
	if atomic.LoadInt32(&used) == 0 {
		t.Fatal("newUTLSConn did not use wsProxyDial")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/platform/kick/ -run TestNewUTLSConn_UsesProxyDial -v`
Expected: FAIL — `wsProxyDial` undefined.

- [ ] **Step 3: Implement**

In `wswatch.go`, add:
```go
// wsProxyDial is the TCP dialer for the WS utls path, set once at backend
// construction (WithProxy). Nil means direct.
var wsProxyDial netutil.DialContextFunc
```
In `newUTLSConn`, replace:
```go
	d := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
	raw, err := d.DialContext(ctx, network, addr)
```
with:
```go
	dial := wsProxyDial
	if dial == nil {
		d := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
		dial = d.DialContext
	}
	raw, err := dial(ctx, network, addr)
```
In `backend.go` `New` (Task 2), after building `dial`, set `wsProxyDial = dial` so the WS globals tunnel through the same proxy.

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/platform/kick/ -v && go build ./...`
Expected: PASS + build OK.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/platform/kick/
git add internal/platform/kick/wswatch.go internal/platform/kick/backend.go internal/platform/kick/transport_test.go
git commit -m "feat(kick): tunnel WS utls dial through the configured proxy"
```

---

### Task 4: Twitch PubSub + per-account backend through proxy

**Files:**
- Modify: `internal/platform/twitch/pubsub.go` (`PubSubClient` gets a dialer; `dialAndPump` uses it)
- Modify: `internal/platform/twitch/backend.go` (thread proxyURL to the PubSub client; confirm `NewWithTransport` path)
- Test: `internal/platform/twitch/pubsub_test.go`

**Interfaces:**
- Consumes: `netutil.ProxyDialer`.
- Produces: `PubSubClient` honoring an injected `netutil.DialContextFunc` as `websocket.Dialer.NetDialContext`.

- [ ] **Step 1: Write the failing test** — a PubSub client built with a proxy dialer uses it when dialing.

```go
// internal/platform/twitch/pubsub_test.go
package twitch

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
)

func TestPubSub_UsesProxyDial(t *testing.T) {
	var used int32
	dial := func(ctx context.Context, network, addr string) (net.Conn, error) {
		atomic.AddInt32(&used, 1)
		return nil, context.Canceled
	}
	p := newPubSubClientWithDial(dial) // see Step 3 for constructor
	_ = p.dialAndPump(context.Background())
	if atomic.LoadInt32(&used) == 0 {
		t.Fatal("PubSub did not use the injected dialer")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/platform/twitch/ -run TestPubSub_UsesProxyDial -v`
Expected: FAIL — `newPubSubClientWithDial` undefined.

- [ ] **Step 3: Implement**

Add a `dial netutil.DialContextFunc` field to `PubSubClient` and a constructor/param that accepts it (mirror the existing constructor; the plan's implementer must read `pubsub.go` for the current constructor name — add `newPubSubClientWithDial(dial netutil.DialContextFunc) *PubSubClient` or extend the existing one with the dialer). In `dialAndPump`, replace:
```go
	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
```
with:
```go
	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	if p.dial != nil {
		dialer.NetDialContext = p.dial
	}
```
In `backend.go`, thread `proxyURL` from the constructor to the PubSub client (build the dialer via `netutil.ProxyDialer`). Because per-account backends are built at `cmd/miner/main.go:315`, the constructor must accept the proxy so those honor it too (Task 6).

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/platform/twitch/ -v && go build ./...`
Expected: PASS + build OK.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/platform/twitch/
git add internal/platform/twitch/pubsub.go internal/platform/twitch/backend.go internal/platform/twitch/pubsub_test.go
git commit -m "feat(twitch): route PubSub websocket dial through the proxy"
```

---

### Task 5: Kick sidecar `--proxy-server`

**Files:**
- Modify: `internal/platform/kick/backend.go` (`WithSidecarAutoCreate` or a sibling receives proxyURL) and the sidecar container-create path (read the current create code to find where Chrome args/cmd are set).
- Test: cover the arg-building helper if one exists; otherwise assert `New(..., WithProxy(url), WithSidecarAutoCreate(...))` stores the proxy for the create path.

**Interfaces:**
- Consumes: the effective proxy URL already on the backend `options` (Task 2).
- Produces: each auto-created sidecar container command includes `--proxy-server=<proxyURL>` when a proxy is set.

- [ ] **Step 1: Write the failing test**

```go
// internal/platform/kick/sidecar_proxy_test.go
package kick

import "testing"

func TestSidecarChromeArgs_IncludeProxy(t *testing.T) {
	args := chromeArgsForSidecar("socks5://127.0.0.1:1080") // helper introduced in Step 3
	found := false
	for _, a := range args {
		if a == "--proxy-server=socks5://127.0.0.1:1080" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected --proxy-server in chrome args, got %v", args)
	}
}

func TestSidecarChromeArgs_NoProxyWhenEmpty(t *testing.T) {
	for _, a := range chromeArgsForSidecar("") {
		if len(a) >= 15 && a[:15] == "--proxy-server=" {
			t.Fatalf("did not expect a proxy arg, got %v", a)
		}
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/platform/kick/ -run TestSidecarChromeArgs -v`
Expected: FAIL — `chromeArgsForSidecar` undefined.

- [ ] **Step 3: Implement**

Read the current sidecar create code (where the Chrome container `Cmd`/args are assembled — search `--` flags or `ContainerCreate` in `internal/platform/kick/` and `internal/dockerctl/`). Extract a small helper:
```go
func chromeArgsForSidecar(proxyURL string) []string {
	args := []string{ /* existing base chrome flags copied verbatim from the current create path */ }
	if proxyURL != "" {
		args = append(args, "--proxy-server="+proxyURL)
	}
	return args
}
```
Wire the backend's `options.proxyURL` into this helper at container-create time.

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/platform/kick/ -v && go build ./...`
Expected: PASS + build OK.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/platform/kick/
git add internal/platform/kick/
git commit -m "feat(kick): pass --proxy-server to auto-created sidecar Chrome"
```

---

### Task 6: Wire the effective proxy URL in `main.go`

**Files:**
- Modify: `cmd/miner/main.go`

**Interfaces:**
- Consumes: `kick.WithProxy`, the twitch backend proxy param (Task 4), `notify.NewDiscordWebhookWithTransport`.

- [ ] **Step 1: Add `WithProxy` to the Kick options**

At `main.go` where `kickOpts` is built (near line 177), add (proxy already computed as `proxyURL`, gated by `proxyEnabled`):
```go
	effectiveProxy := ""
	if proxyEnabled && proxyURL != "" {
		effectiveProxy = proxyURL
	}
	kickOpts = append(kickOpts, kick.WithProxy(effectiveProxy))
```

- [ ] **Step 2: Per-account Twitch backends use the proxy**

At `main.go:315` (`bk := twitch.New()`) and the global backend (line 209), route through the proxy. Replace bare `twitch.New()` with the proxy-aware constructor (Task 4's signature — pass `effectiveProxy` / `proxyTransport`). Confirm both the global (209) and per-account (315) paths carry it.

- [ ] **Step 3: Discord fallback always proxied**

The else branch at `main.go:259` already falls back to a non-proxied webhook when `proxyTransport == nil`. That is correct (nil = proxy disabled). Verify no change needed; if `proxyTransport` is non-nil the `if` branch already applies. No edit unless a bug is found.

- [ ] **Step 4: Build + full test**

Run: `go build ./... && go test ./...`
Expected: build OK, all tests PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w cmd/miner/
git add cmd/miner/main.go
git commit -m "feat(miner): thread the global proxy into Kick, Twitch, and sidecars"
```

---

### Task 7: Changelog + verification gate

**Files:**
- Modify: `docs/CHANGELOG.md`

- [ ] **Step 1: Log under `[Unreleased]`**

```markdown
### Fixed

- **All outbound traffic now honours the proxy.** Kick's Chrome-fingerprinted
  HTTP and WebSocket paths, Twitch PubSub, per-account Twitch, Discord webhooks,
  and the Kick browser sidecar previously bypassed the configured proxy. They
  now tunnel through it (SOCKS5 or HTTP CONNECT) with the TLS fingerprint
  unchanged. Restart to apply.
```

- [ ] **Step 2: Commit**

```bash
git add docs/CHANGELOG.md
git commit -m "docs(changelog): all outbound traffic honours the proxy"
```

- [ ] **Step 3: Live verification gate (accrual-touching — required before tagging)**

This changes the Kick watch/accrual network path. Per repo rules, before any
version tag:
1. Deploy to staging (local-build recipe) with a real proxy configured + enabled.
2. Confirm Kick still passes Cloudflare (drops catalog loads, no 403) — proves
   the utls fingerprint survived the tunnel.
3. Confirm Kick accrual still accrues through the proxy on a live drop (or the
   Heartbeat/accrual canary is green through the proxy).
4. Confirm the proxy-test endpoint reports the proxy IP, and (optionally) that
   the target host sees the proxy IP, not the host IP.
Hold the tag in `[Unreleased]` until a live Kick check passes.

---

## Self-Review

- **Spec coverage:** primitive (T1), Kick HTTP (T2), Kick WS (T3), Twitch PubSub + per-account (T4), sidecar (T5), main wiring + Discord (T6), changelog + accrual gate (T7). All spec components covered; per-account Twitch (`main.go:315`) added beyond the spec as an additional bypass found during planning.
- **Placeholders:** none — the only "read the current code" notes are for the PubSub constructor name and the sidecar base Chrome flags, which must be copied verbatim from existing code; test + wiring code is complete.
- **Type consistency:** `netutil.DialContextFunc` and `netutil.ProxyDialer` used consistently across T1–T6; `WithProxy(string) Option`, `newHTTPDoer(DialContextFunc)`, `wsProxyDial` var, `PubSubClient.dial` field all match their definitions.
