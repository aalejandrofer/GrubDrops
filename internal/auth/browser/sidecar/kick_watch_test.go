package sidecar

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
)

// parseWatchAlive must report alive ONLY when the IVS <video> exists and
// is actually playing. A missing video, a paused player, or malformed
// probe output all count as not-alive so the watcher re-picks a channel
// instead of holding a dead tab.
func TestParseWatchAlive(t *testing.T) {
	cases := []struct {
		name   string
		status string
		want   bool
	}{
		{"playing", `{"video":true,"playing":true,"readyState":4}`, true},
		{"paused", `{"video":true,"playing":false,"readyState":4}`, false},
		{"no video element", `{"video":false,"playing":false}`, false},
		{"video present but not playing", `{"video":true,"playing":false}`, false},
		{"empty string", ``, false},
		{"malformed json", `not json`, false},
		{"probe error shape", `{"video":false,"playing":false,"err":"boom"}`, false},
		{"extra fields ignored", `{"video":true,"playing":true,"foo":"bar"}`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, parseWatchAlive(tc.status))
		})
	}
}

// evalWatchAlive must fold currentTime progression into the liveness
// verdict: a player reporting playing but whose currentTime never advances
// (stream went offline / froze) is dead after maxWatchStalls probes so the
// watcher re-picks a channel.
func TestEvalWatchAlive_StallDetection(t *testing.T) {
	k := NewKick(nil)
	h := "tab_test"

	probe := func(playing bool, ct float64) string {
		v := "false"
		if playing {
			v = "true"
		}
		return `{"video":true,"playing":` + v + `,"currentTime":` + strconv.FormatFloat(ct, 'f', -1, 64) + `}`
	}

	// First probe seeds the baseline => alive.
	assert.True(t, k.evalWatchAlive(h, probe(true, 1.0)))
	// Advancing => alive, resets stall counter.
	assert.True(t, k.evalWatchAlive(h, probe(true, 2.0)))
	assert.True(t, k.evalWatchAlive(h, probe(true, 3.5)))

	// Now freeze: same currentTime. Tolerated for maxWatchStalls probes...
	for i := 0; i < maxWatchStalls; i++ {
		assert.True(t, k.evalWatchAlive(h, probe(true, 3.5)), "stall %d should still be tolerated", i+1)
	}
	// ...then declared dead.
	assert.False(t, k.evalWatchAlive(h, probe(true, 3.5)), "should be dead after maxWatchStalls non-advancing probes")

	// Recovery: currentTime advances again => alive, stalls reset.
	assert.True(t, k.evalWatchAlive(h, probe(true, 4.0)))

	// A player that reports not-playing is dead immediately regardless of time.
	assert.False(t, k.evalWatchAlive(h, probe(false, 5.0)))
}
