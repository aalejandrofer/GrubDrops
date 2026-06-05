package log

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strconv"
	"sync"
	"time"
)

// Well-known event kinds for the dashboard live-events drawer.
// Watcher / notifier code pushes these via Ring.PushEvent so the UI
// can color-code and filter without resorting to substring matching
// on free-form log messages.
const (
	KindClaim     = "claim"
	KindProgress  = "progress"
	KindState     = "state"
	KindDiscovery = "discovery"
	KindError     = "error"
	KindAuth      = "auth"
	KindInfo      = "info"
)

type LogLine struct {
	TS     time.Time      `json:"ts"`
	Level  string         `json:"level"`
	Kind   string         `json:"kind,omitempty"`
	Msg    string         `json:"msg"`
	Fields map[string]any `json:"fields,omitempty"`
}

type Ring struct {
	mu    sync.Mutex
	buf   []LogLine
	size  int
	next  int
	count int
}

func NewRing(size int) *Ring {
	if size <= 0 {
		size = 1
	}
	return &Ring{buf: make([]LogLine, size), size: size}
}

// NewRingFromEnv constructs a Ring sized from MINER_LOG_RING (default
// `def` when unset / unparseable / non-positive). Centralised so the
// main entrypoint doesn't have to repeat the env parsing.
func NewRingFromEnv(def int) *Ring {
	size := def
	if v := os.Getenv("MINER_LOG_RING"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			size = n
		}
	}
	return NewRing(size)
}

func (r *Ring) Push(l LogLine) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buf[r.next] = l
	r.next = (r.next + 1) % r.size
	if r.count < r.size {
		r.count++
	}
}

// PushEvent records a typed event for the dashboard live-events
// drawer. Safe to call with a nil receiver — callers don't have to
// guard each emission. `level` follows slog conventions
// ("INFO"/"WARN"/"ERROR"/"DEBUG"). When empty, defaults to "INFO" for
// non-error kinds and "ERROR" for KindError.
func (r *Ring) PushEvent(kind, level, msg string, fields map[string]any) {
	if r == nil {
		return
	}
	if level == "" {
		if kind == KindError {
			level = "ERROR"
		} else {
			level = "INFO"
		}
	}
	// Copy fields so later mutation by the caller can't corrupt the ring.
	var fcopy map[string]any
	if len(fields) > 0 {
		fcopy = make(map[string]any, len(fields))
		for k, v := range fields {
			fcopy[k] = v
		}
	}
	r.Push(LogLine{
		TS:     time.Now(),
		Level:  level,
		Kind:   kind,
		Msg:    msg,
		Fields: fcopy,
	})
}

func (r *Ring) Snapshot() []LogLine {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]LogLine, 0, r.count)
	start := (r.next - r.count + r.size) % r.size
	for i := 0; i < r.count; i++ {
		out = append(out, r.buf[(start+i)%r.size])
	}
	return out
}

func New(w io.Writer, level string) *slog.Logger {
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: parseLevel(level)}))
}

func NewWithRing(w io.Writer, level string, r *Ring) *slog.Logger {
	base := slog.NewJSONHandler(w, &slog.HandlerOptions{Level: parseLevel(level)})
	return slog.New(&ringHandler{inner: base, ring: r})
}

func parseLevel(s string) slog.Level {
	switch s {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

type ringHandler struct {
	inner slog.Handler
	ring  *Ring
}

func (h *ringHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.inner.Enabled(ctx, l)
}

func (h *ringHandler) Handle(ctx context.Context, rec slog.Record) error {
	fields := map[string]any{}
	rec.Attrs(func(a slog.Attr) bool {
		fields[a.Key] = a.Value.Any()
		return true
	})
	var kind string
	if v, ok := fields["kind"]; ok {
		if s, ok := v.(string); ok {
			kind = s
		}
	}
	h.ring.Push(LogLine{
		TS:     rec.Time,
		Level:  rec.Level.String(),
		Kind:   kind,
		Msg:    rec.Message,
		Fields: fields,
	})
	return h.inner.Handle(ctx, rec)
}

func (h *ringHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &ringHandler{inner: h.inner.WithAttrs(attrs), ring: h.ring}
}

func (h *ringHandler) WithGroup(name string) slog.Handler {
	return &ringHandler{inner: h.inner.WithGroup(name), ring: h.ring}
}
