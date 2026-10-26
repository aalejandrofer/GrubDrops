package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/aalejandrofer/grubdrops/internal/canary"
	mlog "github.com/aalejandrofer/grubdrops/internal/log"
)

// maxHealthEvents caps the events returned after the filter.
const maxHealthEvents = 50

// canaryRunTimeout bounds one detached canary run: two Twitch beacons at 5 s
// plus the Kick window (45 s) with headroom.
const canaryRunTimeout = 90 * time.Second

func (s *server) routesHealth(r chi.Router) {
	r.Get("/health", s.getHealth)
	r.Get("/health/log", s.getHealthLog)
	r.Post("/health/canary", s.postHealthCanary)
	r.Get("/update", s.getUpdate)
	r.Post("/update/check", s.postUpdateCheck)
	r.Get("/img/kick", s.getKickImage)
}

type healthEvent struct {
	At          time.Time `json:"at"`
	Level       string    `json:"level"`
	Kind        string    `json:"kind"`
	AccountID   string    `json:"account_id"`
	AccountName string    `json:"account_name"`
	Msg         string    `json:"msg"`
}

type canaryOut struct {
	Platform  string    `json:"platform"`
	OK        bool      `json:"ok"`
	Detail    string    `json:"detail"`
	CheckedAt time.Time `json:"checked_at"`
}

func isProblem(l mlog.LogLine) bool {
	return l.Level == "WARN" || l.Level == "ERROR" || l.Kind == mlog.KindError || l.Kind == mlog.KindAuth
}

func isClaim(l mlog.LogLine) bool { return l.Kind == mlog.KindClaim }

func (s *server) getHealth(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	filter := r.URL.Query().Get("filter")
	if filter == "" {
		filter = "all"
	}
	if filter != "all" && filter != "problems" && filter != "claims" {
		writeErr(w, http.StatusBadRequest, "bad_request")
		return
	}

	names := map[string]string{}
	if accts, err := s.d.Q.ListAllAccounts(ctx); err == nil {
		for _, a := range accts {
			names[a.ID] = a.DisplayName
		}
	}

	var lines []mlog.LogLine
	if s.d.Ring != nil {
		lines = s.d.Ring.Snapshot()
	}
	counts := map[string]int{"all": len(lines), "problems": 0, "claims": 0}
	events := []healthEvent{}
	for i := len(lines) - 1; i >= 0; i-- { // newest first
		l := lines[i]
		problem, claim := isProblem(l), isClaim(l)
		if problem {
			counts["problems"]++
		}
		if claim {
			counts["claims"]++
		}
		keep := filter == "all" || (filter == "problems" && problem) || (filter == "claims" && claim)
		if !keep || len(events) >= maxHealthEvents {
			continue
		}
		id, _ := l.Fields["account"].(string)
		events = append(events, healthEvent{
			At: l.TS, Level: l.Level, Kind: l.Kind,
			AccountID: id, AccountName: names[id], Msg: l.Msg,
		})
	}

	var lastScan *time.Time
	if s.d.Scans != nil {
		if scans, err := s.d.Scans.List(ctx); err == nil {
			for _, sc := range scans {
				if !sc.ScannedAt.IsZero() && (lastScan == nil || sc.ScannedAt.After(*lastScan)) {
					t := sc.ScannedAt
					lastScan = &t
				}
			}
		}
	}

	cans := []canaryOut{}
	for _, p := range []string{"twitch", "kick"} {
		if res, ok, err := canary.LoadResult(ctx, s.d.Q, p); err == nil && ok {
			cans = append(cans, canaryOut{Platform: p, OK: res.OK, Detail: res.Detail, CheckedAt: res.CheckedAt})
		}
	}

	running := []string{}
	if s.d.KickSidecars != nil {
		if v := s.d.KickSidecars(); v != nil {
			running = v
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"miner":         map[string]any{"started_at": s.d.StartTime, "version": s.d.Version, "commit": s.d.Commit},
		"kick_sidecars": map[string]any{"running": running},
		"last_scan_at":  lastScan,
		"canary":        cans,
		"events":        events,
		"counts":        counts,
	})
}

// getHealthLog downloads every ring line as plain text.
func (s *server) getHealthLog(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="grubdrops-log.txt"`)
	w.WriteHeader(http.StatusOK)
	if s.d.Ring == nil {
		return
	}
	var b strings.Builder
	for _, l := range s.d.Ring.Snapshot() {
		fmt.Fprintf(&b, "%s %s", l.TS.UTC().Format(time.RFC3339), l.Level)
		if l.Kind != "" {
			fmt.Fprintf(&b, " [%s]", l.Kind)
		}
		fmt.Fprintf(&b, " %s", l.Msg)
		keys := make([]string, 0, len(l.Fields))
		for k := range l.Fields {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(&b, " %s=%v", k, l.Fields[k])
		}
		b.WriteByte('\n')
	}
	_, _ = w.Write([]byte(b.String()))
}

// postHealthCanary starts a canary run on a detached context and returns 202.
func (s *server) postHealthCanary(w http.ResponseWriter, _ *http.Request) {
	if s.d.RunCanary != nil {
		run := s.d.RunCanary
		ctx, cancel := context.WithTimeout(context.Background(), canaryRunTimeout)
		go func() {
			defer cancel()
			if err := run(ctx); err != nil {
				slog.Warn("canary run-now failed", "err", err)
			}
		}()
	}
	w.WriteHeader(http.StatusAccepted)
}
