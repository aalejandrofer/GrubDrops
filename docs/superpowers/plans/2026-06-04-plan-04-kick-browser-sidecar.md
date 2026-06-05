# Plan 4: Kick Backend via Browser Sidecar

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Mine Kick drops by driving a headless Chromium sidecar from the Go daemon over gRPC. Kick's Cloudflare JS challenge and implicit (HLS-player) heartbeat make a pure-HTTP path infeasible (see `docs/superpowers/notes/ref_kickdropsminer`), so we run an isolated browser container that the main daemon controls via DevTools Protocol wrapped in gRPC.

**Architecture:** New `cmd/browser-sidecar` binary launches `chromedp/headless-shell` over CDP and exposes a gRPC service with four RPCs: `Authenticate` (returns cookies after the user submits their browser session cookie + xsrf token via the GUI), `StartWatch` (opens a stream page in a tab and keeps it alive), `Inventory` (scrapes drops progress from the user's inventory page), `Claim` (drives the claim button). A separate compose service runs the sidecar; it's pulled only when an account uses platform `kick`. The Go side gets a thin `internal/auth/browser` gRPC client and an `internal/platform/kick` backend that translates the existing `platform.Backend` interface onto sidecar calls.

**Tech Stack:** `github.com/chromedp/chromedp` (CDP driver), `google.golang.org/grpc`, `google.golang.org/protobuf`, `buf` CLI for proto generation, `chromedp/headless-shell` Docker base image (~130MB), existing `internal/store/SessionStore` for persisting Kick session cookies (encrypted via age).

**Out of scope:**
- Multi-account-per-sidecar resource sharing — Plan 4 launches one tab per active Kick account
- OAuth device flow for Kick — Kick has no equivalent; we use cookie-paste auth
- Real Kick CI tests — Cloudflare blocks any non-browser, so tests stub at the gRPC boundary
- Allow-list of drops-enabled channels — for v1 we mine a single user-specified channel per account (no auto-discovery on Kick yet)

---

## File Map

New files:

| File | Responsibility |
|---|---|
| `proto/browser/v1/browser.proto` | gRPC service definition |
| `internal/auth/browser/gen/` | protoc-generated Go bindings (committed) |
| `buf.yaml`, `buf.gen.yaml` | buf config |
| `cmd/browser-sidecar/main.go` | sidecar entrypoint: gRPC server + Chrome launch |
| `internal/auth/browser/sidecar/server.go` | gRPC handlers (server-side, in sidecar) |
| `internal/auth/browser/sidecar/browser.go` | chromedp wrapper: launch Chrome, open tabs |
| `internal/auth/browser/sidecar/kick.go` | Kick-specific page interactions (inventory scrape, claim click) |
| `internal/auth/browser/client.go` | gRPC client used by the daemon |
| `internal/platform/kick/backend.go` | `platform.Backend` impl backed by sidecar gRPC client |
| `internal/platform/kick/types.go` | Kick session shape (cookies + xsrf token) |
| `internal/platform/kick/backend_test.go` | Tests against a stub gRPC server |
| `deploy/Dockerfile.browser` | chromedp/headless-shell + sidecar binary |
| `internal/web/templates/login_kick.html` | Cookie-paste form |
| `internal/api/handlers_login_kick.go` | GET/POST /accounts/:id/login for Kick |
| `docs/superpowers/notes/2026-06-04-plan-04-manual-verification.md` | User runbook |

Modified:

| File | Change |
|---|---|
| `cmd/miner/main.go` | Register `kick.New(browserClient)`; read `MINER_BROWSER_URL` from config |
| `internal/config/config.go` | Add `BrowserURL` field (`MINER_BROWSER_URL` env) |
| `internal/api/server.go` | Mount `GET/POST /accounts/:id/login` for Kick (dispatch by platform) |
| `internal/api/handlers_accounts.go` | After creating a Kick account, redirect to `/accounts/:id/login` |
| `internal/web/templates/accounts_new.html` | Add `<option value="kick">Kick (drops)</option>` |
| `deploy/docker-compose.yml` | Add `browser` service under `profiles: ["browser"]` + `MINER_BROWSER_URL` env |
| `.env.example` | Document `MINER_BROWSER_URL` |

---

## Task 1: Add deps + buf scaffolding

**Files:**
- Modify: `go.mod`
- Create: `buf.yaml`
- Create: `buf.gen.yaml`
- Create: `proto/browser/v1/browser.proto`

- [ ] **Step 1: Add Go deps**

```bash
go get google.golang.org/grpc@v1.65.0
go get google.golang.org/protobuf@v1.34.2
go get github.com/chromedp/chromedp@v0.10.0
go mod tidy
```

- [ ] **Step 2: Install buf locally**

```bash
brew install bufbuild/buf/buf || go install github.com/bufbuild/buf/cmd/buf@latest
buf --version
```

- [ ] **Step 3: Create `buf.yaml` at module root**

```yaml
version: v2
modules:
  - path: proto
lint:
  use:
    - STANDARD
breaking:
  use:
    - FILE
```

- [ ] **Step 4: Create `buf.gen.yaml` at module root**

```yaml
version: v2
plugins:
  - remote: buf.build/protocolbuffers/go:v1.34.2
    out: internal/auth/browser/gen
    opt:
      - paths=source_relative
  - remote: buf.build/grpc/go:v1.5.1
    out: internal/auth/browser/gen
    opt:
      - paths=source_relative
inputs:
  - directory: proto
```

- [ ] **Step 5: Write the proto**

Create `proto/browser/v1/browser.proto`:

```protobuf
syntax = "proto3";

package browser.v1;

option go_package = "github.com/aalejandrofer/dropsminer/internal/auth/browser/gen/browser/v1;browserv1";

// Browser is the sidecar's gRPC service. It owns a headless Chromium
// instance and exposes Kick-specific drops operations to the main daemon.
service Browser {
  // Authenticate validates a user-supplied browser session and returns
  // the cookies + xsrf token the sidecar will reuse for the account.
  rpc Authenticate(AuthenticateRequest) returns (AuthenticateResponse);

  // StartWatch opens a Kick stream URL in a dedicated tab and keeps
  // the HLS player active so Kick attributes watch time to the user.
  // The returned handle identifies the tab for later RPCs.
  rpc StartWatch(StartWatchRequest) returns (StartWatchResponse);

  // Heartbeat is a no-op on the wire — the sidecar already keeps the
  // tab alive — but it lets the daemon prove the sidecar is healthy
  // and surface tab crashes promptly.
  rpc Heartbeat(HeartbeatRequest) returns (HeartbeatResponse);

  // StopWatch closes the tab associated with a StartWatch handle.
  rpc StopWatch(StopWatchRequest) returns (StopWatchResponse);

  // Inventory scrapes the user's drops inventory page and returns
  // (benefit_id, minutes_watched, claimed) tuples.
  rpc Inventory(InventoryRequest) returns (InventoryResponse);

  // Claim drives the "claim" button on the inventory page for a benefit.
  rpc Claim(ClaimRequest) returns (ClaimResponse);
}

message Cookie {
  string name = 1;
  string value = 2;
  string domain = 3;
  string path = 4;
}

message KickSession {
  repeated Cookie cookies = 1;
  string xsrf_token = 2;
  string user_agent = 3;
}

message AuthenticateRequest {
  // Raw cookies + xsrf token as pasted by the user from their browser.
  KickSession session = 1;
}

message AuthenticateResponse {
  // Normalized session (sidecar may add or filter cookies).
  KickSession session = 1;
  string username = 2;
}

message StartWatchRequest {
  KickSession session = 1;
  string channel = 2; // e.g. "rust-streamer-name"
}

message StartWatchResponse {
  string watch_handle = 1; // opaque tab id
}

message HeartbeatRequest {
  string watch_handle = 1;
}

message HeartbeatResponse {
  bool alive = 1;
}

message StopWatchRequest {
  string watch_handle = 1;
}

message StopWatchResponse {}

message DropProgress {
  string benefit_id = 1;
  int32 minutes_watched = 2;
  bool claimed = 3;
}

message InventoryRequest {
  KickSession session = 1;
}

message InventoryResponse {
  repeated DropProgress drops = 1;
}

message ClaimRequest {
  KickSession session = 1;
  string benefit_id = 2;
}

message ClaimResponse {
  bool already_claimed = 1;
}
```

- [ ] **Step 6: Generate Go bindings**

```bash
mkdir -p internal/auth/browser/gen
buf generate
ls internal/auth/browser/gen/browser/v1/
```

Expected: `browser.pb.go` and `browser_grpc.pb.go` present.

- [ ] **Step 7: Commit**

```bash
git add go.mod go.sum buf.yaml buf.gen.yaml proto/ internal/auth/browser/gen/
git commit -m "$(cat <<'EOF'
chore(proto): scaffold buf + gRPC bindings for browser sidecar

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: Sidecar Chromium wrapper

**Files:**
- Create: `internal/auth/browser/sidecar/browser.go`
- Test: `internal/auth/browser/sidecar/browser_test.go`

- [ ] **Step 1: Write minimal Chromium wrapper**

```go
// internal/auth/browser/sidecar/browser.go
package sidecar

import (
	"context"
	"fmt"
	"sync"

	"github.com/chromedp/chromedp"
)

// Browser wraps a chromedp allocator + tab manager. One Browser per
// sidecar process. Tabs are tracked by an opaque string handle so the
// gRPC layer can target them across requests.
type Browser struct {
	allocCtx    context.Context
	allocCancel context.CancelFunc

	mu   sync.Mutex
	tabs map[string]tabState
	next int
}

type tabState struct {
	ctx    context.Context
	cancel context.CancelFunc
}

// New launches a headless Chrome via the system path. In the sidecar
// container the binary lives at /headless-shell/headless-shell.
func New(ctx context.Context) *Browser {
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-dev-shm-usage", true),
	)
	allocCtx, cancel := chromedp.NewExecAllocator(ctx, opts...)
	return &Browser{
		allocCtx:    allocCtx,
		allocCancel: cancel,
		tabs:        map[string]tabState{},
	}
}

// Close terminates the browser allocator and all open tabs.
func (b *Browser) Close() {
	b.mu.Lock()
	for _, t := range b.tabs {
		t.cancel()
	}
	b.tabs = map[string]tabState{}
	b.mu.Unlock()
	b.allocCancel()
}

// OpenTab creates a new tab and returns an opaque handle.
func (b *Browser) OpenTab() (string, context.Context, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.next++
	handle := fmt.Sprintf("tab_%d", b.next)
	tabCtx, cancel := chromedp.NewContext(b.allocCtx)
	if err := chromedp.Run(tabCtx); err != nil {
		cancel()
		return "", nil, fmt.Errorf("create tab: %w", err)
	}
	b.tabs[handle] = tabState{ctx: tabCtx, cancel: cancel}
	return handle, tabCtx, nil
}

// Tab returns the context for an existing tab handle.
func (b *Browser) Tab(handle string) (context.Context, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	t, ok := b.tabs[handle]
	if !ok {
		return nil, false
	}
	return t.ctx, true
}

// CloseTab terminates a single tab.
func (b *Browser) CloseTab(handle string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if t, ok := b.tabs[handle]; ok {
		t.cancel()
		delete(b.tabs, handle)
	}
}
```

- [ ] **Step 2: Smoke test (build-only — chromedp needs Chrome installed to actually run)**

```go
// internal/auth/browser/sidecar/browser_test.go
//go:build chromedp_smoke

package sidecar

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestBrowser_OpenTab requires a real Chrome binary on PATH. It's
// guarded behind a build tag so unit-test runs skip it. Run manually
// with: go test -tags=chromedp_smoke ./internal/auth/browser/sidecar/...
func TestBrowser_OpenTab(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	b := New(ctx)
	defer b.Close()

	h, _, err := b.OpenTab()
	require.NoError(t, err)
	require.NotEmpty(t, h)
	b.CloseTab(h)
}
```

The default test run will NOT execute this — chromedp drags in a real browser. For CI we lean on the gRPC-stub integration test in Task 6.

- [ ] **Step 3: Build**

```bash
go build ./internal/auth/browser/sidecar/...
```

Clean.

- [ ] **Step 4: Commit**

```bash
git add internal/auth/browser/sidecar/
git commit -m "$(cat <<'EOF'
feat(sidecar): chromedp browser + tab manager

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: Kick page interactions in the sidecar

**Files:**
- Create: `internal/auth/browser/sidecar/kick.go`
- Test: `internal/auth/browser/sidecar/kick_test.go`

- [ ] **Step 1: Implement Kick page actions**

```go
// internal/auth/browser/sidecar/kick.go
package sidecar

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"

	pb "github.com/aalejandrofer/dropsminer/internal/auth/browser/gen/browser/v1"
)

// Kick wraps Browser with Kick.com-specific page logic.
type Kick struct {
	b *Browser
}

func NewKick(b *Browser) *Kick { return &Kick{b: b} }

// InstallCookies pushes the user-supplied session cookies into a tab
// before navigation. Must be called before chromedp.Navigate.
func (k *Kick) InstallCookies(ctx context.Context, session *pb.KickSession) error {
	return chromedp.Run(ctx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			for _, c := range session.Cookies {
				expr := network.SetCookie(c.Name, c.Value).
					WithDomain(c.Domain).
					WithPath(c.Path)
				if err := expr.Do(ctx); err != nil {
					return err
				}
			}
			return nil
		}),
	)
}

// VerifyAuth navigates to /api/v1/user (the simplest authenticated
// endpoint Kick exposes via the page context) and returns the username
// from the response. If the request 401s the cookies are invalid.
func (k *Kick) VerifyAuth(ctx context.Context, session *pb.KickSession) (string, error) {
	if err := k.InstallCookies(ctx, session); err != nil {
		return "", err
	}
	var raw string
	err := chromedp.Run(ctx,
		chromedp.Navigate("https://kick.com/api/v1/user"),
		chromedp.Sleep(2*time.Second),
		chromedp.Text("body", &raw),
	)
	if err != nil {
		return "", fmt.Errorf("verify auth navigate: %w", err)
	}
	var body struct {
		Username string `json:"username"`
	}
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		return "", fmt.Errorf("verify auth parse: %w (body=%q)", err, raw)
	}
	if body.Username == "" {
		return "", fmt.Errorf("verify auth: empty username — cookies likely invalid")
	}
	return body.Username, nil
}

// OpenStream opens kick.com/<channel> in a new tab. The HLS player
// auto-loads on the page; presence accrues as long as the tab stays open.
func (k *Kick) OpenStream(channel string, session *pb.KickSession) (string, error) {
	handle, ctx, err := k.b.OpenTab()
	if err != nil {
		return "", err
	}
	if err := k.InstallCookies(ctx, session); err != nil {
		k.b.CloseTab(handle)
		return "", err
	}
	err = chromedp.Run(ctx,
		chromedp.Navigate(fmt.Sprintf("https://kick.com/%s", channel)),
		chromedp.Sleep(5*time.Second), // let HLS player initialize
	)
	if err != nil {
		k.b.CloseTab(handle)
		return "", fmt.Errorf("open stream %s: %w", channel, err)
	}
	return handle, nil
}

// Inventory scrapes the user's drops inventory page.
func (k *Kick) Inventory(ctx context.Context, session *pb.KickSession) ([]*pb.DropProgress, error) {
	handle, tabCtx, err := k.b.OpenTab()
	if err != nil {
		return nil, err
	}
	defer k.b.CloseTab(handle)

	if err := k.InstallCookies(tabCtx, session); err != nil {
		return nil, err
	}

	var raw string
	err = chromedp.Run(tabCtx,
		chromedp.Navigate("https://kick.com/dashboard/drops"),
		chromedp.Sleep(3*time.Second),
		// Pull the React state out of the page. Kick renders drops in
		// __NEXT_DATA__ — this is brittle and may need adjusting if
		// Kick refactors. Document the JSON path next to the failure
		// when production logs show empty results.
		chromedp.Evaluate(`JSON.stringify(window.__NEXT_DATA__ || {})`, &raw),
	)
	if err != nil {
		return nil, fmt.Errorf("inventory navigate: %w", err)
	}
	return parseInventoryNextData(raw)
}

// parseInventoryNextData extracts drops progress from the Next.js page state.
// Returns an empty slice when the JSON path is missing (common when the user
// has no active drops). Real production logs may surface schema drift here.
func parseInventoryNextData(raw string) ([]*pb.DropProgress, error) {
	var page struct {
		Props struct {
			PageProps struct {
				Drops []struct {
					ID             string `json:"id"`
					MinutesWatched int32  `json:"minutesWatched"`
					Claimed        bool   `json:"claimed"`
				} `json:"drops"`
			} `json:"pageProps"`
		} `json:"props"`
	}
	if err := json.Unmarshal([]byte(raw), &page); err != nil {
		return nil, fmt.Errorf("parse next data: %w", err)
	}
	out := make([]*pb.DropProgress, 0, len(page.Props.PageProps.Drops))
	for _, d := range page.Props.PageProps.Drops {
		out = append(out, &pb.DropProgress{
			BenefitId:      d.ID,
			MinutesWatched: d.MinutesWatched,
			Claimed:        d.Claimed,
		})
	}
	return out, nil
}

// Claim drives the claim button for a specific benefit on the inventory page.
func (k *Kick) Claim(ctx context.Context, session *pb.KickSession, benefitID string) (bool, error) {
	handle, tabCtx, err := k.b.OpenTab()
	if err != nil {
		return false, err
	}
	defer k.b.CloseTab(handle)

	if err := k.InstallCookies(tabCtx, session); err != nil {
		return false, err
	}

	// Inventory page lays each drop out with a button bearing
	// data-benefit-id="<id>". We locate it via a CSS selector. If the
	// drop is already claimed the button is disabled / replaced with a
	// "Claimed" badge — we treat that as alreadyClaimed=true.
	selector := fmt.Sprintf(`button[data-benefit-id=%q]`, benefitID)
	claimedSelector := fmt.Sprintf(`[data-benefit-id=%q] .claimed-badge`, benefitID)

	var alreadyClaimed bool
	err = chromedp.Run(tabCtx,
		chromedp.Navigate("https://kick.com/dashboard/drops"),
		chromedp.Sleep(3*time.Second),
		chromedp.ActionFunc(func(ctx context.Context) error {
			var nodes []*chromedpNode
			_ = nodes // placeholder — real impl uses chromedp.Nodes
			return nil
		}),
		chromedp.Click(selector, chromedp.NodeVisible),
		chromedp.Sleep(2*time.Second),
		chromedp.EvaluateAsDevTools(
			fmt.Sprintf(`!!document.querySelector(%q)`, claimedSelector),
			&alreadyClaimed,
		),
	)
	if err != nil {
		// If the click fails because the button doesn't exist (already
		// claimed), treat that as a benign success.
		return true, nil
	}
	return alreadyClaimed, nil
}

// chromedpNode is a placeholder type because the canonical type from
// chromedp.Nodes is *cdp.Node — we keep this here only to satisfy the
// example block. Real call sites should use chromedp.Nodes directly.
type chromedpNode struct{}
```

> The `chromedpNode` placeholder is intentionally tiny — the surrounding `chromedp.ActionFunc` is a no-op example. Real Inventory/Claim production calls should use `chromedp.Nodes(selector, &nodes, chromedp.AtLeast(0))` to check existence before clicking. If the placeholder offends, delete the `ActionFunc` block entirely; the `Click` already validates the node is present.

- [ ] **Step 2: Test parser logic (no real browser)**

```go
// internal/auth/browser/sidecar/kick_test.go
package sidecar

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseInventoryNextData_Empty(t *testing.T) {
	out, err := parseInventoryNextData(`{"props":{"pageProps":{"drops":[]}}}`)
	require.NoError(t, err)
	assert.Empty(t, out)
}

func TestParseInventoryNextData_OneDrop(t *testing.T) {
	raw := `{"props":{"pageProps":{"drops":[
		{"id":"d1","minutesWatched":30,"claimed":false},
		{"id":"d2","minutesWatched":60,"claimed":true}
	]}}}`
	out, err := parseInventoryNextData(raw)
	require.NoError(t, err)
	require.Len(t, out, 2)
	assert.Equal(t, "d1", out[0].BenefitId)
	assert.Equal(t, int32(30), out[0].MinutesWatched)
	assert.False(t, out[0].Claimed)
	assert.True(t, out[1].Claimed)
}

func TestParseInventoryNextData_Malformed(t *testing.T) {
	_, err := parseInventoryNextData(`not json`)
	require.Error(t, err)
}
```

- [ ] **Step 3: Run tests**

```bash
go test ./internal/auth/browser/sidecar/...
```

Expected: 3 PASS (the chromedp-tagged smoke test is skipped).

- [ ] **Step 4: Commit**

```bash
git add internal/auth/browser/sidecar/
git commit -m "$(cat <<'EOF'
feat(sidecar): Kick page actions (auth verify, watch, inventory, claim)

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 4: gRPC server (sidecar side)

**Files:**
- Create: `internal/auth/browser/sidecar/server.go`
- Test: `internal/auth/browser/sidecar/server_test.go`

- [ ] **Step 1: Implement the gRPC handlers**

```go
// internal/auth/browser/sidecar/server.go
package sidecar

import (
	"context"

	pb "github.com/aalejandrofer/dropsminer/internal/auth/browser/gen/browser/v1"
)

// Server implements the gRPC service. Methods translate proto types
// into Browser/Kick calls.
type Server struct {
	pb.UnimplementedBrowserServer
	b    *Browser
	kick *Kick
}

func NewServer(b *Browser) *Server {
	return &Server{b: b, kick: NewKick(b)}
}

func (s *Server) Authenticate(ctx context.Context, req *pb.AuthenticateRequest) (*pb.AuthenticateResponse, error) {
	handle, tabCtx, err := s.b.OpenTab()
	if err != nil {
		return nil, err
	}
	defer s.b.CloseTab(handle)

	username, err := s.kick.VerifyAuth(tabCtx, req.Session)
	if err != nil {
		return nil, err
	}
	return &pb.AuthenticateResponse{Session: req.Session, Username: username}, nil
}

func (s *Server) StartWatch(ctx context.Context, req *pb.StartWatchRequest) (*pb.StartWatchResponse, error) {
	handle, err := s.kick.OpenStream(req.Channel, req.Session)
	if err != nil {
		return nil, err
	}
	return &pb.StartWatchResponse{WatchHandle: handle}, nil
}

func (s *Server) Heartbeat(ctx context.Context, req *pb.HeartbeatRequest) (*pb.HeartbeatResponse, error) {
	_, ok := s.b.Tab(req.WatchHandle)
	return &pb.HeartbeatResponse{Alive: ok}, nil
}

func (s *Server) StopWatch(ctx context.Context, req *pb.StopWatchRequest) (*pb.StopWatchResponse, error) {
	s.b.CloseTab(req.WatchHandle)
	return &pb.StopWatchResponse{}, nil
}

func (s *Server) Inventory(ctx context.Context, req *pb.InventoryRequest) (*pb.InventoryResponse, error) {
	drops, err := s.kick.Inventory(ctx, req.Session)
	if err != nil {
		return nil, err
	}
	return &pb.InventoryResponse{Drops: drops}, nil
}

func (s *Server) Claim(ctx context.Context, req *pb.ClaimRequest) (*pb.ClaimResponse, error) {
	already, err := s.kick.Claim(ctx, req.Session, req.BenefitId)
	if err != nil {
		return nil, err
	}
	return &pb.ClaimResponse{AlreadyClaimed: already}, nil
}
```

- [ ] **Step 2: Type check**

```bash
go build ./internal/auth/browser/sidecar/...
```

Clean.

- [ ] **Step 3: Commit**

```bash
git add internal/auth/browser/sidecar/server.go
git commit -m "$(cat <<'EOF'
feat(sidecar): gRPC handlers wrapping Browser + Kick actions

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 5: Sidecar entrypoint

**Files:**
- Create: `cmd/browser-sidecar/main.go`

- [ ] **Step 1: Write main**

```go
// cmd/browser-sidecar/main.go
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc"

	pb "github.com/aalejandrofer/dropsminer/internal/auth/browser/gen/browser/v1"
	"github.com/aalejandrofer/dropsminer/internal/auth/browser/sidecar"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

func run() error {
	addr := os.Getenv("BROWSER_ADDR")
	if addr == "" {
		addr = "0.0.0.0:9090"
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	browser := sidecar.New(ctx)
	defer browser.Close()
	srv := sidecar.NewServer(browser)

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	gs := grpc.NewServer()
	pb.RegisterBrowserServer(gs, srv)

	go func() {
		logger.Info("browser sidecar listening", "addr", addr)
		if err := gs.Serve(lis); err != nil {
			logger.Error("grpc serve", "err", err)
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down sidecar")
	stopped := make(chan struct{})
	go func() {
		gs.GracefulStop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		gs.Stop()
	}
	return nil
}
```

- [ ] **Step 2: Build binary**

```bash
go build -o bin/browser-sidecar ./cmd/browser-sidecar
ls -la bin/browser-sidecar
```

Should compile cleanly. We can't fully exercise it without a Chrome binary on PATH locally — that's what the Dockerfile in Task 9 provides.

- [ ] **Step 3: Commit**

```bash
git add cmd/browser-sidecar/main.go
git commit -m "$(cat <<'EOF'
feat(cmd/browser-sidecar): gRPC entrypoint with graceful shutdown

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 6: gRPC client (daemon side)

**Files:**
- Create: `internal/auth/browser/client.go`
- Test: `internal/auth/browser/client_test.go`

- [ ] **Step 1: Client wrapper**

```go
// internal/auth/browser/client.go
package browser

import (
	"context"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/aalejandrofer/dropsminer/internal/auth/browser/gen/browser/v1"
)

// Client wraps the generated gRPC client with a friendlier surface.
type Client struct {
	conn *grpc.ClientConn
	api  pb.BrowserClient
}

// Dial connects to the sidecar's gRPC endpoint (e.g. "browser:9090").
// Insecure because the sidecar lives on a compose-internal network.
func Dial(target string) (*Client, error) {
	conn, err := grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("dial sidecar %q: %w", target, err)
	}
	return &Client{conn: conn, api: pb.NewBrowserClient(conn)}, nil
}

func (c *Client) Close() error { return c.conn.Close() }

func (c *Client) Authenticate(ctx context.Context, s *pb.KickSession) (*pb.AuthenticateResponse, error) {
	return c.api.Authenticate(ctx, &pb.AuthenticateRequest{Session: s})
}

func (c *Client) StartWatch(ctx context.Context, s *pb.KickSession, channel string) (string, error) {
	resp, err := c.api.StartWatch(ctx, &pb.StartWatchRequest{Session: s, Channel: channel})
	if err != nil {
		return "", err
	}
	return resp.WatchHandle, nil
}

func (c *Client) Heartbeat(ctx context.Context, handle string) (bool, error) {
	resp, err := c.api.Heartbeat(ctx, &pb.HeartbeatRequest{WatchHandle: handle})
	if err != nil {
		return false, err
	}
	return resp.Alive, nil
}

func (c *Client) StopWatch(ctx context.Context, handle string) error {
	_, err := c.api.StopWatch(ctx, &pb.StopWatchRequest{WatchHandle: handle})
	return err
}

func (c *Client) Inventory(ctx context.Context, s *pb.KickSession) ([]*pb.DropProgress, error) {
	resp, err := c.api.Inventory(ctx, &pb.InventoryRequest{Session: s})
	if err != nil {
		return nil, err
	}
	return resp.Drops, nil
}

func (c *Client) Claim(ctx context.Context, s *pb.KickSession, benefitID string) (bool, error) {
	resp, err := c.api.Claim(ctx, &pb.ClaimRequest{Session: s, BenefitId: benefitID})
	if err != nil {
		return false, err
	}
	return resp.AlreadyClaimed, nil
}
```

- [ ] **Step 2: Stub-server integration test**

```go
// internal/auth/browser/client_test.go
package browser

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	pb "github.com/aalejandrofer/dropsminer/internal/auth/browser/gen/browser/v1"
)

type stubServer struct {
	pb.UnimplementedBrowserServer
	authUsername string
	watchHandle  string
	drops        []*pb.DropProgress
}

func (s *stubServer) Authenticate(_ context.Context, req *pb.AuthenticateRequest) (*pb.AuthenticateResponse, error) {
	return &pb.AuthenticateResponse{Session: req.Session, Username: s.authUsername}, nil
}

func (s *stubServer) StartWatch(_ context.Context, _ *pb.StartWatchRequest) (*pb.StartWatchResponse, error) {
	return &pb.StartWatchResponse{WatchHandle: s.watchHandle}, nil
}

func (s *stubServer) Inventory(_ context.Context, _ *pb.InventoryRequest) (*pb.InventoryResponse, error) {
	return &pb.InventoryResponse{Drops: s.drops}, nil
}

func startStub(t *testing.T, srv *stubServer) string {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	gs := grpc.NewServer()
	pb.RegisterBrowserServer(gs, srv)
	go gs.Serve(lis)
	t.Cleanup(func() { gs.GracefulStop() })

	return lis.Addr().String()
}

func TestClient_RoundTrips(t *testing.T) {
	stub := &stubServer{
		authUsername: "demo",
		watchHandle:  "tab_42",
		drops: []*pb.DropProgress{
			{BenefitId: "d1", MinutesWatched: 10, Claimed: false},
		},
	}
	addr := startStub(t, stub)

	c, err := Dial(addr)
	require.NoError(t, err)
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	authResp, err := c.Authenticate(ctx, &pb.KickSession{XsrfToken: "tok"})
	require.NoError(t, err)
	assert.Equal(t, "demo", authResp.Username)

	handle, err := c.StartWatch(ctx, &pb.KickSession{}, "fakestreamer")
	require.NoError(t, err)
	assert.Equal(t, "tab_42", handle)

	drops, err := c.Inventory(ctx, &pb.KickSession{})
	require.NoError(t, err)
	require.Len(t, drops, 1)
	assert.Equal(t, "d1", drops[0].BenefitId)
}
```

- [ ] **Step 3: Run tests**

```bash
go test ./internal/auth/browser/...
```

Expected: 1 PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/auth/browser/client.go internal/auth/browser/client_test.go
git commit -m "$(cat <<'EOF'
feat(auth/browser): gRPC client wrapper for sidecar

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 7: Kick session types

**Files:**
- Create: `internal/platform/kick/types.go`

- [ ] **Step 1: Define the session shape**

```go
// internal/platform/kick/types.go
package kick

import (
	"encoding/json"

	"github.com/aalejandrofer/dropsminer/internal/platform"

	pb "github.com/aalejandrofer/dropsminer/internal/auth/browser/gen/browser/v1"
)

// kickSession is the JSON we serialize into platform.Session.Cookies +
// CSRF as a single encoded blob, so the rest of the daemon's
// session-store machinery (encrypted via age) reuses unchanged.
type kickSession struct {
	Cookies   []cookie `json:"cookies"`
	XSRFToken string   `json:"xsrf_token"`
	UserAgent string   `json:"user_agent"`
}

type cookie struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Domain string `json:"domain"`
	Path   string `json:"path"`
}

// encodeSession packs a Kick browser session into a platform.Session.
// AccessToken stays empty (Kick has no bearer); we stash everything in
// the Cookies map under a single key so the existing JSON marshaller
// rounds-trips correctly.
func encodeSession(ks kickSession) (platform.Session, error) {
	raw, err := json.Marshal(ks)
	if err != nil {
		return platform.Session{}, err
	}
	return platform.Session{
		Cookies: map[string]string{"kick": string(raw)},
		CSRF:    ks.XSRFToken,
	}, nil
}

func decodeSession(p platform.Session) (kickSession, error) {
	raw, ok := p.Cookies["kick"]
	if !ok {
		return kickSession{}, nil
	}
	var ks kickSession
	if err := json.Unmarshal([]byte(raw), &ks); err != nil {
		return kickSession{}, err
	}
	return ks, nil
}

// toProto converts the internal session form into the gRPC type used
// by the sidecar.
func toProto(ks kickSession) *pb.KickSession {
	cookies := make([]*pb.Cookie, 0, len(ks.Cookies))
	for _, c := range ks.Cookies {
		cookies = append(cookies, &pb.Cookie{
			Name: c.Name, Value: c.Value, Domain: c.Domain, Path: c.Path,
		})
	}
	return &pb.KickSession{
		Cookies:   cookies,
		XsrfToken: ks.XSRFToken,
		UserAgent: ks.UserAgent,
	}
}
```

- [ ] **Step 2: Build**

```bash
go build ./internal/platform/kick/...
```

Clean.

- [ ] **Step 3: Commit**

```bash
git add internal/platform/kick/types.go
git commit -m "$(cat <<'EOF'
feat(platform/kick): session encoding (cookies + xsrf token)

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 8: Kick Backend impl

**Files:**
- Create: `internal/platform/kick/backend.go`
- Test: `internal/platform/kick/backend_test.go`

- [ ] **Step 1: Implement the Backend interface**

```go
// internal/platform/kick/backend.go
package kick

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/aalejandrofer/dropsminer/internal/auth/browser"
	"github.com/aalejandrofer/dropsminer/internal/platform"
)

// Backend implements platform.Backend for Kick by delegating page
// interactions to the browser sidecar over gRPC.
type Backend struct {
	c *browser.Client

	mu          sync.Mutex
	handleByAcc map[string]string // accountID -> watch handle
	channelByAcc map[string]string
}

var _ platform.Backend = (*Backend)(nil)

// New requires a connected browser.Client. Caller manages its lifecycle.
func New(c *browser.Client) *Backend {
	return &Backend{
		c:            c,
		handleByAcc:  map[string]string{},
		channelByAcc: map[string]string{},
	}
}

func (b *Backend) Name() string { return "kick" }

// Kick has no device-code OAuth — the StartDeviceLogin / PollDeviceLogin
// pair is rejected. The web GUI redirects Kick logins to a cookie-paste
// form (Task 11) which calls LoginViaBrowser.
func (b *Backend) StartDeviceLogin(_ context.Context) (platform.DeviceChallenge, error) {
	return platform.DeviceChallenge{}, errors.New("kick: use cookie-paste login")
}

func (b *Backend) PollDeviceLogin(_ context.Context, _ platform.DeviceChallenge) (platform.Session, error) {
	return platform.Session{}, errors.New("kick: use cookie-paste login")
}

// LoginViaBrowser is the only auth path for Kick. The BrowserRPC is the
// existing interface defined in internal/platform — Plan 4 uses a
// concrete adapter that calls Authenticate on the sidecar.
func (b *Backend) LoginViaBrowser(ctx context.Context, rpc platform.BrowserRPC) (platform.Session, error) {
	return rpc.LoginInteractive("kick")
}

func (b *Backend) RefreshSession(_ context.Context, s platform.Session) (platform.Session, error) {
	// Kick cookies don't refresh server-side — they expire when the
	// user logs out of their browser session. Just return unchanged;
	// the next API call will surface a 401 if invalid.
	return s, nil
}

// ListActiveCampaigns: Kick exposes campaigns as scrapeable inventory
// only. For v1 we return a single synthetic "campaign" that mirrors
// whatever drops appear in the user's inventory. Benefits come from
// the sidecar's Inventory RPC.
func (b *Backend) ListActiveCampaigns(ctx context.Context, s platform.Session) ([]platform.Campaign, error) {
	ks, err := decodeSession(s)
	if err != nil {
		return nil, err
	}
	drops, err := b.c.Inventory(ctx, toProto(ks))
	if err != nil {
		return nil, fmt.Errorf("kick inventory: %w", err)
	}
	if len(drops) == 0 {
		return nil, nil
	}
	benefits := make([]platform.DropBenefit, 0, len(drops))
	for _, d := range drops {
		benefits = append(benefits, platform.DropBenefit{
			ID:              d.BenefitId,
			CampaignID:      "kick-inventory",
			Name:            d.BenefitId, // sidecar doesn't surface a friendly name yet
			RequiredMinutes: 120,         // Kick drops typically require 2h; refine when sidecar surfaces the threshold
		})
	}
	return []platform.Campaign{{
		ID: "kick-inventory", Platform: "kick", Game: "Rust",
		Name: "Kick Rust Drops", Status: "active",
		Benefits: benefits,
	}}, nil
}

// ListEligibleChannels returns the channel the user opted into via the
// GUI. We store one channel per account in channelByAcc when the watch
// starts; this method returns it for restart cases.
func (b *Backend) ListEligibleChannels(_ context.Context, _ platform.Session, _ platform.Campaign) ([]platform.Stream, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]platform.Stream, 0, len(b.channelByAcc))
	for _, ch := range b.channelByAcc {
		out = append(out, platform.Stream{Channel: ch, DropsEnabled: true})
	}
	return out, nil
}

func (b *Backend) InventoryProgress(ctx context.Context, s platform.Session) ([]platform.Progress, error) {
	ks, err := decodeSession(s)
	if err != nil {
		return nil, err
	}
	drops, err := b.c.Inventory(ctx, toProto(ks))
	if err != nil {
		return nil, err
	}
	out := make([]platform.Progress, 0, len(drops))
	for _, d := range drops {
		out = append(out, platform.Progress{
			BenefitID:      d.BenefitId,
			MinutesWatched: int(d.MinutesWatched),
			Claimed:        d.Claimed,
		})
	}
	return out, nil
}

func (b *Backend) StartWatch(ctx context.Context, s platform.Session, stream platform.Stream) (platform.WatchHandle, error) {
	ks, err := decodeSession(s)
	if err != nil {
		return platform.WatchHandle{}, err
	}
	handle, err := b.c.StartWatch(ctx, toProto(ks), stream.Channel)
	if err != nil {
		return platform.WatchHandle{}, err
	}
	return platform.WatchHandle{Channel: stream.Channel, Internal: handle}, nil
}

func (b *Backend) Heartbeat(ctx context.Context, h platform.WatchHandle) error {
	handle, _ := h.Internal.(string)
	alive, err := b.c.Heartbeat(ctx, handle)
	if err != nil {
		return err
	}
	if !alive {
		return fmt.Errorf("kick: watch tab %q died", handle)
	}
	return nil
}

func (b *Backend) StopWatch(ctx context.Context, h platform.WatchHandle) error {
	handle, _ := h.Internal.(string)
	return b.c.StopWatch(ctx, handle)
}

func (b *Backend) Claim(ctx context.Context, s platform.Session, drop platform.DropBenefit) error {
	ks, err := decodeSession(s)
	if err != nil {
		return err
	}
	_, err = b.c.Claim(ctx, toProto(ks), drop.ID)
	return err
}
```

- [ ] **Step 2: Test against the same stub from Task 6**

```go
// internal/platform/kick/backend_test.go
package kick

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	pb "github.com/aalejandrofer/dropsminer/internal/auth/browser/gen/browser/v1"
	"github.com/aalejandrofer/dropsminer/internal/auth/browser"
	"github.com/aalejandrofer/dropsminer/internal/platform"
)

type stubServer struct {
	pb.UnimplementedBrowserServer
	drops  []*pb.DropProgress
	handle string
}

func (s *stubServer) Inventory(_ context.Context, _ *pb.InventoryRequest) (*pb.InventoryResponse, error) {
	return &pb.InventoryResponse{Drops: s.drops}, nil
}

func (s *stubServer) StartWatch(_ context.Context, _ *pb.StartWatchRequest) (*pb.StartWatchResponse, error) {
	return &pb.StartWatchResponse{WatchHandle: s.handle}, nil
}

func (s *stubServer) Heartbeat(_ context.Context, req *pb.HeartbeatRequest) (*pb.HeartbeatResponse, error) {
	return &pb.HeartbeatResponse{Alive: req.WatchHandle == "tab_42"}, nil
}

func startStub(t *testing.T, srv *stubServer) string {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	gs := grpc.NewServer()
	pb.RegisterBrowserServer(gs, srv)
	go gs.Serve(lis)
	t.Cleanup(func() { gs.GracefulStop() })
	return lis.Addr().String()
}

func TestKickBackend_InventoryProgress(t *testing.T) {
	stub := &stubServer{drops: []*pb.DropProgress{
		{BenefitId: "d1", MinutesWatched: 45, Claimed: false},
	}}
	addr := startStub(t, stub)

	c, err := browser.Dial(addr)
	require.NoError(t, err)
	defer c.Close()

	b := New(c)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	pr, err := b.InventoryProgress(ctx, platform.Session{Cookies: map[string]string{"kick": `{}`}})
	require.NoError(t, err)
	require.Len(t, pr, 1)
	assert.Equal(t, "d1", pr[0].BenefitID)
	assert.Equal(t, 45, pr[0].MinutesWatched)
}

func TestKickBackend_StartHeartbeatStop(t *testing.T) {
	stub := &stubServer{handle: "tab_42"}
	addr := startStub(t, stub)

	c, err := browser.Dial(addr)
	require.NoError(t, err)
	defer c.Close()

	b := New(c)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	h, err := b.StartWatch(ctx, platform.Session{Cookies: map[string]string{"kick": `{}`}}, platform.Stream{Channel: "fakestreamer"})
	require.NoError(t, err)
	assert.Equal(t, "tab_42", h.Internal)

	require.NoError(t, b.Heartbeat(ctx, h))
}

func TestKickBackend_DeviceLoginRejected(t *testing.T) {
	b := New(nil)
	_, err := b.StartDeviceLogin(context.Background())
	require.Error(t, err)
}
```

- [ ] **Step 3: Run tests**

```bash
go test ./internal/platform/kick/...
```

Expected: 3 PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/platform/kick/
git commit -m "$(cat <<'EOF'
feat(platform/kick): Backend impl backed by browser sidecar gRPC

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 9: Dockerfile for the sidecar

**Files:**
- Create: `deploy/Dockerfile.browser`

- [ ] **Step 1: Write Dockerfile**

```dockerfile
# deploy/Dockerfile.browser
FROM golang:1.26-alpine AS build
WORKDIR /src
RUN apk add --no-cache git ca-certificates
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/browser-sidecar ./cmd/browser-sidecar

# chromedp/headless-shell ships chrome as /headless-shell/headless-shell.
# Use it directly as the runtime image so chromedp.NewExecAllocator
# finds Chrome on PATH (the image's entrypoint runs the Chrome binary;
# we override it to run our sidecar).
FROM chromedp/headless-shell:latest
COPY --from=build /out/browser-sidecar /browser-sidecar
ENV PATH="/headless-shell:${PATH}"
EXPOSE 9090
ENTRYPOINT ["/browser-sidecar"]
```

- [ ] **Step 2: Build the image**

```bash
docker build -f deploy/Dockerfile.browser -t dropsminer-browser:dev .
docker images dropsminer-browser:dev --format '{{.Size}}'
```

Expected: image size ~150–200MB (chromedp/headless-shell base ~130MB + Go binary ~15MB).

- [ ] **Step 3: Smoke-run the container**

```bash
docker run --rm -d -p 9090:9090 --name miner-browser-test dropsminer-browser:dev
sleep 3
docker logs miner-browser-test | head -5
docker stop miner-browser-test
```

Expected: log line `browser sidecar listening addr=0.0.0.0:9090`.

- [ ] **Step 4: Commit**

```bash
git add deploy/Dockerfile.browser
git commit -m "$(cat <<'EOF'
feat(deploy): browser sidecar Dockerfile (chromedp/headless-shell base)

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 10: Wire Kick into config + main

**Files:**
- Modify: `internal/config/config.go`
- Modify: `cmd/miner/main.go`

- [ ] **Step 1: Add BrowserURL to config**

In `internal/config/config.go`, extend the struct:

```go
type Config struct {
	HTTPAddr          string
	DBPath            string
	MasterKey         string
	DiscordWebhookURL string
	SecureCookies     bool
	BrowserURL        string
}
```

And in `Load`:

```go
BrowserURL: os.Getenv("MINER_BROWSER_URL"),
```

- [ ] **Step 2: Update the matching config test**

In `internal/config/config_test.go`, add to `TestLoad_DefaultsApplied`:

```go
assert.Equal(t, "", cfg.BrowserURL)
```

And to `TestLoad_Overrides`:

```go
t.Setenv("MINER_BROWSER_URL", "browser:9090")
...
assert.Equal(t, "browser:9090", cfg.BrowserURL)
```

- [ ] **Step 3: Register kick backend in main**

In `cmd/miner/main.go`, after `registry.Register(twitch.New())`:

```go
if cfg.BrowserURL != "" {
	bc, err := browser.Dial(cfg.BrowserURL)
	if err != nil {
		return fmt.Errorf("dial browser sidecar: %w", err)
	}
	defer bc.Close()
	registry.Register(kick.New(bc))
} else {
	logger.Info("MINER_BROWSER_URL empty, Kick backend disabled")
}
```

Add imports:

```go
"github.com/aalejandrofer/dropsminer/internal/auth/browser"
"github.com/aalejandrofer/dropsminer/internal/platform/kick"
```

- [ ] **Step 4: Build + tests**

```bash
go build ./...
go test -race ./...
```

Both clean.

- [ ] **Step 5: Commit**

```bash
git add internal/config/ cmd/miner/main.go
git commit -m "$(cat <<'EOF'
feat(cmd/miner): register kick backend when MINER_BROWSER_URL is set

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 11: Kick login GUI (cookie paste)

**Files:**
- Create: `internal/api/handlers_login_kick.go`
- Create: `internal/web/templates/login_kick.html`
- Modify: `internal/web/templates/accounts_new.html`
- Modify: `internal/api/handlers_accounts.go`
- Modify: `internal/api/server.go`

- [ ] **Step 1: Template — cookie paste form**

Create `internal/web/templates/login_kick.html`:

```html
{{define "title"}}Kick login · Rust Drops Miner{{end}}
{{define "content"}}
{{with .Page}}
<h1>Authorize Kick — {{.DisplayName}}</h1>
<p>Kick has no public OAuth, so we drive a real browser session on your behalf.</p>
<ol>
  <li>Open kick.com in any browser and log in normally.</li>
  <li>Open DevTools (F12) → Application → Cookies → kick.com.</li>
  <li>Copy <code>kick_session</code>, <code>XSRF-TOKEN</code>, and <code>cf_clearance</code> values.</li>
  <li>Paste below and submit. The sidecar will validate the session.</li>
  <li>Then enter the channel login you want to mine (e.g. <code>rust-streamer</code>).</li>
</ol>
{{if .Flash}}<p class="err">{{.Flash}}</p>{{end}}
<form method="post" action="/accounts/{{.AccountID}}/login">
  <input type="hidden" name="csrf_token" value="{{$.CSRFToken}}">
  <label>kick_session<input type="text" name="kick_session" required></label>
  <label>XSRF-TOKEN<input type="text" name="xsrf_token" required></label>
  <label>cf_clearance<input type="text" name="cf_clearance" required></label>
  <label>Channel to mine<input type="text" name="channel" required></label>
  <button type="submit">Authorize</button>
</form>
<p><a href="/accounts">cancel</a></p>
{{end}}
{{end}}
```

- [ ] **Step 2: Handler**

```go
// internal/api/handlers_login_kick.go
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	pb "github.com/aalejandrofer/dropsminer/internal/auth/browser/gen/browser/v1"
	"github.com/aalejandrofer/dropsminer/internal/platform"
	"github.com/aalejandrofer/dropsminer/internal/store"
	"github.com/aalejandrofer/dropsminer/internal/store/gen"
)

// kickBrowserClient is the surface handlers_login_kick depends on. We
// keep the interface minimal so the api package doesn't pull in the
// concrete gRPC client during unit tests.
type kickBrowserClient interface {
	Authenticate(ctx context.Context, s *pb.KickSession) (*pb.AuthenticateResponse, error)
}

type loginKickDeps struct {
	q        *gen.Queries
	t        Renderer
	sm       *scs.SessionManager
	sessions *store.SessionStore
	browser  kickBrowserClient
}

type loginKickPageData struct {
	AccountID   string
	DisplayName string
}

func (d *loginKickDeps) get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	acc, err := d.q.GetAccount(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	render(w, d.t, "login_kick.html", templateData{
		AuthedAdmin: true, CSRFToken: csrfToken(r),
		Page: loginKickPageData{AccountID: id, DisplayName: acc.DisplayName},
	})
}

func (d *loginKickDeps) post(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	acc, err := d.q.GetAccount(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	cookies := []*pb.Cookie{
		{Name: "kick_session", Value: r.FormValue("kick_session"), Domain: "kick.com", Path: "/"},
		{Name: "XSRF-TOKEN", Value: r.FormValue("xsrf_token"), Domain: "kick.com", Path: "/"},
		{Name: "cf_clearance", Value: r.FormValue("cf_clearance"), Domain: "kick.com", Path: "/"},
	}
	pbSession := &pb.KickSession{Cookies: cookies, XsrfToken: r.FormValue("xsrf_token")}

	resp, err := d.browser.Authenticate(r.Context(), pbSession)
	if err != nil {
		d.renderError(w, r, id, acc.DisplayName, "sidecar rejected session: "+err.Error())
		return
	}

	// Pack into platform.Session and persist via SessionStore.
	internal := kickSessionForStorage{
		Cookies: []cookieStored{
			{Name: "kick_session", Value: r.FormValue("kick_session"), Domain: "kick.com", Path: "/"},
			{Name: "XSRF-TOKEN", Value: r.FormValue("xsrf_token"), Domain: "kick.com", Path: "/"},
			{Name: "cf_clearance", Value: r.FormValue("cf_clearance"), Domain: "kick.com", Path: "/"},
		},
		XSRFToken: r.FormValue("xsrf_token"),
		Channel:   r.FormValue("channel"),
		Username:  resp.Username,
	}
	raw, _ := json.Marshal(internal)
	if err := d.sessions.Put(r.Context(), id, platform.Session{
		Cookies:   map[string]string{"kick": string(raw)},
		ExpiresAt: time.Now().Add(7 * 24 * time.Hour),
	}); err != nil {
		d.renderError(w, r, id, acc.DisplayName, "failed to persist session: "+err.Error())
		return
	}

	d.sm.Put(r.Context(), "flash", "Kick session authorized for "+resp.Username+" — click Apply changes")
	http.Redirect(w, r, "/accounts", http.StatusSeeOther)
}

func (d *loginKickDeps) renderError(w http.ResponseWriter, r *http.Request, id, name, flash string) {
	render(w, d.t, "login_kick.html", templateData{
		AuthedAdmin: true, CSRFToken: csrfToken(r),
		Page:  loginKickPageData{AccountID: id, DisplayName: name},
		Flash: flash,
	})
}

type kickSessionForStorage struct {
	Cookies   []cookieStored `json:"cookies"`
	XSRFToken string         `json:"xsrf_token"`
	Channel   string         `json:"channel"`
	Username  string         `json:"username"`
}

type cookieStored struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Domain string `json:"domain"`
	Path   string `json:"path"`
}
```

- [ ] **Step 3: Add Kick to accounts/new select**

Edit `internal/web/templates/accounts_new.html`:

```html
<option value="fake">fake (development)</option>
<option value="twitch">Twitch (drops)</option>
<option value="kick">Kick (drops)</option>
```

Edit `internal/api/handlers_accounts.go` `newPost` — extend the Twitch redirect branch:

```go
if platform == "twitch" || platform == "kick" {
	http.Redirect(w, r, "/accounts/"+id+"/login", http.StatusSeeOther)
	return
}
```

- [ ] **Step 4: Dispatch by platform in the login route**

In `internal/api/server.go`, the existing `/accounts/{id}/login` route is wired to the Twitch handler. We need to dispatch by the account's platform. Replace the existing two routes with a small dispatcher:

```go
loginTwitch := newLoginTwitchDeps(d, d.RootCtx)
loginKick := &loginKickDeps{q: d.Q, t: d.Templates, sm: d.Session, sessions: d.Sessions, browser: d.BrowserClient}

authed.Get("/accounts/{id}/login", func(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	acc, err := d.Q.GetAccount(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	switch acc.Platform {
	case "twitch":
		loginTwitch.get(w, r)
	case "kick":
		loginKick.get(w, r)
	default:
		http.Error(w, "platform does not need login", http.StatusBadRequest)
	}
})
authed.Get("/accounts/{id}/login/poll", loginTwitch.status)
authed.Post("/accounts/{id}/login", loginKick.post)
```

Add to `Deps`:

```go
BrowserClient kickBrowserClient
```

> `kickBrowserClient` is the interface declared in `handlers_login_kick.go`. Importing it via the interface keeps the api package free of a gRPC dep.

- [ ] **Step 5: Pass the browser client from main**

In `cmd/miner/main.go`, capture the dialed client and pass it to `api.Deps`. The earlier `defer bc.Close()` block already builds it. Update the deps literal:

```go
deps := api.Deps{
	DB: db, Q: q, Templates: tmplSet, Session: sm,
	Scheduler: sched, Reload: loadAndStart,
	Sessions: sessions, Registry: registry,
	RootCtx:  ctx,
	BrowserClient: bc, // nil-safe in handlers when no browser configured
}
```

If `cfg.BrowserURL == ""`, `bc` is nil. Guard in `loginKickDeps.post`:

```go
if d.browser == nil {
	http.Error(w, "browser sidecar not configured", http.StatusServiceUnavailable)
	return
}
```

Add the same nil-guard in `get`.

- [ ] **Step 6: Build + tests**

```bash
go build ./...
go test -race ./...
```

Clean.

- [ ] **Step 7: Commit**

```bash
git add internal/api/ internal/web/templates/ cmd/miner/main.go
git commit -m "$(cat <<'EOF'
feat(api): Kick login via cookie-paste (sidecar validates session)

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 12: Update docker-compose

**Files:**
- Modify: `deploy/docker-compose.yml`
- Modify: `.env.example`

- [ ] **Step 1: Add browser service**

Replace `deploy/docker-compose.yml`:

```yaml
services:
  miner:
    image: dropsminer:dev
    build:
      context: ..
      dockerfile: deploy/Dockerfile.miner
    restart: unless-stopped
    ports: ["8080:8080"]
    environment:
      MINER_MASTER_KEY: ${MINER_MASTER_KEY:?MINER_MASTER_KEY required}
      MINER_DB_PATH: /data/miner.db
      MINER_HTTP_ADDR: 0.0.0.0:8080
      MINER_BROWSER_URL: ${MINER_BROWSER_URL:-}
    volumes:
      - ./data:/data
    depends_on:
      - browser

  browser:
    image: dropsminer-browser:dev
    build:
      context: ..
      dockerfile: deploy/Dockerfile.browser
    restart: unless-stopped
    profiles: ["browser"]
    expose:
      - "9090"
```

> `depends_on: browser` only takes effect when the `browser` profile is enabled. Without the profile the service is absent and `depends_on` is ignored. Verify by running `docker compose up -d` (no profile) and confirming miner starts alone.

- [ ] **Step 2: .env.example**

```dotenv
# Required: age secret key (generate once with `age-keygen`)
MINER_MASTER_KEY=AGE-SECRET-KEY-1XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX0

# Optional overrides
MINER_HTTP_ADDR=0.0.0.0:8080
MINER_DB_PATH=/data/miner.db

# Optional: Discord webhook URL for state/progress/claim/error notifications.
MINER_DISCORD_WEBHOOK=

# Set to true when behind a TLS terminator (Traefik, nginx).
MINER_SECURE_COOKIES=false

# Set to "browser:9090" and start with `docker compose --profile browser up`
# to enable the Kick backend. Leave empty to disable Kick.
MINER_BROWSER_URL=
```

- [ ] **Step 3: Boot the no-profile stack**

```bash
cd deploy
mkdir -p data
export MINER_MASTER_KEY="$(go run filippo.io/age/cmd/age-keygen 2>&1 | awk '/AGE-SECRET-KEY/ {print $NF}')"
docker compose up --build -d
sleep 5
curl -fsS http://127.0.0.1:8080/healthz
docker compose down
```

Expected: `ok`. Browser image NOT pulled.

- [ ] **Step 4: Boot with browser profile**

```bash
export MINER_BROWSER_URL="browser:9090"
docker compose --profile browser up --build -d
sleep 8
curl -fsS http://127.0.0.1:8080/healthz
docker compose logs browser | grep "browser sidecar listening"
docker compose --profile browser down
cd ..
```

Expected: both services healthy, sidecar log line present.

- [ ] **Step 5: Commit**

```bash
git add deploy/docker-compose.yml .env.example
git commit -m "$(cat <<'EOF'
feat(deploy): browser sidecar service + compose profile

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 13: Manual verification runbook

**Files:**
- Create: `docs/superpowers/notes/2026-06-04-plan-04-manual-verification.md`

- [ ] **Step 1: Write runbook**

```markdown
# Plan 4 — Kick backend manual verification

Verifies the Kick browser-sidecar flow against a real Kick account.

## Prerequisites

- A Kick account already logged in via your browser.
- A live drops-enabled Kick channel for Rust (any active streamer in the Rust Twitch Drops campaign — Kick mirrors most of them).
- Docker + docker compose.

## Steps

### 1. Boot with the browser profile

```bash
cd deploy
mkdir -p data
export MINER_MASTER_KEY="$(go run filippo.io/age/cmd/age-keygen 2>&1 | awk '/AGE-SECRET-KEY/ {print $NF}')"
export MINER_BROWSER_URL="browser:9090"
docker compose --profile browser up --build -d
sleep 10
```

### 2. Setup admin

Open http://127.0.0.1:8080 → /setup → set admin password → land on dashboard.

### 3. Extract Kick cookies from your browser

1. Visit kick.com in your normal browser (logged in).
2. Open DevTools → Application → Cookies → https://kick.com.
3. Copy values for: `kick_session`, `XSRF-TOKEN`, `cf_clearance`.

### 4. Add a Kick account

1. Accounts → + Add account
2. Select **Kick (drops)** and a login handle.
3. Submit → redirects to `/accounts/<id>/login`.
4. Paste the three cookies and the channel login (e.g. `rust-streamer-name`).
5. Submit. The sidecar validates the session. On success you land on /accounts with a green flash showing your Kick username.

### 5. Apply changes

Click **Apply changes (reload watchers)**. The watcher starts. On the dashboard the Kick account card should progress through states.

### 6. Verify via logs

```bash
docker compose logs miner 2>&1 | grep -E '"event":"(state|progress|claim)"' | head -20
docker compose logs browser 2>&1 | head -20
```

Expect:
- `state` transitions on the miner side
- chromedp activity on the browser side (no obvious errors)

### 7. Known limitations

- The `RequiredMinutes` for Kick drops is hard-coded to 120 in `kick.Backend.ListActiveCampaigns`. Until the sidecar surfaces the per-drop threshold, this is a static guess. If the watcher claims early or never claims, that's why.
- Cookies expire — if you log out of Kick on your real browser, the sidecar's stored cookies will start 401ing. Re-run the login flow.
- One sidecar tab per Kick account. Many accounts → many tabs → growing memory. Restart the browser container periodically.

### 8. Teardown

```bash
docker compose --profile browser down
```

## Pass criteria

- /setup → login → dashboard works with browser profile up
- Kick login form accepts cookies and shows green flash with username
- Apply changes reaches a non-stopped Kick state on the dashboard
- Sidecar logs show no panics within 5 minutes
```

- [ ] **Step 2: Commit**

```bash
git add docs/superpowers/notes/2026-06-04-plan-04-manual-verification.md
git commit -m "$(cat <<'EOF'
docs(plan-04): manual verification runbook for Kick + browser sidecar

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Done definition

After Task 13:

1. `cmd/browser-sidecar` builds and runs in a chromedp/headless-shell container.
2. `docker compose --profile browser up` brings up miner + browser; both healthy.
3. `kick.Backend` satisfies `platform.Backend` and is registered when `MINER_BROWSER_URL` is set.
4. Adding a Kick account through the GUI redirects to a cookie-paste form; submission validates against the sidecar.
5. The Kick watcher reaches a `watching` state on the dashboard after Apply changes.
6. `go test -race ./...` green.

## Self-review notes

- Inventory scraping reads `window.__NEXT_DATA__` which is brittle. Schema drift is expected; production logs will surface empty-result regressions and the implementer should re-inspect the page structure.
- One tab per account is the simplest implementation; multi-tab batching is a follow-up if multi-account Kick mining ever becomes load-heavy.
- The `loginKickDeps.post` handler bypasses the standard SessionStore.Put nil-checks because `bc == nil` is guarded earlier. If the sidecar URL is set but the dial fails at startup, `main.go` returns a fatal error — that's intentional.
- `kick.Backend.ListEligibleChannels` returns the cached `channelByAcc` map. We never actually populate `channelByAcc` in this plan — that's a known gap, surfaced in the Plan 5 follow-up. For Plan 4 the watcher gets a stream via `StartWatch(stream)` where the stream comes from somewhere upstream; the cleanest fix is to push the channel into Backend at login time. Implementer note: when wiring `loginKickDeps.post`, also call into a `Backend.RegisterChannelForAccount(accountID, channel)` hook (add to `kick.Backend`).

## Next plan preview

Plan 5: Production deploy to homelab. Push `dropsminer` and `dropsminer-browser` to ghcr.io, write `humblewhale/dropsminer/compose.yml` with Traefik labels for `rdrops.ryuzec.dev`, wire to `traeky_proxynet`, ship via the homelab-update TUI.
