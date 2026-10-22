package api

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func at(day, h, m int) time.Time { return time.Date(2026, 10, day, h, m, 0, 0, time.UTC) }

// A window that stays inside one day, and one that crosses midnight, both
// report asleep inside (give or take the daily +/-20 min shift) and awake well
// outside, with the end falling on the right day.
func TestSleepWindowUntil(t *testing.T) {
	day := SleepWindow{Enabled: true, Start: 2 * 60, End: 8 * 60}
	for _, c := range []struct {
		now   time.Time
		sleep bool
	}{
		{at(10, 0, 30), false}, {at(10, 1, 30), false},
		{at(10, 3, 0), true}, {at(10, 7, 0), true},
		{at(10, 8, 40), false}, {at(10, 15, 0), false},
	} {
		_, got := day.Until(c.now, "acc")
		assert.Equal(t, c.sleep, got, c.now.Format("15:04"))
	}
	end, _ := day.Until(at(10, 5, 0), "acc")
	assert.WithinDuration(t, at(10, 8, 0), end, 20*time.Minute)

	night := SleepWindow{Enabled: true, Start: 23 * 60, End: 7 * 60}
	end, asleep := night.Until(at(10, 23, 50), "acc")
	assert.True(t, asleep)
	assert.WithinDuration(t, at(11, 7, 0), end, 20*time.Minute, "ends the next morning")
	end, asleep = night.Until(at(11, 3, 0), "acc")
	assert.True(t, asleep, "after midnight it is still last night's window")
	assert.WithinDuration(t, at(11, 7, 0), end, 20*time.Minute)
	_, asleep = night.Until(at(11, 12, 0), "acc")
	assert.False(t, asleep)

	_, asleep = SleepWindow{Start: 0, End: 24*60 - 1}.Until(at(10, 12, 0), "acc")
	assert.False(t, asleep, "disabled window never sleeps")
}

// The shift is the same for every check within a day and differs by account,
// so the edge does not flap and accounts do not all move in lockstep.
func TestSleepJitterIsStablePerDay(t *testing.T) {
	d := at(10, 0, 0)
	assert.Equal(t, jitter("a", d, "s"), jitter("a", d, "s"))
	seen := map[time.Duration]bool{}
	for _, id := range []string{"a", "b", "c", "d", "e", "f", "g", "h"} {
		j := jitter(id, d, "s")
		assert.LessOrEqual(t, j.Abs(), sleepJitter)
		seen[j] = true
	}
	assert.Greater(t, len(seen), 1)
}
