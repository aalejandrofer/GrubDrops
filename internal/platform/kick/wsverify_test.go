package kick

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// TestLive_WSAccrual drives the real backend WS watch path against live Kick
// and asserts drop watch-time accrues with NO browser. Gated on
// KICK_WS_TEST_TOKEN (a full session_token, "id|secret"); skipped otherwise so
// CI/unit runs stay offline.
//
//	KICK_WS_TEST_TOKEN='376...|abc' KICK_WS_TEST_CHANNEL=coconutb KICK_WS_TEST_MIN=10 \
//	  go test ./internal/platform/kick/ -run TestLive_WSAccrual -v -timeout 20m
func TestLive_WSAccrual(t *testing.T) {
	tok := os.Getenv("KICK_WS_TEST_TOKEN")
	if tok == "" {
		t.Skip("set KICK_WS_TEST_TOKEN to run the live WS accrual test")
	}
	channel := os.Getenv("KICK_WS_TEST_CHANNEL")
	if channel == "" {
		channel = "coconutb"
	}
	mins := 10
	if v := os.Getenv("KICK_WS_TEST_MIN"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			mins = n
		}
	}

	// No sidecar client + EnableBrowserWatch NOT called → browserWatch stays
	// off → StartWatch uses the pure-WS path. This is exactly the runtime wiring
	// for kick_watch_mode = "ws".
	b := New(nil, nil, "x", 0, time.Minute)
	sess, err := encodeSession(kickSession{
		Cookies: []cookie{{Name: "session_token", Value: tok, Domain: ".kick.com", Path: "/"}},
	})
	if err != nil {
		t.Fatalf("encode session: %v", err)
	}

	totalMinutes := func(label string) int {
		prog, perr := b.InventoryProgress(context.Background(), sess)
		if perr != nil {
			t.Fatalf("%s progress: %v", label, perr)
		}
		sum := 0
		for _, p := range prog {
			sum += p.MinutesWatched
		}
		t.Logf("%s: %d reward rows, %d total minutes watched", label, len(prog), sum)
		return sum
	}

	before := totalMinutes("before")

	startCtx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	h, err := b.StartWatch(startCtx, sess, platform.Stream{Channel: channel})
	if err != nil {
		t.Fatalf("StartWatch(%s): %v", channel, err)
	}
	t.Logf("WS watch started on %s; running %d min (no browser)…", channel, mins)

	// Watch for the configured duration, heartbeating every 30s to confirm the
	// loop stays alive (and to mirror how the watcher polls it).
	deadline := time.Now().Add(time.Duration(mins) * time.Minute)
	for time.Now().Before(deadline) {
		time.Sleep(30 * time.Second)
		if hbErr := b.Heartbeat(context.Background(), h); hbErr != nil {
			t.Fatalf("heartbeat reported watch dead: %v", hbErr)
		}
	}
	if stopErr := b.StopWatch(context.Background(), h); stopErr != nil {
		t.Errorf("StopWatch: %v", stopErr)
	}

	after := totalMinutes("after")
	if after <= before {
		t.Fatalf("FAIL: no accrual — total watch-minutes did not grow (before=%d after=%d). WS-only path did not earn.", before, after)
	}
	t.Logf("PASS: WS-only accrued +%d watch-minutes over ~%d min (before=%d after=%d), zero browser.",
		after-before, mins, before, after)
}
