// Package timeutil holds the display timezone for the web UI. The zone is
// resolved from (in order) the in-app Timezone setting, the TZ environment
// variable, then UTC, and is swappable at runtime so a setting change applies
// live without a restart.
package timeutil

import (
	"sync/atomic"
	"time"
)

// Valid reports whether name is a loadable IANA zone. Empty is valid and
// means "unset" (fall back to env/UTC).
func Valid(name string) bool {
	if name == "" {
		return true
	}
	_, err := time.LoadLocation(name)
	return err == nil
}

// Resolve picks the effective display location: the setting if loadable, else
// the TZ env value if loadable, else UTC. Never returns nil.
func Resolve(setting, env string) *time.Location {
	if setting != "" {
		if loc, err := time.LoadLocation(setting); err == nil {
			return loc
		}
	}
	if env != "" {
		if loc, err := time.LoadLocation(env); err == nil {
			return loc
		}
	}
	return time.UTC
}

// Zone holds the current display location behind an atomic pointer so readers
// (request handlers formatting timestamps) and the settings writer never race.
type Zone struct {
	p atomic.Pointer[time.Location]
}

// NewZone returns a Zone set to loc (UTC when loc is nil).
func NewZone(loc *time.Location) *Zone {
	z := &Zone{}
	if loc == nil {
		loc = time.UTC
	}
	z.p.Store(loc)
	return z
}

// Location returns the current display location, never nil.
func (z *Zone) Location() *time.Location {
	if loc := z.p.Load(); loc != nil {
		return loc
	}
	return time.UTC
}

// Name returns the IANA name of the current location (e.g. "Asia/Shanghai",
// "UTC"), suitable for a browser Intl timeZone.
func (z *Zone) Name() string { return z.Location().String() }

// Set swaps the current location live. A nil argument is ignored so callers
// never accidentally blank the zone.
func (z *Zone) Set(loc *time.Location) {
	if loc == nil {
		return
	}
	z.p.Store(loc)
}
