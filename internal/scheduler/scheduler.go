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
	s.current = s.startInternal(ctx)
	return nil
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
