package api

import (
	"bufio"
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func recv(t *testing.T, ch <-chan Event) Event {
	t.Helper()
	select {
	case e, ok := <-ch:
		require.True(t, ok, "channel closed")
		return e
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for event")
	}
	return Event{}
}

func TestHub_FanOut(t *testing.T) {
	h := NewHub()
	a, ca := h.Subscribe()
	b, cb := h.Subscribe()
	defer ca()
	defer cb()
	h.Publish(Event{Name: EventGames, Data: map[string]any{}})
	assert.Equal(t, EventGames, recv(t, a).Name)
	assert.Equal(t, EventGames, recv(t, b).Name)
}

func TestHub_RunnerCoalesced(t *testing.T) {
	old := flushWindow
	flushWindow = 50 * time.Millisecond
	defer func() { flushWindow = old }()
	h := NewHub()
	ch, cancel := h.Subscribe()
	defer cancel()
	ctx, stop := contextWithCancel(t)
	defer stop()
	go h.Run(ctx)
	for i := 1; i <= 5; i++ {
		h.Publish(Event{Name: EventRunner, Key: "a1", Data: i})
	}
	h.Publish(Event{Name: EventRunner, Key: "a2", Data: "x"})
	got := map[string]any{}
	for i := 0; i < 2; i++ {
		e := recv(t, ch)
		got[e.Key] = e.Data
	}
	assert.Equal(t, map[string]any{"a1": 5, "a2": "x"}, got)
	select {
	case e := <-ch:
		t.Fatalf("unexpected extra event %+v", e)
	case <-time.After(150 * time.Millisecond):
	}
}

func TestHub_SlowSubscriberDropped(t *testing.T) {
	h := NewHub()
	ch, cancel := h.Subscribe()
	defer cancel()
	for i := 0; i < 65; i++ {
		start := time.Now()
		h.Publish(Event{Name: EventLog, Data: i})
		assert.Less(t, time.Since(start), 10*time.Millisecond)
	}
	assert.Equal(t, 0, h.subscribers())
	n := 0
	for range ch {
		n++
	}
	assert.Equal(t, 64, n)
}

func TestHub_PublishWithoutRun(t *testing.T) {
	h := NewHub()
	for i := 0; i < 1000; i++ {
		h.Publish(Event{Name: EventRunner, Key: "a1", Data: i})
	}
}

func TestEvents_StreamFormat(t *testing.T) {
	old := pingEvery
	pingEvery = 20 * time.Millisecond
	defer func() { pingEvery = old }()
	e := newEnv(t)
	e.setup("password123")
	req, _ := http.NewRequest("GET", e.srv.URL+"/api/events", nil)
	ctx, cancel := contextWithCancel(t)
	defer cancel()
	resp, err := e.cl.Do(req.WithContext(ctx))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))
	assert.Equal(t, "no-cache", resp.Header.Get("Cache-Control"))
	assert.Equal(t, "no", resp.Header.Get("X-Accel-Buffering"))
	require.Eventually(t, func() bool { return e.s.d.Hub.subscribers() == 1 }, time.Second, 5*time.Millisecond)
	e.s.d.Hub.Publish(Event{Name: EventClaim, Data: map[string]string{"id": "c1"}})

	lines := make(chan string, 64)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()
	var seen []string
	deadline := time.After(2 * time.Second)
	for !(contains(seen, "event: claim") && contains(seen, `data: {"id":"c1"}`) && contains(seen, ": ping")) {
		select {
		case l, ok := <-lines:
			require.True(t, ok, "stream ended; saw %v", seen)
			seen = append(seen, l)
		case <-deadline:
			t.Fatalf("timeout; saw %v", seen)
		}
	}
	cancel()
	require.Eventually(t, func() bool { return e.s.d.Hub.subscribers() == 0 }, 2*time.Second, 5*time.Millisecond)
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if strings.TrimSpace(x) == s {
			return true
		}
	}
	return false
}

func TestEvents_Unauthed401(t *testing.T) {
	e := newEnv(t)
	resp, _ := e.do("GET", "/api/events", "", nil)
	assert.Equal(t, 401, resp.StatusCode)
}

func contextWithCancel(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithCancel(context.Background())
}
