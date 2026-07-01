# Proxy bypass fix — design

**Date:** 2026-07-01
**Status:** approved, pre-implementation
**Scope:** route ALL outbound traffic through the single global proxy. Fixes the
reported bug "when using a proxy, not all traffic is routed through it."

## Problem

The proxy is a global setting (`settings:proxy_url`, `settings:proxy_enabled`)
read once at startup in `cmd/miner/main.go`, which builds an `*http.Transport`
via `netutil.NewTransport` and injects it into a subset of clients. Several
outbound paths construct their own dialers/transports and bypass the proxy:

- **Kick utls HTTP** — `internal/platform/kick/transport.go` `httpDoer.connFor`
  dials raw TCP with `net.Dialer` then wraps with `utls.UClient` (Chrome
  fingerprint). All Kick API + discovery + image fetches go through this.
- **Kick WS** — `internal/platform/kick/wswatch.go`: `newUTLSConn` (line 73)
  uses `net.Dialer`; the package globals `wsHTTPClient` and `wsDialer` use it
  as `DialTLSContext` / `NetDialTLSContext`. Viewer-token fetch + WS watch path.
- **Twitch PubSub** — `internal/platform/twitch/pubsub.go:185` uses a bare
  `websocket.Dialer` (watch-time accrual notifications).
- **Discord fallback** — `cmd/miner/main.go` only uses the proxy-aware webhook
  constructor when a transport is present; the else branch is direct.
- **Kick browser sidecar** — the auto-created chromedp Chrome containers egress
  directly (Kick IVS video / accrual).

Already correct (leave alone): Twitch backend (`NewWithTransport`), the update
checker, the global Discord path, and the proxy-test endpoint.

## Non-goals

- **Live reload.** Proxy is read once at startup; changing it in Settings
  applies on the next restart (unchanged from today).
- **Per-account proxy.** The dormant `accounts.proxy_url` column was removed
  (migration 0015). Proxy is global-only.

## Key insight: fingerprint is preserved

Kick beats Cloudflare with a real Chrome TLS fingerprint via utls. The proxy
must tunnel at the **TCP layer** (SOCKS5 or HTTP `CONNECT`) and hand the
resulting `net.Conn` to `utls.UClient` unchanged. The proxy never terminates
TLS, so the Chrome ClientHello is byte-for-byte identical to the direct path.
This is why routing utls through a proxy is safe.

## Components

### 1. `netutil.ProxyDialer` (new primitive)

```go
// DialContextFunc dials a TCP connection straight to addr, optionally tunneled
// through the configured proxy.
type DialContextFunc func(ctx context.Context, network, addr string) (net.Conn, error)

// ProxyDialer returns a dialer for the given proxy URL. An empty url returns a
// direct net.Dialer. socks5:// uses golang.org/x/net/proxy; http(s):// performs
// an HTTP CONNECT tunnel. Returns an error for unsupported schemes.
func ProxyDialer(proxyURL string) (DialContextFunc, error)
```

- `""` → `(&net.Dialer{Timeout, KeepAlive}).DialContext` (direct).
- `socks5://host:port` → `proxy.SOCKS5` context dialer (dep already present).
- `http(s)://host:port` → dial the proxy, write `CONNECT host:port HTTP/1.1`,
  read status; on `200` return the raw conn, else error and close.

The returned conn speaks directly to the target host; callers run
utls/TLS/HTTP over it exactly as before.

`netutil.NewTransport` (used by the already-correct paths) is unchanged; it can
optionally be refactored to build on `ProxyDialer` later, but that is not
required for this fix.

### 2. Go outbound paths

- **Kick `httpDoer`** (`transport.go`): hold a `DialContextFunc`; `connFor`
  uses it in place of `net.Dialer`, then `utls.UClient` as-is.
- **Kick WS** (`wswatch.go`): `newUTLSConn` uses the injected dialer for the
  inner TCP dial before `utls.UClient`. The `wsHTTPClient` / `wsDialer`
  package globals are refactored to be constructed with the dialer (backend
  field or a package-level dialer set once at construction).
- Both fed from a new `kick.WithProxy(proxyURL string) Option` so the `Backend`
  carries the dialer and passes it to the HTTP doer and WS path.
- **Twitch PubSub** (`pubsub.go`): set `websocket.Dialer.NetDialContext` to the
  proxy dialer (TLS for `wss` still handled by the websocket dialer). Fed from
  the twitch backend/pubsub constructor using the same effective proxy URL.
- **Discord fallback** (`main.go`): always call
  `NewDiscordWebhookWithTransport(url, filter, proxyTransport)` where
  `proxyTransport` is nil when the proxy is disabled (nil → direct).

### 3. Sidecar Chrome

When `dockerctl` auto-creates a Kick sidecar (`kick.WithSidecarAutoCreate`),
add `--proxy-server=<proxyURL>` to the Chrome container command so IVS video
egress is proxied. Chrome accepts `http://`, `https://`, and `socks5://` forms.
Wired via the sidecar-creation option receiving the effective proxy URL.

### 4. Config propagation (startup)

`cmd/miner/main.go` already reads `proxy_url` + `proxy_enabled` and computes
whether the proxy is active. Compute one effective proxy URL (`""` when
disabled) and pass it into: `kick.New(..., WithProxy(url))`, the sidecar
auto-create option, the Twitch backend/PubSub constructor, and the Discord
fallback (as a transport). Read once; restart to apply.

## Error handling

- If the proxy is **enabled but a dial fails**, the request returns an error
  (handled by the existing watcher backoff/retry). It does **not** fall back to
  a direct connection — silent direct fallback is exactly the leak being fixed.
- `ProxyDialer` returns an error for an unsupported scheme at construction;
  `main.go` logs it and treats the proxy as misconfigured (surfaced to the
  operator; the existing proxy-test endpoint validates a URL before saving).
- A bad `--proxy-server` makes the sidecar Chrome fail to load the page, which
  surfaces as an unhealthy sidecar / no accrual (visible), not a silent leak.

## Testing

- **`netutil.ProxyDialer` unit tests**: spin an in-test mock SOCKS5 listener and
  an in-test HTTP `CONNECT` listener; assert the returned conn reaches a local
  target and that the mock proxy observed the connection (CONNECT line / SOCKS5
  handshake). Cover the direct (`""`) case and the unsupported-scheme error.
- **Wiring tests**: with a proxy URL set, assert each constructor produces a
  dialer that connects to the mock proxy (dependency injection makes this
  testable without real Kick/Cloudflare). Covers Kick httpDoer, Kick WS,
  Twitch PubSub.
- **utls end-to-end is not unit-tested** (needs live network + Cloudflare).
  Because this changes the Kick watch/accrual network path, it is gated on a
  **live-Kick staging verify that accrual still accrues through the proxy**
  before tagging (per the repo release rules for accrual-touching changes).

## Files touched (anticipated)

- `internal/netutil/` — new `ProxyDialer` (+ tests).
- `internal/platform/kick/transport.go`, `wswatch.go`, `backend.go` — inject
  dialer + `WithProxy` option.
- `internal/platform/kick/*` sidecar creation — `--proxy-server` flag.
- `internal/platform/twitch/pubsub.go` (+ constructor) — dialer injection.
- `internal/notify` + `cmd/miner/main.go` — Discord fallback always proxied.
- `cmd/miner/main.go` — propagate the effective proxy URL.
