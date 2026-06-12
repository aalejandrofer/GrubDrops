# On-demand Kick Sidecars Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stop each Kick account's chromedp sidecar container when that account has had nothing to watch for 10 minutes, and start it on demand when a live channel reappears — freeing Chrome RAM during lulls and entirely between drop events.

**Architecture:** A new `dockerctl` package controls containers by name over the mounted Docker socket. The Kick backend derives each account's sidecar name from its username (`grubdrops-browser-<slug>`), tracks per-account `lastActive` (bumped on StartWatch + Heartbeat), starts the container on demand inside `StartWatch`, and runs a 1-minute reaper that stops containers idle past the grace window. Detection stays pure-HTTP, so the daemon needs no browser while sidecars are stopped. Degrades to always-on if the socket is unavailable.

**Tech Stack:** Go, `github.com/docker/docker/client` (Engine SDK), existing chromedp gRPC sidecar, compose.

Spec: `docs/superpowers/specs/2026-06-12-on-demand-kick-sidecars-design.md`
Base branch: `agent-a12f334b3f499cfa9` (per-account sidecar pool, commit `7fe74ac`).

---

## File Structure

- `internal/dockerctl/dockerctl.go` (new) — `Controller` interface + Docker-SDK impl. Sole responsibility: start/stop/inspect a container by name.
- `internal/dockerctl/dockerctl_test.go` (new) — interface behavior via a fake engine.
- `internal/platform/kick/sidecars.go` (new) — `slugify`, sidecar-name derivation, the `sidecar` struct + registry, `EnsureSidecarUp`, reaper. Keeps lifecycle logic out of the large `backend.go`.
- `internal/platform/kick/sidecars_test.go` (new) — slugify + registry + lifecycle/reaper tests with a fake `Controller`.
- `internal/platform/kick/backend.go` (modify) — `New` takes a `dockerctl.Controller` + template; `RegisterSidecar(accountID, username)`; `StartWatch`/`Heartbeat` call into the registry; remove the old round-robin pool.
- `internal/config/config.go` (modify) — `KickSidecarTemplate`, `KickSidecarPort`.
- `cmd/miner/main.go` (modify) — build `dockerctl.Controller`, pass to `kick.New`, call `RegisterSidecar` in the per-account build loop.
- `deploy/docker-compose.yml` + host `compose.yml` (modify) — rename sidecars, mount docker socket on `grubdrops`.
- host `.env` (modify) — optional template; drop `GRUB_BROWSER_URLS`.

---

## Task 1: `dockerctl` package

**Files:**
- Create: `internal/dockerctl/dockerctl.go`
- Test: `internal/dockerctl/dockerctl_test.go`

- [ ] **Step 1: Add the Docker SDK dependency**

Run:
```bash
cd "$(git rev-parse --show-toplevel)"
go get github.com/docker/docker/client@v27.3.1
go mod tidy
```
Expected: `go.mod` gains `github.com/docker/docker`. (If v27.3.1 is unavailable, use the latest `go get github.com/docker/docker/client` resolves.)

- [ ] **Step 2: Write the failing test (fake engine)**

```go
package dockerctl

import (
	"context"
	"errors"
	"testing"
)

type fakeEngine struct {
	running   map[string]bool
	startErr  error
	stopErr   error
	startCalls []string
	stopCalls  []string
}

func (f *fakeEngine) start(_ context.Context, name string) error {
	f.startCalls = append(f.startCalls, name)
	if f.startErr != nil {
		return f.startErr
	}
	f.running[name] = true
	return nil
}
func (f *fakeEngine) stop(_ context.Context, name string) error {
	f.stopCalls = append(f.stopCalls, name)
	if f.stopErr != nil {
		return f.stopErr
	}
	f.running[name] = false
	return nil
}
func (f *fakeEngine) running(_ context.Context, name string) (bool, error) {
	return f.running[name], nil
}

func TestController_StartStopRunning(t *testing.T) {
	f := &fakeEngine{running: map[string]bool{"box": false}}
	c := &Client{eng: f}
	ctx := context.Background()

	if err := c.Start(ctx, "box"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	ok, _ := c.Running(ctx, "box")
	if !ok {
		t.Fatal("want running after Start")
	}
	if err := c.Stop(ctx, "box"); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	ok, _ = c.Running(ctx, "box")
	if ok {
		t.Fatal("want stopped after Stop")
	}
}

func TestController_StartErrorPropagates(t *testing.T) {
	f := &fakeEngine{running: map[string]bool{}, startErr: errors.New("boom")}
	c := &Client{eng: f}
	if err := c.Start(context.Background(), "box"); err == nil {
		t.Fatal("want error")
	}
}
```

- [ ] **Step 3: Run the test, verify it fails**

Run: `go test ./internal/dockerctl/ -v`
Expected: FAIL — `Client`/`eng` undefined.

- [ ] **Step 4: Implement `dockerctl.go`**

```go
// Package dockerctl controls containers by name over the host Docker socket.
// Sole responsibility: start/stop/inspect — no daemon policy lives here.
package dockerctl

import (
	"context"
	"fmt"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

// Controller is the narrow surface the rest of the app depends on, so callers
// can fake it and a nil Controller can mean "degrade to no container control".
type Controller interface {
	Start(ctx context.Context, name string) error
	Stop(ctx context.Context, name string) error
	Running(ctx context.Context, name string) (bool, error)
}

// engine is the slice of the Docker SDK we use; the real one wraps *client.Client.
type engine interface {
	start(ctx context.Context, name string) error
	stop(ctx context.Context, name string) error
	running(ctx context.Context, name string) (bool, error)
}

type Client struct{ eng engine }

// New connects to the Docker daemon via the ambient environment
// (DOCKER_HOST or /var/run/docker.sock). Returns an error if unreachable so
// the caller can fall back to always-on sidecars.
func New() (*Client, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("docker client: %w", err)
	}
	if _, err := cli.Ping(context.Background()); err != nil {
		return nil, fmt.Errorf("docker ping: %w", err)
	}
	return &Client{eng: &sdkEngine{cli: cli}}, nil
}

func (c *Client) Start(ctx context.Context, name string) error { return c.eng.start(ctx, name) }
func (c *Client) Stop(ctx context.Context, name string) error  { return c.eng.stop(ctx, name) }
func (c *Client) Running(ctx context.Context, name string) (bool, error) {
	return c.eng.running(ctx, name)
}

type sdkEngine struct{ cli *client.Client }

func (s *sdkEngine) start(ctx context.Context, name string) error {
	return s.cli.ContainerStart(ctx, name, container.StartOptions{})
}
func (s *sdkEngine) stop(ctx context.Context, name string) error {
	t := 15 // seconds
	return s.cli.ContainerStop(ctx, name, container.StopOptions{Timeout: &t})
}
func (s *sdkEngine) running(ctx context.Context, name string) (bool, error) {
	info, err := s.cli.ContainerInspect(ctx, name)
	if err != nil {
		return false, err
	}
	return info.State != nil && info.State.Running, nil
}

var _ = time.Second // keep time import if unused after edits
```

(Remove the trailing `var _ = time.Second` and the `time` import if `go build` flags them unused.)

- [ ] **Step 5: Run tests, verify pass + build**

Run: `go test ./internal/dockerctl/ -v && go build ./...`
Expected: PASS; build OK.

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum internal/dockerctl/
git commit -m "feat(dockerctl): container start/stop/inspect over docker socket"
```

---

## Task 2: slugify + sidecar name derivation

**Files:**
- Create: `internal/platform/kick/sidecars.go`
- Test: `internal/platform/kick/sidecars_test.go`

- [ ] **Step 1: Write the failing test**

```go
package kick

import "testing"

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"TTik3r":       "ttik3r",
		"Phluses":      "phluses",
		"Cool_Name 99": "cool-name-99",
		"--weird--":    "weird",
		"":             "",
	}
	for in, want := range cases {
		if got := slugify(in); got != want {
			t.Errorf("slugify(%q)=%q want %q", in, got, want)
		}
	}
}

func TestSidecarName(t *testing.T) {
	tmpl := "grubdrops-browser-{slug}"
	if got := sidecarName(tmpl, "TTik3r"); got != "grubdrops-browser-ttik3r" {
		t.Fatalf("got %q", got)
	}
	// empty slug → empty name (caller treats as "no controllable sidecar")
	if got := sidecarName(tmpl, ""); got != "" {
		t.Fatalf("empty username should yield empty name, got %q", got)
	}
}
```

- [ ] **Step 2: Run, verify fail**

Run: `go test ./internal/platform/kick/ -run 'TestSlugify|TestSidecarName' -v`
Expected: FAIL — `slugify`/`sidecarName` undefined.

- [ ] **Step 3: Implement in `sidecars.go`**

```go
package kick

import "strings"

// slugify lowercases s and collapses every run of chars outside [a-z0-9] to a
// single '-', trimming leading/trailing '-'. Deterministic so the derived
// container name always matches what compose declares.
func slugify(s string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevDash = false
		} else if !prevDash {
			b.WriteByte('-')
			prevDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

// sidecarName fills {slug} in the template from the username. Returns "" when
// the username yields an empty slug (caller treats as no controllable sidecar).
func sidecarName(template, username string) string {
	slug := slugify(username)
	if slug == "" {
		return ""
	}
	return strings.ReplaceAll(template, "{slug}", slug)
}
```

- [ ] **Step 4: Run, verify pass**

Run: `go test ./internal/platform/kick/ -run 'TestSlugify|TestSidecarName' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/platform/kick/sidecars.go internal/platform/kick/sidecars_test.go
git commit -m "feat(kick): username->sidecar-name slugify"
```

---

## Task 3: sidecar registry + lifecycle + reaper

**Files:**
- Modify: `internal/platform/kick/sidecars.go`
- Test: `internal/platform/kick/sidecars_test.go`

- [ ] **Step 1: Write the failing test (registry, EnsureSidecarUp, reaper)**

Add to `sidecars_test.go`:

```go
import (
	"context"
	"sync"
	"testing"
	"time"
)

// fakeCtl implements dockerctl.Controller for tests.
type fakeCtl struct {
	mu      sync.Mutex
	run     map[string]bool
	starts  []string
	stops   []string
}

func newFakeCtl() *fakeCtl { return &fakeCtl{run: map[string]bool{}} }
func (f *fakeCtl) Start(_ context.Context, n string) error {
	f.mu.Lock(); defer f.mu.Unlock(); f.run[n] = true; f.starts = append(f.starts, n); return nil
}
func (f *fakeCtl) Stop(_ context.Context, n string) error {
	f.mu.Lock(); defer f.mu.Unlock(); f.run[n] = false; f.stops = append(f.stops, n); return nil
}
func (f *fakeCtl) Running(_ context.Context, n string) (bool, error) {
	f.mu.Lock(); defer f.mu.Unlock(); return f.run[n], nil
}
func (f *fakeCtl) stopCount() int { f.mu.Lock(); defer f.mu.Unlock(); return len(f.stops) }

func TestRegistry_ReaperStopsIdleRunning(t *testing.T) {
	ctl := newFakeCtl()
	ctl.run["grubdrops-browser-ttik3r"] = true
	reg := newSidecarRegistry(ctl, "grubdrops-browser-{slug}", 9090, 50*time.Millisecond)
	reg.register("acc1", "TTik3r")
	// lastActive far in the past → reaper should stop it.
	reg.touchAt("acc1", time.Now().Add(-time.Hour))

	reg.reapOnce(context.Background())
	if ctl.stopCount() != 1 {
		t.Fatalf("want 1 stop, got %d", ctl.stopCount())
	}
}

func TestRegistry_ReaperKeepsFreshAndStopped(t *testing.T) {
	ctl := newFakeCtl()
	ctl.run["grubdrops-browser-ttik3r"] = true   // fresh, running
	ctl.run["grubdrops-browser-phluses"] = false // idle but already stopped
	reg := newSidecarRegistry(ctl, "grubdrops-browser-{slug}", 9090, 50*time.Millisecond)
	reg.register("acc1", "TTik3r")
	reg.register("acc2", "Phluses")
	reg.touchAt("acc1", time.Now())                    // fresh
	reg.touchAt("acc2", time.Now().Add(-time.Hour))    // idle but stopped

	reg.reapOnce(context.Background())
	if ctl.stopCount() != 0 {
		t.Fatalf("want 0 stops, got %d", ctl.stopCount())
	}
}

func TestRegistry_NilControllerDegrades(t *testing.T) {
	reg := newSidecarRegistry(nil, "grubdrops-browser-{slug}", 9090, time.Minute)
	reg.register("acc1", "TTik3r")
	// must not panic / must be a no-op
	if err := reg.ensureUp(context.Background(), "acc1", func(context.Context) error { return nil }); err != nil {
		t.Fatalf("nil controller ensureUp: %v", err)
	}
	reg.reapOnce(context.Background())
}
```

- [ ] **Step 2: Run, verify fail**

Run: `go test ./internal/platform/kick/ -run TestRegistry -v`
Expected: FAIL — `newSidecarRegistry` etc. undefined.

- [ ] **Step 3: Implement registry in `sidecars.go`**

Append:

```go
import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/dockerctl"
)

type sidecar struct {
	containerName string // "" = no controllable container (empty slug)
	lastActive    time.Time
}

type sidecarRegistry struct {
	ctl       dockerctl.Controller // nil = degrade (never start/stop)
	template  string
	port      int
	idleGrace time.Duration

	mu  sync.Mutex
	byAcc map[string]*sidecar
}

func newSidecarRegistry(ctl dockerctl.Controller, template string, port int, idleGrace time.Duration) *sidecarRegistry {
	return &sidecarRegistry{ctl: ctl, template: template, port: port, idleGrace: idleGrace, byAcc: map[string]*sidecar{}}
}

// register derives the account's sidecar from its username. Safe to call again
// (e.g. on Reload) — updates the name, preserves lastActive.
func (r *sidecarRegistry) register(accountID, username string) {
	name := sidecarName(r.template, username)
	r.mu.Lock()
	defer r.mu.Unlock()
	if s, ok := r.byAcc[accountID]; ok {
		s.containerName = name
		return
	}
	r.byAcc[accountID] = &sidecar{containerName: name}
}

func (r *sidecarRegistry) touch(accountID string)               { r.touchAt(accountID, time.Now()) }
func (r *sidecarRegistry) touchAt(accountID string, t time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if s, ok := r.byAcc[accountID]; ok {
		s.lastActive = t
	}
}

// ensureUp starts the account's container (if controllable + stopped) and waits
// for readiness via the supplied probe, then bumps lastActive. No-op when no
// controller or no controllable container.
func (r *sidecarRegistry) ensureUp(ctx context.Context, accountID string, ready func(context.Context) error) error {
	r.mu.Lock()
	s := r.byAcc[accountID]
	ctl := r.ctl
	r.mu.Unlock()
	if ctl == nil || s == nil || s.containerName == "" {
		return nil
	}
	running, err := ctl.Running(ctx, s.containerName)
	if err != nil {
		slog.Warn("kick sidecar inspect failed; assuming up", "container", s.containerName, "err", err)
		r.touch(accountID)
		return nil
	}
	if !running {
		slog.Info("kick sidecar starting on demand", "container", s.containerName, "account", accountID)
		if err := ctl.Start(ctx, s.containerName); err != nil {
			return err
		}
		// Wait for the gRPC server to accept calls.
		deadline := time.Now().Add(30 * time.Second)
		for {
			if err := ready(ctx); err == nil {
				break
			}
			if time.Now().After(deadline) {
				return context.DeadlineExceeded
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Second):
			}
		}
		slog.Info("kick sidecar ready", "container", s.containerName, "account", accountID)
	}
	r.touch(accountID)
	return nil
}

// reapOnce stops every controllable container whose account has been idle
// longer than idleGrace and is currently running.
func (r *sidecarRegistry) reapOnce(ctx context.Context) {
	if r.ctl == nil {
		return
	}
	type cand struct{ acc, name string }
	var cands []cand
	cutoff := time.Now().Add(-r.idleGrace)
	r.mu.Lock()
	for acc, s := range r.byAcc {
		if s.containerName == "" || s.lastActive.IsZero() || s.lastActive.After(cutoff) {
			continue
		}
		cands = append(cands, cand{acc, s.containerName})
	}
	r.mu.Unlock()
	for _, c := range cands {
		running, err := r.ctl.Running(ctx, c.name)
		if err != nil || !running {
			continue
		}
		slog.Info("kick sidecar idle, stopping", "container", c.name, "account", c.acc, "grace", r.idleGrace)
		if err := r.ctl.Stop(ctx, c.name); err != nil {
			slog.Warn("kick sidecar stop failed", "container", c.name, "err", err)
		}
	}
}

// runReaper ticks reapOnce every minute until ctx is done.
func (r *sidecarRegistry) runReaper(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			r.reapOnce(ctx)
		}
	}
}
```

(Merge this `import` block into the file's existing single import; do not leave two import blocks.)

- [ ] **Step 4: Run, verify pass**

Run: `go test ./internal/platform/kick/ -run TestRegistry -v && go build ./...`
Expected: PASS; build OK.

- [ ] **Step 5: Commit**

```bash
git add internal/platform/kick/sidecars.go internal/platform/kick/sidecars_test.go
git commit -m "feat(kick): on-demand sidecar registry + idle reaper"
```

---

## Task 4: wire registry into the backend + config + main

**Files:**
- Modify: `internal/platform/kick/backend.go`
- Modify: `internal/config/config.go`
- Modify: `cmd/miner/main.go`
- Test: `internal/platform/kick/backend_test.go`

- [ ] **Step 1: Config — add template + port**

In `internal/config/config.go`, add fields after `BrowserURLs`:

```go
	// KickSidecarTemplate names each Kick account's sidecar container from its
	// username slug ("{slug}" placeholder). Default "grubdrops-browser-{slug}".
	KickSidecarTemplate string
	KickSidecarPort     int
```

In `Load()` after the `BrowserURLs` block:

```go
	cfg.KickSidecarTemplate = getenv("GRUB_KICK_SIDECAR_TEMPLATE", "grubdrops-browser-{slug}")
	cfg.KickSidecarPort = 9090
	if v := os.Getenv("GRUB_KICK_SIDECAR_PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.KickSidecarPort = n
		}
	}
```

- [ ] **Step 2: Backend — replace pool with registry**

In `internal/platform/kick/backend.go`:

Replace the pool fields (added in commit `7fe74ac`):
```go
	watchPool   []*browser.Client
	clientByAcc map[string]*browser.Client
	nextClient  int
```
with:
```go
	// sidecars manages on-demand start/stop + per-account client pinning for
	// the IVS watch path. clientByName dials one gRPC client per derived
	// container name (lazy connect, survives container restarts).
	sidecars    *sidecarRegistry
	clientByName map[string]*browser.Client
	clientMu    sync.Mutex
	sidecarPort int
```

Change `New` to accept the controller + template/port and the login client; build the registry and start the reaper. Replace the whole `New(...)` and `watchClientFor(...)` from Task in `7fe74ac` with:

```go
func New(c *browser.Client, ctl dockerctl.Controller, template string, port int, idleGrace time.Duration) *Backend {
	b := &Backend{
		c:                c,
		api:              newAPI(),
		sidecars:         newSidecarRegistry(ctl, template, port, idleGrace),
		clientByName:     map[string]*browser.Client{},
		sidecarPort:      port,
		handleByAcc:      map[string]string{},
		channelsByAcc:    map[string][]string{},
		campaignChannels: map[string][]kickChannel{},
		categoryChannels: map[string][]kickChannel{},
	}
	if ctl != nil {
		go b.sidecars.runReaper(context.Background())
	}
	return b
}

// RegisterSidecar maps an account to its username-derived sidecar. Called at
// startup (per-account build loop) and on login.
func (b *Backend) RegisterSidecar(accountID, username string) {
	b.sidecars.register(accountID, username)
}

// watchClientForName returns (dialing once) the gRPC client for a container
// name. Lazy connect means a stopped container is fine until first RPC.
func (b *Backend) watchClientForName(name string) (*browser.Client, error) {
	b.clientMu.Lock()
	defer b.clientMu.Unlock()
	if cl, ok := b.clientByName[name]; ok {
		return cl, nil
	}
	target := name
	if b.sidecarPort > 0 {
		target = fmt.Sprintf("%s:%d", name, b.sidecarPort)
	}
	cl, err := browser.Dial(target)
	if err != nil {
		return nil, err
	}
	b.clientByName[name] = cl
	return cl, nil
}
```

Add imports to `backend.go`: `"context"` (already present), `"time"` (already present), and `"github.com/aalejandrofer/grubdrops/internal/dockerctl"`.

- [ ] **Step 3: Backend — StartWatch starts the container then watches**

Replace the browser-watch block in `StartWatch` (the `if cl := b.watchClientFor(...)` block from `7fe74ac`) with:

```go
	if b.browserWatch && b.c != nil {
		name := b.sidecars.nameFor(s.AccountID)
		if name == "" {
			return platform.WatchHandle{}, fmt.Errorf("kick start watch: no sidecar for account %s", s.AccountID)
		}
		cl, err := b.watchClientForName(name)
		if err != nil {
			return platform.WatchHandle{}, fmt.Errorf("kick start watch: dial sidecar %s: %w", name, err)
		}
		// Start the container on demand + wait for readiness.
		if err := b.sidecars.ensureUp(ctx, s.AccountID, func(c context.Context) error {
			_, e := cl.Heartbeat(c, "") // nil error == gRPC server reachable
			return e
		}); err != nil {
			return platform.WatchHandle{}, fmt.Errorf("kick start watch: sidecar up %s: %w", name, err)
		}
		ks, err := decodeSession(s)
		if err != nil {
			return platform.WatchHandle{}, fmt.Errorf("kick start watch: decode session: %w", err)
		}
		handle, err := cl.StartWatch(ctx, toProto(ks), stream.Channel)
		if err != nil {
			return platform.WatchHandle{}, fmt.Errorf("kick start watch (browser) %s: %w", stream.Channel, err)
		}
		b.sidecars.touch(s.AccountID)
		return platform.WatchHandle{Channel: stream.Channel, Internal: kickBrowserWatch{handle: handle, client: cl}}, nil
	}
```

Add a small helper to `sidecars.go`:

```go
func (r *sidecarRegistry) nameFor(accountID string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if s, ok := r.byAcc[accountID]; ok {
		return s.containerName
	}
	return ""
}
```

- [ ] **Step 4: Backend — Heartbeat bumps lastActive**

In `Heartbeat`, the `case kickBrowserWatch:` already calls `w.client.Heartbeat`. After a successful alive check, bump activity. Change that case to capture the account — but `WatchHandle` carries `Channel`, not account. Simplest: bump on StartWatch (done) and on each Heartbeat via the handle's client is not enough to know the account. Instead, store accountID on the handle.

Change `kickBrowserWatch` in `backend.go`:
```go
type kickBrowserWatch struct {
	handle    string
	client    *browser.Client
	accountID string
}
```
Set `accountID: s.AccountID` in the `StartWatch` return (Step 3) and in the `Heartbeat` case add `b.sidecars.touch(w.accountID)` before returning nil:

```go
	case kickBrowserWatch:
		alive, err := w.client.Heartbeat(ctx, w.handle)
		if err != nil {
			return fmt.Errorf("kick heartbeat (browser) %q: %w", h.Channel, err)
		}
		if !alive {
			return fmt.Errorf("kick: browser watch not playing for %q", h.Channel)
		}
		b.sidecars.touch(w.accountID)
		return nil
```

(Update the `StartWatch` return in Step 3 to include `accountID: s.AccountID`.)

- [ ] **Step 5: Update existing backend tests for the new `New` signature**

In `internal/platform/kick/backend_test.go`, every `New(<client>)` call becomes `New(<client>, nil, "grubdrops-browser-{slug}", 9090, 10*time.Minute)`. Run to find them:

Run: `cd "$(git rev-parse --show-toplevel)" && grep -rn "kick.New(\|New(" internal/platform/kick/*_test.go`

Update each call site accordingly (controller `nil` = degrade, so tests don't touch Docker). For any test that exercised the browser-watch path via the old pool, also call `b.RegisterSidecar("acc1", "Acc1")` before `StartWatch` so `nameFor` returns non-empty.

- [ ] **Step 6: main.go — build controller, pass to New, register usernames**

In `cmd/miner/main.go`, replace the dial loop + `kick.New(...)` from `7fe74ac` with:

```go
	var browserClient *browser.Client
	if len(cfg.BrowserURLs) > 0 {
		bc, err := browser.Dial(cfg.BrowserURLs[0])
		if err != nil {
			return fmt.Errorf("dial browser sidecar %q: %w", cfg.BrowserURLs[0], err)
		}
		defer bc.Close()
		browserClient = bc // login / Twitch / display client
	}
	var dockerCtl dockerctl.Controller
	if cfg.KickBrowserWatch {
		if dc, err := dockerctl.New(); err != nil {
			logger.Warn("docker control unavailable; kick sidecars stay always-on", "err", err)
		} else {
			dockerCtl = dc
			logger.Info("docker control enabled for on-demand kick sidecars")
		}
	}
	kickBackend = kick.New(browserClient, dockerCtl, cfg.KickSidecarTemplate, cfg.KickSidecarPort, 10*time.Minute)
```

Add import `"github.com/aalejandrofer/grubdrops/internal/dockerctl"`.

In the per-account build loop (near the `if a.Platform == "kick" && kickBackend != nil` block at ~main.go:291), add:

```go
		if a.Platform == "kick" && kickBackend != nil {
			kickBackend.RegisterSidecar(a.ID, a.DisplayName)
		}
```

Also add `kickBackend.RegisterSidecar(id, acc.DisplayName)` in the Kick login handler success path so a freshly-added account is mapped without a restart. Find it:

Run: `grep -n "kick session persisted\|RegisterChannels" internal/api/handlers_login_kick.go`

In `persistKickSession`, after channels are registered, call the registrar. The handler already has a `KickChannelRegistrar`; extend that interface with `RegisterSidecar(accountID, username string)` (the `*kick.Backend` already satisfies it after Step 2) and call it with `acc.DisplayName`. (If threading the username is awkward, registration at the next Reload also covers it — acceptable; note it.)

- [ ] **Step 7: Build + full kick/config tests**

Run: `cd "$(git rev-parse --show-toplevel)" && go build ./... && go test ./internal/platform/kick/ ./internal/config/ ./internal/dockerctl/ -v 2>&1 | tail -20`
Expected: build OK; tests PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/platform/kick/ internal/config/config.go cmd/miner/main.go
git commit -m "feat(kick): on-demand sidecar start/stop wired into backend + main"
```

---

## Task 5: deploy (rename sidecars, mount socket, env)

**Files:**
- Modify: `deploy/docker-compose.yml`
- Modify: host `~/projects/homelab/humblewhale/grubdrops/compose.yml`
- Modify: host `.env`

- [ ] **Step 1: Update the in-repo compose for parity**

Edit `deploy/docker-compose.yml`: rename the two browser services to `grubdrops-browser-ttik3r` and `grubdrops-browser-phluses` (set matching `container_name`), update `grubdrops.depends_on`, and add the socket mount to `grubdrops`:
```yaml
    volumes:
      - /home/jandro/localConfig/grubdrops/data:/data
      - /var/run/docker.sock:/var/run/docker.sock
```

- [ ] **Step 2: Build the image (host, COPY-only)**

Run (from repo root):
```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o /tmp/gd-build/grubdrops ./cmd/miner
# helpers as before (see prior deploy), then scp /tmp/gd-build/* to host and:
ssh -i /Users/jandro/.ssh/jandro -o IdentityAgent=none 10.10.2.40 'cd /tmp/gd-build && docker build -t grubdrops:latest .'
```
Expected: image built.

- [ ] **Step 3: Update host compose.yml + .env**

On the host, rewrite `compose.yml` so the two sidecars are named `grubdrops-browser-ttik3r` / `grubdrops-browser-phluses` (matching `container_name`), `grubdrops.depends_on` lists both, and `grubdrops.volumes` includes `- /var/run/docker.sock:/var/run/docker.sock`. In `.env`, remove `GRUB_BROWSER_URLS` (or leave; ignored), keep `GRUB_KICK_BROWSER_WATCH=1`. Validate:

Run: `ssh ... 'cd ~/projects/homelab/humblewhale/grubdrops && docker compose config >/dev/null && echo VALID'`
Expected: `VALID`.

- [ ] **Step 4: Deploy**

Run: `ssh ... 'cd ~/projects/homelab/humblewhale/grubdrops && docker compose up -d --force-recreate'`
Expected: `grubdrops`, `grubdrops-browser-ttik3r`, `grubdrops-browser-phluses` all up.

- [ ] **Step 5: Verify mining + on-demand stop**

Run:
```bash
ssh ... 'docker logs grubdrops --since 3m 2>&1 | grep -iE "docker control enabled|sidecar starting on demand|sidecar ready|watcher progress" | tail -20'
```
Expected: `docker control enabled`; both accounts show advancing `watcher progress`.

To verify the stop path (manual, when a campaign goes idle OR by temporarily pointing an account at a dead category): after 10 min idle, `docker ps` shows that account's sidecar exited; `docker logs grubdrops | grep "sidecar idle, stopping"` present; RAM drops.

- [ ] **Step 6: Commit deploy config**

```bash
git add deploy/docker-compose.yml
git commit -m "chore(deploy): rename sidecars per-account + mount docker socket"
```

---

## Self-Review Notes

- Spec coverage: config/derivation (Task 2,4-step1), dockerctl (Task 1), registry+reaper+ensureUp (Task 3), backend wiring + readiness probe + lastActive on StartWatch/Heartbeat (Task 4), graceful degrade (nil controller throughout; main step 6), rename + socket mount + deploy (Task 5). Testing section covered by Tasks 1-4 unit tests + Task 5 manual live check.
- Type consistency: `Controller` (Start/Stop/Running), `sidecarRegistry` methods (`register`, `touch`/`touchAt`, `ensureUp`, `nameFor`, `reapOnce`, `runReaper`), `kickBrowserWatch{handle,client,accountID}`, `New(c, ctl, template, port, idleGrace)` used consistently across tasks.
- Known follow-up (out of scope, in memory backlog): VerifyAuth false-green; 403 churn backoff cap. Not part of this plan.
