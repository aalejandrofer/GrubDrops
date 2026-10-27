package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
)

// SSE event names.
const (
	EventRunner   = "runner"
	EventClaim    = "claim"
	EventGames    = "games"
	EventAccounts = "accounts"
	EventLog      = "log"

	EventForceWatch = "force_watch"
)

const subBuffer = 64

// Test overrides. Production: ping every 25 s, flush coalesced events every 1 s.
var (
	pingEvery   = 25 * time.Second
	flushWindow = time.Second
)

// Event is one hub message. A non-empty Key coalesces events of the same Name
// and Key to the latest one per flush window.
type Event struct {
	Name string
	Key  string
	Data any
}

type coalesceKey struct{ name, key string }

// Hub fans events out to subscribers. Publish never blocks: a subscriber whose
// buffer is full is dropped and its channel closed.
type Hub struct {
	mu      sync.Mutex
	subs    map[chan Event]struct{}
	pending map[coalesceKey]Event
	order   []coalesceKey
}

func NewHub() *Hub {
	return &Hub{subs: map[chan Event]struct{}{}, pending: map[coalesceKey]Event{}}
}

// Publish delivers e to every subscriber, or queues it for the next flush when
// it has a Key.
func (h *Hub) Publish(e Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if e.Key == "" {
		h.sendLocked(e)
		return
	}
	k := coalesceKey{e.Name, e.Key}
	if _, ok := h.pending[k]; !ok {
		h.order = append(h.order, k)
	}
	h.pending[k] = e
}

func (h *Hub) sendLocked(e Event) {
	for ch := range h.subs {
		select {
		case ch <- e:
		default:
			delete(h.subs, ch)
			close(ch)
		}
	}
}

func (h *Hub) flush() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, k := range h.order {
		h.sendLocked(h.pending[k])
	}
	h.order = h.order[:0]
	clear(h.pending)
}

// Subscribe returns a channel of events and an idempotent cancel func. The
// channel is closed when the subscriber is dropped or cancelled.
func (h *Hub) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, subBuffer)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if _, ok := h.subs[ch]; ok {
			delete(h.subs, ch)
			close(ch)
		}
	}
}

func (h *Hub) subscribers() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}

// Run flushes coalesced events every flush window until ctx ends.
func (h *Hub) Run(ctx context.Context) {
	t := time.NewTicker(flushWindow)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			h.flush()
		}
	}
}

func (s *server) routesEvents(r chi.Router) {
	r.Get("/events", s.events)
}

func (s *server) events(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok || s.d.Hub == nil {
		writeErr(w, http.StatusInternalServerError, "internal")
		return
	}
	ch, cancel := s.d.Hub.Subscribe()
	defer cancel()
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprint(w, ": ok\n\n")
	fl.Flush()

	ping := time.NewTicker(pingEvery)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ping.C:
			_, _ = fmt.Fprint(w, ": ping\n\n")
		case e, ok := <-ch:
			if !ok {
				return
			}
			b, err := json.Marshal(e.Data)
			if err != nil {
				slog.Error("sse event marshal failed", "kind", "error", "event", e.Name, "err", err)
				continue
			}
			_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.Name, b)
		}
		fl.Flush()
	}
}
