package forcewatch

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/store"
)

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (f *fakeClock) now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.t
}

func (f *fakeClock) advance(d time.Duration) {
	f.mu.Lock()
	f.t = f.t.Add(d)
	f.mu.Unlock()
}

func newTest(lookup LookupFunc, ttl time.Duration) (*Checker, *fakeClock) {
	c := New(lookup, ttl)
	clk := &fakeClock{t: time.Unix(1000, 0)}
	c.now = clk.now
	return c, clk
}

func TestChecker_CachesWithinTTL(t *testing.T) {
	var n int32
	c, clk := newTest(func(ctx context.Context, p, l string) (platform.Stream, bool, error) {
		atomic.AddInt32(&n, 1)
		return platform.Stream{Channel: l}, true, nil
	}, time.Minute)
	for i := 0; i < 2; i++ {
		if _, live, err := c.Lookup(context.Background(), "twitch", "a"); err != nil || !live {
			t.Fatalf("lookup: live=%v err=%v", live, err)
		}
	}
	if n != 1 {
		t.Fatalf("calls=%d want 1", n)
	}
	clk.advance(2 * time.Minute)
	if _, _, err := c.Lookup(context.Background(), "twitch", "a"); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("calls=%d want 2", n)
	}
}

func TestChecker_SharesInflight(t *testing.T) {
	var n int32
	release := make(chan struct{})
	c, _ := newTest(func(ctx context.Context, p, l string) (platform.Stream, bool, error) {
		atomic.AddInt32(&n, 1)
		<-release
		return platform.Stream{}, true, nil
	}, time.Minute)
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.Lookup(context.Background(), "kick", "x")
		}()
	}
	time.Sleep(20 * time.Millisecond)
	close(release)
	wg.Wait()
	if n != 1 {
		t.Fatalf("calls=%d want 1", n)
	}
}

func TestChecker_ErrorLeavesUnknown(t *testing.T) {
	var fail atomic.Bool
	boom := errors.New("boom")
	c, clk := newTest(func(ctx context.Context, p, l string) (platform.Stream, bool, error) {
		if fail.Load() {
			return platform.Stream{}, false, boom
		}
		return platform.Stream{}, true, nil
	}, time.Minute)
	if _, _, err := c.Lookup(context.Background(), "twitch", "a"); err != nil {
		t.Fatal(err)
	}
	clk.advance(2 * time.Minute)
	fail.Store(true)
	if _, _, err := c.Lookup(context.Background(), "twitch", "a"); !errors.Is(err, boom) {
		t.Fatalf("err=%v", err)
	}
	if st := c.Peek("twitch", "a"); st.Live != nil {
		t.Fatalf("Live=%v want nil", *st.Live)
	}
	if st := c.Peek("twitch", "never"); st.Live != nil {
		t.Fatal("unknown key should be nil")
	}
}

func TestChecker_PeekAfterLookup(t *testing.T) {
	c, _ := newTest(func(ctx context.Context, p, l string) (platform.Stream, bool, error) {
		return platform.Stream{Channel: l, ViewerCount: 5}, true, nil
	}, time.Minute)
	c.Lookup(context.Background(), "twitch", "a")
	st := c.Peek("twitch", "a")
	if st.Live == nil || !*st.Live || st.Stream.ViewerCount != 5 || st.CheckedAt.IsZero() {
		t.Fatalf("bad status %+v", st)
	}
}

func TestChecker_RunRefreshesAll(t *testing.T) {
	var mu sync.Mutex
	seen := map[string]int{}
	c := New(func(ctx context.Context, p, l string) (platform.Stream, bool, error) {
		mu.Lock()
		seen[p+"/"+l]++
		mu.Unlock()
		return platform.Stream{}, false, nil
	}, time.Nanosecond)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		c.Run(ctx, func() []store.ForceChannel {
			return []store.ForceChannel{{Platform: "twitch", Channel: "a"}, {Platform: "kick", Channel: "b"}}
		}, 5*time.Millisecond)
		close(done)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		ok := seen["twitch/a"] > 0 && seen["kick/b"] > 0
		mu.Unlock()
		if ok {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not stop")
	}
	mu.Lock()
	defer mu.Unlock()
	if seen["twitch/a"] == 0 || seen["kick/b"] == 0 {
		t.Fatalf("seen=%v", seen)
	}
}
