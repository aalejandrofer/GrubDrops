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
