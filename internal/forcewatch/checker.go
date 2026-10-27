// Package forcewatch holds the shared live-status cache for force-watch
// channels. Runners call Lookup; the API calls Peek.
package forcewatch

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/store"
)

const lookupTimeout = 10 * time.Second

// LookupFunc reports whether login is live on the platform.
type LookupFunc func(ctx context.Context, platform, login string) (platform.Stream, bool, error)

// Status is a non-blocking cache snapshot. Live nil means unknown.
type Status struct {
	Live      *bool
	Stream    platform.Stream
	CheckedAt time.Time
}

type entry struct {
	stream  platform.Stream
	live    bool
	checked time.Time
}

// Checker caches lookups for ttl and shares in-flight calls per key.
type Checker struct {
	ttl    time.Duration
	lookup LookupFunc
	now    func() time.Time

	mu    sync.Mutex
	cache map[string]entry
	sf    singleflight.Group
}

// New builds a Checker. Production ttl is 2 minutes.
func New(lookup LookupFunc, ttl time.Duration) *Checker {
	return &Checker{ttl: ttl, lookup: lookup, now: time.Now, cache: map[string]entry{}}
}

func key(platform, login string) string { return platform + "/" + login }

// Lookup returns a cached result when fresh, else performs one shared lookup.
func (c *Checker) Lookup(ctx context.Context, platform, login string) (platform.Stream, bool, error) {
	k := key(platform, login)
	c.mu.Lock()
	e, ok := c.cache[k]
	c.mu.Unlock()
	if ok && c.now().Sub(e.checked) < c.ttl {
		return e.stream, e.live, nil
	}
	return c.refresh(ctx, platform, login)
}

func (c *Checker) refresh(ctx context.Context, plat, login string) (platform.Stream, bool, error) {
	k := key(plat, login)
	type res struct {
		s    platform.Stream
		live bool
	}
	v, err, _ := c.sf.Do(k, func() (any, error) {
		// Detach from the caller so one caller's cancel does not fail the others.
		lctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), lookupTimeout)
		defer cancel()
		s, live, err := c.lookup(lctx, plat, login)
		c.mu.Lock()
		defer c.mu.Unlock()
		if err != nil {
			delete(c.cache, k)
			return nil, err
		}
		c.cache[k] = entry{stream: s, live: live, checked: c.now()}
		return res{s, live}, nil
	})
	if err != nil {
		slog.Debug("force watch lookup failed", "platform", plat, "channel", login, "err", err)
		return platform.Stream{}, false, err
	}
	r := v.(res)
	return r.s, r.live, nil
}

// Peek never blocks and never calls the lookup. Live nil means unknown.
func (c *Checker) Peek(platform, login string) Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.cache[key(platform, login)]
	if !ok {
		return Status{}
	}
	live := e.live
	return Status{Live: &live, Stream: e.stream, CheckedAt: e.checked}
}

// Run refreshes every listed channel each `every` until ctx is done.
func (c *Checker) Run(ctx context.Context, list func() []store.ForceChannel, every time.Duration) {
	pass := func() {
		for _, ch := range list() {
			if ctx.Err() != nil {
				return
			}
			c.refresh(ctx, ch.Platform, ch.Channel) // errors logged in refresh
		}
	}
	pass()
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			pass()
		}
	}
}
