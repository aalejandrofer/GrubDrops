package scheduler

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/aalejandrofer/grubdrops/internal/notify"
	"github.com/aalejandrofer/grubdrops/internal/watcher"
)

type runner interface {
	Run(ctx context.Context) error
}

type entry struct {
	id     string
	runner runner
}

type Options struct {
	Notifier notify.Notifier
}

type Scheduler struct {
	opts Options

	mu      sync.Mutex
	entries []entry

	runMu   sync.Mutex
	current *runState

	// baseCtx is the long-lived context every run is rooted in. It is
	// captured (derived from Background) on the FIRST Start and reused for
	// the lifetime of the scheduler. Reload/ReloadAccount run watchers under
	// baseCtx — NOT under the per-call parent — so a reload triggered by a
	// short-lived context (e.g. an HTTP request context, which is cancelled
	// the moment the handler returns) cannot tear the watcher roster down.
	// The v1.0.1 prod stall was exactly this: the Kick re-login handler
	// reloaded with the request context, and finishing the request cancelled
	// every freshly-rebuilt watcher. baseCancel cancels it for a full Stop.
	baseCtx    context.Context
	baseCancel context.CancelFunc

	reloadMu sync.Mutex
}

func New(opts Options) *Scheduler { return &Scheduler{opts: opts} }

func (s *Scheduler) Add(id string, w *watcher.Watcher) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = append(s.entries, entry{id: id, runner: w})
}

func (s *Scheduler) Start(ctx context.Context) error {
	s.runMu.Lock()
	defer s.runMu.Unlock()
	if s.current != nil {
		return errors.New("scheduler already running")
	}
	s.current = s.startInternal(s.ensureBaseCtx(ctx))
	return nil
}

// ensureBaseCtx returns the long-lived base context, deriving it from the
// FIRST start's parent and reusing it forever after. Runs hold the runMu, so
// this is single-flighted. The first parent is the process root context (with
// signal-driven cancellation) on boot, so shutdown semantics are preserved;
// every later reload's parent is ignored for run lifetime, so a request
// context can't propagate its cancellation into the watcher roster.
func (s *Scheduler) ensureBaseCtx(parent context.Context) context.Context {
	if s.baseCtx == nil {
		s.baseCtx, s.baseCancel = context.WithCancel(parent)
	}
	return s.baseCtx
}

func (s *Scheduler) Wait() {
	s.runMu.Lock()
	r := s.current
	s.runMu.Unlock()
	if r == nil {
		return
	}
	r.wg.Wait()
}

func (s *Scheduler) supervise(ctx context.Context, e entry) {
	defer func() {
		if r := recover(); r != nil {
			if s.opts.Notifier != nil {
				_ = s.opts.Notifier.Notify(ctx, notify.EventError, map[string]any{
					"account": e.id, "panic": fmt.Sprint(r),
				})
			}
		}
	}()
	if err := e.runner.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		if s.opts.Notifier != nil {
			_ = s.opts.Notifier.Notify(ctx, notify.EventError, map[string]any{
				"account": e.id, "error": err.Error(),
			})
		}
	}
}
