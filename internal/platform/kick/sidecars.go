package kick

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/dockerctl"
)

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

type sidecar struct {
	containerName string // "" = no controllable container (empty slug)
	slug          string // username slug (for the create label)
	lastActive    time.Time
}

type sidecarRegistry struct {
	ctl       dockerctl.Controller // nil = degrade (never start/stop)
	template  string
	port      int
	idleGrace time.Duration

	// image is the browser image auto-created sidecars are built from.
	image string
	// netOverride forces the sidecar network; "" means self-detect once.
	netOverride string
	// proxyURL, when set, is passed to auto-created sidecars via
	// GRUB_SIDECAR_PROXY so their Chrome egresses through the same proxy as
	// the rest of the Kick backend's traffic.
	proxyURL string

	// netOnce guards lazy network self-detection (inspect the miner's own
	// container) so we only inspect once and cache the result.
	netOnce sync.Once
	nets    []string

	mu    sync.Mutex
	byAcc map[string]*sidecar
}

func newSidecarRegistry(ctl dockerctl.Controller, template string, port int, idleGrace time.Duration) *sidecarRegistry {
	return &sidecarRegistry{ctl: ctl, template: template, port: port, idleGrace: idleGrace, byAcc: map[string]*sidecar{}}
}

// withCreate enables auto-create: the image to pull/create from and an optional
// network override. Returns r for chaining at construction.
func (r *sidecarRegistry) withCreate(image, netOverride string) *sidecarRegistry {
	r.image = image
	r.netOverride = netOverride
	return r
}

// withProxy sets the proxy URL (may be "") passed to auto-created sidecars via
// GRUB_SIDECAR_PROXY. Returns r for chaining at construction.
func (r *sidecarRegistry) withProxy(proxyURL string) *sidecarRegistry {
	r.proxyURL = proxyURL
	return r
}

// sidecarEnv builds the env for an auto-created sidecar. When a proxy is
// configured, GRUB_SIDECAR_PROXY tells the sidecar's Chrome to route
// through it (see internal/auth/browser/sidecar).
func sidecarEnv(proxyURL string) []string {
	if proxyURL == "" {
		return nil
	}
	return []string{"GRUB_SIDECAR_PROXY=" + proxyURL}
}

// register derives the account's sidecar from its username. Safe to call again
// (e.g. on Reload) — updates the name, preserves lastActive.
func (r *sidecarRegistry) register(accountID, username string) {
	name := sidecarName(r.template, username)
	slug := slugify(username)
	r.mu.Lock()
	defer r.mu.Unlock()
	if s, ok := r.byAcc[accountID]; ok {
		s.containerName = name
		s.slug = slug
		return
	}
	r.byAcc[accountID] = &sidecar{containerName: name, slug: slug}
}

// runningNames returns the addresses of registered sidecars whose container is
// actually running, sorted. Unlike names() (which lists every registered
// account), this reflects runtime so the Status page shows a sidecar only when
// it's up — on-demand containers that are idle/never-started don't appear. With
// no docker controller, nothing is considered running.
func (r *sidecarRegistry) runningNames(ctx context.Context) []string {
	if r.ctl == nil {
		return nil
	}
	type ent struct{ name, addr string }
	r.mu.Lock()
	ents := make([]ent, 0, len(r.byAcc))
	for _, s := range r.byAcc {
		if s.containerName == "" {
			continue
		}
		ents = append(ents, ent{s.containerName, fmt.Sprintf("%s:%d", s.containerName, r.port)})
	}
	r.mu.Unlock()
	out := make([]string, 0, len(ents))
	for _, e := range ents {
		running, err := r.ctl.Running(ctx, e.name)
		if err != nil || !running {
			continue
		}
		out = append(out, e.addr)
	}
	sort.Strings(out)
	return out
}

// networks resolves the network(s) to attach auto-created sidecars to. Uses the
// override when set, else self-detects (cached). Empty result means attach to
// none (the default bridge) — fine when the miner runs on the default network.
func (r *sidecarRegistry) networks(ctx context.Context) []string {
	if r.netOverride != "" {
		return []string{r.netOverride}
	}
	r.netOnce.Do(func() {
		if r.ctl == nil {
			return
		}
		nets, err := r.ctl.SelfNetworks(ctx)
		if err != nil {
			slog.Warn("kick sidecar network self-detect failed; sidecars use default network", "err", err)
			return
		}
		r.nets = nets
	})
	return r.nets
}

// nameFor returns the derived container name for an account, or "" if the
// account is unregistered or has no controllable sidecar (empty slug).
func (r *sidecarRegistry) nameFor(accountID string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if s, ok := r.byAcc[accountID]; ok {
		return s.containerName
	}
	return ""
}

// names returns each registered account's sidecar address ("container:port"),
// sorted, for read-only display. Skips accounts with no controllable sidecar.
func (r *sidecarRegistry) names() []string {
	r.mu.Lock()
	out := make([]string, 0, len(r.byAcc))
	for _, s := range r.byAcc {
		if s.containerName == "" {
			continue
		}
		out = append(out, fmt.Sprintf("%s:%d", s.containerName, r.port))
	}
	r.mu.Unlock()
	sort.Strings(out)
	return out
}

func (r *sidecarRegistry) touch(accountID string) { r.touchAt(accountID, time.Now()) }
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
	slug := ""
	if s != nil {
		slug = s.slug
	}
	r.mu.Unlock()
	if ctl == nil || s == nil || s.containerName == "" {
		return nil
	}
	// Auto-create path: if the container does not exist at all, pull + create
	// it (labelled, on the miner's network) before starting. A pre-existing
	// container (hand-defined or already auto-created) is just started — we
	// never recreate it. ContainerInspect via Running reports not-found as an
	// error, so probe existence explicitly first.
	exists, err := ctl.Exists(ctx, s.containerName)
	if err != nil {
		slog.Warn("kick sidecar existence probe failed; assuming up", "container", s.containerName, "err", err)
		r.touch(accountID)
		return nil
	}
	if !exists {
		if r.image == "" {
			slog.Warn("kick sidecar missing and auto-create disabled (no image); needs a hand-defined container", "container", s.containerName, "account", accountID)
			r.touch(accountID)
			return nil
		}
		slog.Info("kick sidecar absent, auto-creating", "container", s.containerName, "account", accountID, "image", r.image)
		if err := ctl.EnsureImage(ctx, r.image); err != nil {
			return err
		}
		if err := ctl.Create(ctx, dockerctl.CreateSpec{
			Name:     s.containerName,
			Image:    r.image,
			Port:     r.port,
			Networks: r.networks(ctx),
			Labels: map[string]string{
				dockerctl.LabelManaged: "true",
				dockerctl.LabelAccount: accountID,
				dockerctl.LabelSlug:    slug,
			},
			Env: sidecarEnv(r.proxyURL),
		}); err != nil {
			return err
		}
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

// activeAccounts snapshots the account ids currently registered (i.e. the live
// Kick roster). The orphan sweep removes managed containers whose account is
// NOT in this set.
func (r *sidecarRegistry) activeAccounts() map[string]struct{} {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[string]struct{}, len(r.byAcc))
	for acc := range r.byAcc {
		out[acc] = struct{}{}
	}
	return out
}

// sweepOrphans removes every auto-created sidecar (carrying grubdrops.managed=
// true) whose grubdrops.account label is NOT in the live account set.
//
// SAFETY: it lists ONLY containers matching grubdrops.managed=true, so an
// unlabeled hand-defined sidecar (the current prod containers) is never even
// returned, let alone removed. A managed container whose account is still
// active is spared. A managed container with no account label is treated as an
// orphan (it cannot map to any live account) and removed.
func (r *sidecarRegistry) sweepOrphans(ctx context.Context) {
	if r.ctl == nil {
		return
	}
	managed, err := r.ctl.List(ctx, map[string]string{dockerctl.LabelManaged: "true"})
	if err != nil {
		slog.Warn("kick sidecar orphan sweep: list failed", "err", err)
		return
	}
	active := r.activeAccounts()
	for _, c := range managed {
		// Defensive re-check: never act on a container that does not carry
		// the managed label (the filter should already guarantee this).
		if c.Labels[dockerctl.LabelManaged] != "true" {
			continue
		}
		acc := c.Labels[dockerctl.LabelAccount]
		if acc != "" {
			if _, ok := active[acc]; ok {
				continue // account still live — keep
			}
		}
		slog.Info("kick sidecar orphan, removing", "container", c.Name, "account", acc)
		if err := r.ctl.Remove(ctx, c.Name); err != nil {
			slog.Warn("kick sidecar orphan remove failed", "container", c.Name, "err", err)
		}
	}
}

// runReaper ticks reapOnce + sweepOrphans every minute until ctx is done. The
// startup sweep is NOT run here (the account roster isn't registered yet when
// New spins this up) — main triggers it via Backend.SweepSidecars after the
// initial Reload, when activeAccounts is populated.
func (r *sidecarRegistry) runReaper(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			r.reapOnce(ctx)
			r.sweepOrphans(ctx)
		}
	}
}
