package timeutil

import (
	"testing"
	"time"
)

func TestParseClock(t *testing.T) {
	ok := map[string]int{"00:00": 0, "02:00": 120, "23:59": 1439, "07:05": 425, "12:30": 750}
	for s, want := range ok {
		got, valid := ParseClock(s)
		if !valid || got != want {
			t.Errorf("ParseClock(%q) = %d, %v; want %d", s, got, valid, want)
		}
	}
	for _, s := range []string{"", "24:00", "7:00", "07:60", "07-00", "0700", " 07:00", "07:00 ", "ab:cd", "-1:00", "07:5"} {
		if _, valid := ParseClock(s); valid {
			t.Errorf("ParseClock(%q) valid, want invalid", s)
		}
	}
}

func TestFormatClock(t *testing.T) {
	for _, m := range []int{0, 120, 425, 1439} {
		got, ok := ParseClock(FormatClock(m))
		if !ok || got != m {
			t.Errorf("round trip %d -> %q -> %d", m, FormatClock(m), got)
		}
	}
}

func at(loc *time.Location, y int, mo time.Month, d, h, mi int) time.Time {
	return time.Date(y, mo, d, h, mi, 0, 0, loc)
}

func clock(t *testing.T, s string) int {
	t.Helper()
	m, ok := ParseClock(s)
	if !ok {
		t.Fatalf("bad clock %q", s)
	}
	return m
}

func TestSleepUntil_NoJitter(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Madrid")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name       string
		start, end string
		now        time.Time
		in         bool
		wake       time.Time
	}{
		{"same-day before", "02:00", "08:00", at(loc, 2026, 6, 10, 1, 59), false, time.Time{}},
		{"same-day start boundary", "02:00", "08:00", at(loc, 2026, 6, 10, 2, 0), true, at(loc, 2026, 6, 10, 8, 0)},
		{"same-day inside", "02:00", "08:00", at(loc, 2026, 6, 10, 7, 59), true, at(loc, 2026, 6, 10, 8, 0)},
		{"same-day end boundary", "02:00", "08:00", at(loc, 2026, 6, 10, 8, 0), false, time.Time{}},
		{"same-day evening", "02:00", "08:00", at(loc, 2026, 6, 10, 23, 0), false, time.Time{}},
		{"cross-midnight before start", "23:30", "07:00", at(loc, 2026, 6, 10, 23, 29), false, time.Time{}},
		{"cross-midnight before midnight", "23:30", "07:00", at(loc, 2026, 6, 10, 23, 30), true, at(loc, 2026, 6, 11, 7, 0)},
		{"cross-midnight after midnight", "23:30", "07:00", at(loc, 2026, 6, 11, 0, 30), true, at(loc, 2026, 6, 11, 7, 0)},
		{"cross-midnight last minute", "23:30", "07:00", at(loc, 2026, 6, 11, 6, 59), true, at(loc, 2026, 6, 11, 7, 0)},
		{"cross-midnight wake boundary", "23:30", "07:00", at(loc, 2026, 6, 11, 7, 0), false, time.Time{}},
		{"cross-midnight noon", "23:30", "07:00", at(loc, 2026, 6, 11, 12, 0), false, time.Time{}},
		{"month end crossing", "22:00", "06:00", at(loc, 2026, 6, 30, 23, 0), true, at(loc, 2026, 7, 1, 6, 0)},
		{"year end crossing", "22:00", "06:00", at(loc, 2027, 1, 1, 3, 0), true, at(loc, 2027, 1, 1, 6, 0)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			wake, in := SleepUntil(c.now, loc, clock(t, c.start), clock(t, c.end), false)
			if in != c.in || !wake.Equal(c.wake) {
				t.Fatalf("got in=%v wake=%v; want in=%v wake=%v", in, wake, c.in, c.wake)
			}
		})
	}
}

// The window is read in loc, not in the zone of now.
func TestSleepUntil_UsesZone(t *testing.T) {
	tokyo, _ := time.LoadLocation("Asia/Tokyo")
	now := time.Date(2026, 6, 10, 18, 0, 0, 0, time.UTC) // 03:00 next day in Tokyo
	wake, in := SleepUntil(now, tokyo, clock(t, "02:00"), clock(t, "08:00"), false)
	if !in || !wake.Equal(at(tokyo, 2026, 6, 11, 8, 0)) {
		t.Fatalf("in=%v wake=%v", in, wake)
	}
	if _, in := SleepUntil(now, time.UTC, clock(t, "02:00"), clock(t, "08:00"), false); in {
		t.Fatal("18:00 UTC is outside 02:00-08:00 UTC")
	}
	// A nil zone is UTC.
	if _, in := SleepUntil(now, nil, clock(t, "17:00"), clock(t, "19:00"), false); !in {
		t.Fatal("nil zone should be UTC")
	}
}

func TestSleepUntil_StartEqualsEndNeverSleeps(t *testing.T) {
	for h := range 24 {
		if _, in := SleepUntil(at(time.UTC, 2026, 6, 10, h, 0), time.UTC, 120, 120, true); in {
			t.Fatalf("start==end slept at %02d:00", h)
		}
	}
}

func TestSleepUntil_DST(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	// 2026-03-08: clocks jump 02:00 EST to 03:00 EDT.
	wake, in := SleepUntil(at(ny, 2026, 3, 8, 4, 0), ny, clock(t, "01:00"), clock(t, "05:00"), false)
	if !in || !wake.Equal(at(ny, 2026, 3, 8, 5, 0)) {
		t.Fatalf("spring forward: in=%v wake=%v", in, wake)
	}
	// Cross-midnight window into the spring-forward night.
	wake, in = SleepUntil(at(ny, 2026, 3, 8, 3, 30), ny, clock(t, "23:00"), clock(t, "07:00"), false)
	if !in || !wake.Equal(at(ny, 2026, 3, 8, 7, 0)) {
		t.Fatalf("spring forward cross-midnight: in=%v wake=%v", in, wake)
	}
	// 2026-11-01: clocks fall back 02:00 EDT to 01:00 EST; 01:30 happens twice.
	first := time.Date(2026, 11, 1, 5, 30, 0, 0, time.UTC)  // 01:30 EDT
	second := time.Date(2026, 11, 1, 6, 30, 0, 0, time.UTC) // 01:30 EST
	for _, now := range []time.Time{first, second} {
		wake, in = SleepUntil(now, ny, clock(t, "00:00"), clock(t, "03:00"), false)
		if !in || !wake.Equal(at(ny, 2026, 11, 1, 3, 0)) {
			t.Fatalf("fall back %v: in=%v wake=%v", now, in, wake)
		}
	}
	if _, in := SleepUntil(at(ny, 2026, 11, 1, 3, 0), ny, clock(t, "00:00"), clock(t, "03:00"), false); in {
		t.Fatal("fall back: 03:00 should be awake")
	}
}

func TestSleepJitter_BoundedAndVaries(t *testing.T) {
	seen := map[time.Duration]bool{}
	day := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	for i := range 400 {
		d := day.AddDate(0, 0, i)
		for _, tag := range []string{"start", "end"} {
			j := sleepJitter(d, tag)
			if j < -SleepJitter || j > SleepJitter {
				t.Fatalf("%s %s jitter %v out of ±%v", d.Format(time.DateOnly), tag, j, SleepJitter)
			}
			if j%time.Minute != 0 {
				t.Fatalf("jitter %v not whole minutes", j)
			}
			seen[j] = true
			if sleepJitter(d, tag) != j {
				t.Fatal("jitter not deterministic")
			}
		}
	}
	if len(seen) < 20 {
		t.Fatalf("jitter takes only %d values over 400 days", len(seen))
	}
}

// With jitter, each window's bounds stay within ±20 min of the set times,
// actually move on some days, and the wake reported anywhere inside one
// window is the same instant.
func TestSleepUntil_JitterStableAndBounded(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Madrid")
	start, end := clock(t, "23:30"), clock(t, "07:00")
	moved := 0
	for i := range 120 {
		d := time.Date(2026, 1, 1, 0, 0, 0, 0, loc).AddDate(0, 0, i)
		y, m, dd := d.Date()
		s, e := sleepBounds(y, m, dd, loc, start, end, true)
		ns := at(loc, y, m, dd, 23, 30)
		ne := at(loc, y, m, dd+1, 7, 0)
		if ds := s.Sub(ns); ds < -SleepJitter || ds > SleepJitter {
			t.Fatalf("day %d start off by %v", i, ds)
		}
		if de := e.Sub(ne); de < -SleepJitter || de > SleepJitter {
			t.Fatalf("day %d end off by %v", i, de)
		}
		if !s.Equal(ns) || !e.Equal(ne) {
			moved++
		}
		// Inside: just after the jittered start, at nominal midnight and just
		// before the jittered end. All report the same wake.
		var wakes []time.Time
		for _, now := range []time.Time{s, s.Add(time.Minute), at(loc, y, m, dd+1, 3, 0), e.Add(-time.Second)} {
			w, in := SleepUntil(now, loc, start, end, true)
			if !in {
				t.Fatalf("day %d: %v not in window [%v, %v)", i, now, s, e)
			}
			wakes = append(wakes, w)
		}
		for _, w := range wakes {
			if !w.Equal(e) {
				t.Fatalf("day %d: wake %v, want %v", i, w, e)
			}
		}
		if _, in := SleepUntil(s.Add(-time.Second), loc, start, end, true); in {
			t.Fatalf("day %d: asleep before the jittered start", i)
		}
		if _, in := SleepUntil(e, loc, start, end, true); in {
			t.Fatalf("day %d: asleep at the jittered wake", i)
		}
	}
	if moved == 0 {
		t.Fatal("jitter never moved a window")
	}
}

// A short window never inverts or collapses under jitter.
func TestSleepUntil_JitterNeverInverts(t *testing.T) {
	loc := time.UTC
	for _, w := range [][2]string{{"02:00", "02:10"}, {"23:55", "00:05"}, {"02:00", "02:45"}, {"02:00", "08:00"}} {
		start, end := clock(t, w[0]), clock(t, w[1])
		span := time.Duration((end-start+1440)%1440) * time.Minute
		floor := min(span, minSleepSpan)
		for i := range 400 {
			d := time.Date(2026, 1, 1, 0, 0, 0, 0, loc).AddDate(0, 0, i)
			y, m, dd := d.Date()
			s, e := sleepBounds(y, m, dd, loc, start, end, true)
			if e.Sub(s) < floor {
				t.Fatalf("%v day %d: window %v..%v is %v, want >= %v", w, i, s, e, e.Sub(s), floor)
			}
		}
	}
}

// The wake time is never itself inside a window, even when a near-24h window
// overlaps the next day's under jitter.
func TestSleepUntil_WakeIsAwake(t *testing.T) {
	loc, _ := time.LoadLocation("America/New_York")
	start, end := clock(t, "08:00"), clock(t, "07:50")
	now := time.Date(2026, 3, 1, 0, 0, 0, 0, loc)
	for i := 0; i < 60*24*20; i += 7 {
		n := now.Add(time.Duration(i) * time.Minute)
		w, in := SleepUntil(n, loc, start, end, true)
		if !in {
			continue
		}
		if !w.After(n) {
			t.Fatalf("%v: wake %v not after now", n, w)
		}
		if _, again := SleepUntil(w, loc, start, end, true); again {
			t.Fatalf("%v: wake %v is inside a window", n, w)
		}
	}
}
