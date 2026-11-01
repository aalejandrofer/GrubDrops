package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
)

// scanCooldown is the least time between two accepted POST /api/scan calls.
const scanCooldown = time.Minute

// scanGate holds the one cooldown POST /api/scan shares across platforms.
type scanGate struct {
	mu   sync.Mutex
	last time.Time
}

// take reports whether a scan may start at now and, when it may, starts the
// cooldown. Otherwise it reports false with the time left.
func (g *scanGate) take(now time.Time) (bool, time.Duration) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.last.IsZero() {
		if left := g.last.Add(scanCooldown).Sub(now); left > 0 {
			return false, left
		}
	}
	g.last = now
	return true, 0
}

func (s *server) routesScan(r chi.Router) {
	r.Post("/scan", s.postScan)
}

// postScan asks every running account, or only those on body.platform
// (twitch or kick), to rescan drops and claims now and returns 202; the new
// state arrives over SSE. The body is optional. Inside the cooldown it
// returns 429 with retry_after_sec and a Retry-After header.
func (s *server) postScan(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Platform string `json:"platform"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil && !errors.Is(err, io.EOF) {
		writeErr(w, http.StatusBadRequest, "bad_request")
		return
	}
	switch in.Platform {
	case "", "twitch", "kick":
	default:
		writeErr(w, http.StatusBadRequest, "bad_request")
		return
	}
	if s.d.Pool == nil {
		writeErr(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	ok, wait := s.scan.take(s.overviewNow())
	if !ok {
		sec := int((wait + time.Second - 1) / time.Second)
		w.Header().Set("Retry-After", strconv.Itoa(sec))
		writeJSON(w, http.StatusTooManyRequests, map[string]any{"error": "cooldown", "retry_after_sec": sec})
		return
	}
	if in.Platform == "" {
		s.d.Pool.RescanAll()
	} else {
		s.d.Pool.RescanPlatform(in.Platform, "")
	}
	w.WriteHeader(http.StatusAccepted)
}
