// Package summary sends the daily watch summary at each local midnight.
package summary

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/i18n"
	"github.com/aalejandrofer/grubdrops/internal/notify"
	"github.com/aalejandrofer/grubdrops/internal/store"
)

// Config wires the summary loop. Zone and Now may be nil (time.Local, time.Now).
type Config struct {
	Settings *store.Settings
	Minutes  *store.WatchMinutes
	Claims   *store.ClaimLog
	Notifier notify.Notifier
	Zone     func() *time.Location
	Now      func() time.Time
}

// maxWait caps one sleep so a clock jump or zone change is noticed.
var maxWait = 30 * time.Second

// NextMidnight returns the first local midnight after now in loc.
func NextMidnight(now time.Time, loc *time.Location) time.Time {
	y, m, d := now.In(loc).Date()
	return time.Date(y, m, d+1, 0, 0, 0, 0, loc)
}

// Run blocks until ctx ends. At each midnight, when the daily summary is on,
// it sends one EventSummary for the day that ended.
func Run(ctx context.Context, c Config) {
	loc := c.location()
	next := NextMidnight(c.now(), loc)
	for {
		for {
			if l := c.location(); l.String() != loc.String() {
				loc = l
				next = NextMidnight(c.now(), loc)
			}
			wait := next.Sub(c.now())
			if wait <= 0 {
				break
			}
			if wait > maxWait {
				wait = maxWait
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			}
		}
		c.send(ctx, next)
		next = NextMidnight(c.now(), loc)
	}
}

func (c Config) location() *time.Location {
	if c.Zone != nil {
		if l := c.Zone(); l != nil {
			return l
		}
	}
	return time.Local
}

func (c Config) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// send reports the day that ended at midnight.
func (c Config) send(ctx context.Context, midnight time.Time) {
	on, err := c.Settings.NotifyDailySummary(ctx)
	if err != nil || !on {
		return
	}
	y, m, d := midnight.Date()
	start := time.Date(y, m, d-1, 0, 0, 0, 0, midnight.Location())
	day := start.Format("2006-01-02")

	minutes, err := c.Minutes.Day(ctx, day)
	if err != nil {
		slog.Warn("daily summary: minutes", "kind", "error", "err", err)
		return
	}
	sinceStart, err1 := c.Claims.CountSince(ctx, start)
	sinceEnd, err2 := c.Claims.CountSince(ctx, midnight)
	if err1 != nil || err2 != nil {
		slog.Warn("daily summary: claims", "kind", "error", "err", err1, "err2", err2)
		return
	}
	claims := sinceStart - sinceEnd

	lang, _ := c.Settings.UILanguage(ctx)
	fields := map[string]any{
		"day":     day,
		"minutes": minutes,
		"claims":  claims,
		"title":   i18n.T(lang, "summary.title"),
		"msg":     fmt.Sprintf(i18n.T(lang, "summary.message"), day, minutes, claims),
	}
	if err := c.Notifier.Notify(ctx, notify.EventSummary, fields); err != nil {
		slog.Warn("daily summary: send", "kind", "error", "err", err)
	}
}
