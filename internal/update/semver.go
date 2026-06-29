// Package update checks GitHub for a newer GrubDrops release and exposes the
// result to the UI.
package update

import (
	"strconv"
	"strings"
)

// parseSemver extracts MAJOR.MINOR.PATCH from a tag like "v1.3.4",
// "1.3.4-itempull", or "v1.3". A leading "v" is stripped and anything from the
// first non-(digit|dot) character on is ignored (build/prerelease suffix).
// A missing patch (or minor) defaults to 0. ok is false when there aren't at
// least major+minor numeric components.
func parseSemver(s string) (maj, min, patch int, ok bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "v")
	// Truncate at the first char that isn't a digit or '.'.
	end := len(s)
	for i, r := range s {
		if (r < '0' || r > '9') && r != '.' {
			end = i
			break
		}
	}
	s = s[:end]
	if s == "" {
		return 0, 0, 0, false
	}
	parts := strings.Split(s, ".")
	nums := make([]int, 3)
	have := 0
	for i := 0; i < len(parts) && i < 3; i++ {
		if parts[i] == "" {
			return 0, 0, 0, false
		}
		n, err := strconv.Atoi(parts[i])
		if err != nil {
			return 0, 0, 0, false
		}
		nums[i] = n
		have++
	}
	if have < 2 { // need at least MAJOR.MINOR
		return 0, 0, 0, false
	}
	return nums[0], nums[1], nums[2], true
}

// Newer reports whether latest is a strictly higher semver than current.
// Returns false if EITHER side fails to parse (so dev/source/empty builds never
// show an update).
func Newer(current, latest string) bool {
	cMaj, cMin, cPatch, cok := parseSemver(current)
	lMaj, lMin, lPatch, lok := parseSemver(latest)
	if !cok || !lok {
		return false
	}
	if lMaj != cMaj {
		return lMaj > cMaj
	}
	if lMin != cMin {
		return lMin > cMin
	}
	return lPatch > cPatch
}
