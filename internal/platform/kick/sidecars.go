package kick

import (
	"context"
	"log/slog"
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
	lastActive    time.Time
}

type sidecarRegistry struct {
	ctl       dockerctl.Controller // nil = degrade (never start/stop)
	template  string
	port      int
	idleGrace time.Duration

	mu    sync.Mutex
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

func (r *sidecarRegistry) touch(accountID string)              { r.touchAt(accountID, time.Now()) }
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
