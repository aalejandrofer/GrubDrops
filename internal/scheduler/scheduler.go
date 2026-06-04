package scheduler

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/chano-fernandez/rust-drops-miner/internal/notify"
	"github.com/chano-fernandez/rust-drops-miner/internal/watcher"
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
	opts    Options
	mu      sync.Mutex
	entries []entry
	wg      sync.WaitGroup
}

func New(opts Options) *Scheduler {
	return &Scheduler{opts: opts}
}

func (s *Scheduler) Add(id string, w *watcher.Watcher) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = append(s.entries, entry{id: id, runner: w})
}

func (s *Scheduler) Start(ctx context.Context) error {
	s.mu.Lock()
	entries := append([]entry(nil), s.entries...)
	s.mu.Unlock()

	for _, e := range entries {
		s.wg.Add(1)
		go s.supervise(ctx, e)
	}
	return nil
}

func (s *Scheduler) Wait() { s.wg.Wait() }

func (s *Scheduler) supervise(ctx context.Context, e entry) {
	defer s.wg.Done()
	defer func() {
		if r := recover(); r != nil {
			if s.opts.Notifier != nil {
				_ = s.opts.Notifier.Notify(ctx, notify.EventError, map[string]any{
					"account": e.id,
					"panic":   fmt.Sprint(r),
				})
			}
		}
	}()
	if err := e.runner.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		if s.opts.Notifier != nil {
			_ = s.opts.Notifier.Notify(ctx, notify.EventError, map[string]any{
				"account": e.id,
				"error":   err.Error(),
			})
		}
	}
}
