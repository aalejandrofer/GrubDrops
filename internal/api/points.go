package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/aalejandrofer/grubdrops/internal/store"
)

func (s *server) routesPoints(r chi.Router) {
	r.Get("/points", s.getPointsSafety)
	r.Put("/points/safety", s.putPointsSafety)
}

type pointsSafetyJSON struct {
	SleepEnabled bool   `json:"sleep_enabled"`
	SleepStart   string `json:"sleep_start"`
	SleepEnd     string `json:"sleep_end"`
	FollowRaids  bool   `json:"follow_raids"`
}

type pointsSafetyPutJSON struct {
	SleepEnabled *bool  `json:"sleep_enabled"`
	SleepStart   string `json:"sleep_start"`
	SleepEnd     string `json:"sleep_end"`
	FollowRaids  *bool  `json:"follow_raids"`
}

func safetyJSON(v store.PointsSafety) pointsSafetyJSON {
	clock := func(minutes int) string {
		return time.Date(0, 1, 1, minutes/60, minutes%60, 0, 0, time.UTC).Format("15:04")
	}
	return pointsSafetyJSON{SleepEnabled: v.SleepEnabled, SleepStart: clock(v.SleepStart), SleepEnd: clock(v.SleepEnd), FollowRaids: v.FollowRaids}
}

func parseSafetyClock(v string) (int, bool) {
	t, err := time.Parse("15:04", v)
	if err != nil || t.Format("15:04") != v {
		return 0, false
	}
	return t.Hour()*60 + t.Minute(), true
}

func (s *server) getPointsSafety(w http.ResponseWriter, r *http.Request) {
	settings, err := store.LoadPointsSafety(r.Context(), s.d.Q)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal")
		return
	}
	writeJSON(w, http.StatusOK, safetyJSON(settings))
}

func (s *server) putPointsSafety(w http.ResponseWriter, r *http.Request) {
	var body pointsSafetyPutJSON
	if !readJSON(w, r, &body) {
		return
	}
	start, okStart := parseSafetyClock(body.SleepStart)
	end, okEnd := parseSafetyClock(body.SleepEnd)
	if body.SleepEnabled == nil || body.FollowRaids == nil || !okStart || !okEnd || start == end {
		writeErr(w, http.StatusBadRequest, "bad_request")
		return
	}
	settings := store.PointsSafety{SleepEnabled: *body.SleepEnabled, SleepStart: start, SleepEnd: end, FollowRaids: *body.FollowRaids}
	if err := store.SavePointsSafety(r.Context(), s.d.Q, settings); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal")
		return
	}
	if s.d.PointsSafetyChanged != nil {
		s.d.PointsSafetyChanged()
	}
	writeJSON(w, http.StatusOK, safetyJSON(settings))
}
