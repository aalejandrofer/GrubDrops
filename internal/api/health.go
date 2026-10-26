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
	"github.com/aalejandrofer/grubdrops/internal/store"
)

// maxHealthEvents caps the events returned after the filter.
const maxHealthEvents = 50

// canaryRunTimeout bounds one run-now canary run: two Twitch beacons at 5 s
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
	At          *string `json:"at"`
	Level       string  `json:"level"`
	Kind        string  `json:"kind"`
	AccountID   string  `json:"account_id"`
	AccountName string  `json:"account_name"`
	Msg         string  `json:"msg"`
	// Fields holds only the healthFieldAllow keys of the ring line.
	Fields map[string]string `json:"fields,omitempty"`
}

// healthFieldAllow lists the ring line fields that reach the browser. Fields
// are untrusted and may hold secrets, so nothing else is copied.
var healthFieldAllow = []string{"game", "campaign", "benefit_name", "channel", "state", "prev", "err"}

// maxHealthFieldRunes cuts each allowlisted value.
const maxHealthFieldRunes = 200

func healthFields(l mlog.LogLine) map[string]string {
	var out map[string]string
	for _, k := range healthFieldAllow {
		v, ok := l.Fields[k]
		if !ok || v == nil {
			continue
		}
		str := fmt.Sprint(v)
		if r := []rune(str); len(r) > maxHealthFieldRunes {
			str = string(r[:maxHealthFieldRunes])
		}
		if out == nil {
			out = map[string]string{}
		}
		out[k] = str
	}
	return out
}

// LogAccountID returns the account a ring line is about, or "".
func LogAccountID(l mlog.LogLine) string {
	id, _ := l.Fields["account"].(string)
	return id
}

func logEvent(l mlog.LogLine, accountName string) healthEvent {
	return healthEvent{
		At: rfc3339(l.TS), Level: l.Level, Kind: l.Kind,
		AccountID: LogAccountID(l), AccountName: accountName, Msg: l.Msg,
		Fields: healthFields(l),
	}
}

// LogEventJSON is the Event body of a health event and a `log` SSE event.
func LogEventJSON(l mlog.LogLine, accountName string) any {
	return logEvent(l, accountName)
}

type canaryOut struct {
	Platform  string  `json:"platform"`
	OK        bool    `json:"ok"`
	Skipped   bool    `json:"skipped"`
	Detail    string  `json:"detail"`
	CheckedAt *string `json:"checked_at"`
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
			names[a.ID] = store.AccountName(a)
		}
	}

	var lines []mlog.LogLine
	if s.d.Ring != nil {
		lines = s.d.Ring.Snapshot()
	}
	counts := map[string]int{"all": 0, "problems": 0, "claims": 0}
	events := []healthEvent{}
	for i := len(lines) - 1; i >= 0; i-- { // newest first
		l := lines[i]
		// DEBUG lines stay in the full log download only.
		if l.Level == "DEBUG" {
			continue
		}
		counts["all"]++
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
		events = append(events, logEvent(l, names[LogAccountID(l)]))
	}

	var lastScan time.Time
	if s.d.Scans != nil {
		if scans, err := s.d.Scans.List(ctx); err == nil {
			for _, sc := range scans {
				if sc.ScannedAt.After(lastScan) {
					lastScan = sc.ScannedAt
				}
			}
		}
	}

	cans := []canaryOut{}
	for _, p := range []string{"twitch", "kick"} {
		if res, ok, err := canary.LoadResult(ctx, s.d.Q, p); err == nil && ok {
			cans = append(cans, canaryOut{Platform: p, OK: res.OK, Skipped: res.Skipped, Detail: res.Detail, CheckedAt: rfc3339(res.CheckedAt)})
		}
	}

	running := []string{}
	if s.d.KickSidecars != nil {
		if v := s.d.KickSidecars(); v != nil {
			running = v
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"miner":         map[string]any{"started_at": rfc3339(s.d.StartTime), "version": s.d.Version, "commit": s.d.Commit},
		"kick_sidecars": map[string]any{"running": running},
		"last_scan_at":  rfc3339(lastScan),
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

// postHealthCanary starts a canary run on the root context and returns 202,
// so a run ends with the miner instead of outliving the database.
func (s *server) postHealthCanary(w http.ResponseWriter, _ *http.Request) {
	// A run already in flight absorbs this request; still 202.
	s.startCanary()
	w.WriteHeader(http.StatusAccepted)
}

// startCanary starts one background canary run on the root context with
// canaryRunTimeout. It returns false when no runner is wired or a run is
// already in flight. A run that ends with canaryRerun set starts one more.
func (s *server) startCanary() bool {
	if s.d.RunCanary == nil || !s.canaryRunning.CompareAndSwap(false, true) {
		return false
	}
	// This run reads the current accounts, so it serves any pending rerun.
	s.canaryRerun.Store(false)
	run := s.d.RunCanary
	ctx, cancel := context.WithTimeout(s.rootCtx(), canaryRunTimeout)
	go func() {
		defer cancel()
		if err := run(ctx); err != nil {
			slog.Warn("canary run-now failed", "err", err)
		}
		s.canaryRunning.Store(false)
		// Swap after Store: a rerun asked for while this run was in flight
		// set the flag before its own start failed, so it is seen here.
		if s.canaryRerun.Swap(false) && s.rootCtx().Err() == nil {
			s.startCanary()
		}
	}()
	return true
}

// rerunSkippedCanary starts a canary run when the stored result for
// platform says it skipped for lack of an active account. Called after an
// account on that platform is resumed or signs in. A run already in flight
// may have read the accounts before the change, so the request waits for it
// and then runs once more.
func (s *server) rerunSkippedCanary(ctx context.Context, platform string) {
	res, ok, err := canary.LoadResult(ctx, s.d.Q, platform)
	if err != nil || !ok || !res.Skipped {
		return
	}
	s.canaryRerun.Store(true)
	s.startCanary()
}
