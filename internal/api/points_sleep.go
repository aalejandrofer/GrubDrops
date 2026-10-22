package api

import (
	"context"
	"fmt"
	"hash/fnv"
	"strings"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

// A sleep window is a time of day (local) when an account does not watch for
// channel points, like a person going to bed. It is used instead of timed
// breaks because stream lengths are unknown: a break cut mid-stream at a
// random moment is stranger than a fixed nightly gap. The default, 02:00-08:00,
// leaves the account watching 75% of the day.
const (
	defaultSleepStart = 2 * 60
	defaultSleepEnd   = 8 * 60
	// sleepJitter shifts each end of the window by up to this much, per day, so
	// the account does not go quiet and return at the exact same minute daily.
	sleepJitter = 20 * time.Minute
)

// SleepWindow is minutes since local midnight. End before Start means the
// window crosses midnight (e.g. 23:00-07:00).
type SleepWindow struct {
	Enabled    bool
	Start, End int
}

// PointsSleepKey is the per-account KV key holding "HH:MM-HH:MM" or "off".
func PointsSleepKey(accountID string) string { return "points_sleep:" + accountID }

func clock(min int) string { return fmt.Sprintf("%02d:%02d", min/60, min%60) }

// StartHM and EndHM render the window for <input type="time">.
func (s SleepWindow) StartHM() string { return clock(s.Start) }
func (s SleepWindow) EndHM() string   { return clock(s.End) }

func (s SleepWindow) encode() string {
	if !s.Enabled {
		return "off"
	}
	return s.StartHM() + "-" + s.EndHM()
}

func parseClock(s string) (int, bool) {
	var h, m int
	if _, err := fmt.Sscanf(strings.TrimSpace(s), "%d:%d", &h, &m); err != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, false
	}
	return h*60 + m, true
}

// PointsSleepWindow returns the account's sleep window; unset means the
// default 02:00-08:00.
func PointsSleepWindow(ctx context.Context, q *gen.Queries, accountID string) SleepWindow {
	def := SleepWindow{Enabled: true, Start: defaultSleepStart, End: defaultSleepEnd}
	v, err := q.GetSettingString(ctx, PointsSleepKey(accountID))
	if err != nil {
		return def
	}
	if string(v) == "off" {
		return SleepWindow{Start: defaultSleepStart, End: defaultSleepEnd}
	}
	a, b, ok := strings.Cut(string(v), "-")
	start, ok1 := parseClock(a)
	end, ok2 := parseClock(b)
	if !ok || !ok1 || !ok2 || start == end {
		return def
	}
	return SleepWindow{Enabled: true, Start: start, End: end}
}

// jitter is a stable offset in [-sleepJitter, +sleepJitter] for one edge of one
// day's window, so repeated checks within a day agree with each other.
func jitter(accountID string, day time.Time, edge string) time.Duration {
	h := fnv.New32a()
	_, _ = h.Write([]byte(accountID + day.Format("2006-01-02") + edge))
	span := int(2*sleepJitter/time.Minute) + 1
	return time.Duration(int(h.Sum32())%span)*time.Minute - sleepJitter
}

// Until reports whether now (in now's location) falls inside the window and,
// if so, when it ends.
func (s SleepWindow) Until(now time.Time, accountID string) (time.Time, bool) {
	if !s.Enabled {
		return time.Time{}, false
	}
	// Check the window that began yesterday (it may still be running past
	// midnight) and the one beginning today.
	for back := -1; back <= 0; back++ {
		day := time.Date(now.Year(), now.Month(), now.Day()+back, 0, 0, 0, 0, now.Location())
		start := day.Add(time.Duration(s.Start)*time.Minute + jitter(accountID, day, "s"))
		endDay := day
		if s.End <= s.Start {
			endDay = day.AddDate(0, 0, 1)
		}
		end := time.Date(endDay.Year(), endDay.Month(), endDay.Day(), 0, 0, 0, 0, now.Location()).
			Add(time.Duration(s.End)*time.Minute + jitter(accountID, day, "e"))
		if !now.Before(start) && now.Before(end) {
			return end, true
		}
	}
	return time.Time{}, false
}
