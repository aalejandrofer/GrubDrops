package canary

import (
	"context"
	"fmt"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/platform/kick"
)

// wsProber is the surface of kick.Backend that KickProbe needs.
// Defined as an interface so tests can inject a fake; in practice
// *kick.Backend is the only real implementation.
type wsProber interface {
	ProbeWS(ctx context.Context, sess platform.Session, channel string, window time.Duration) error
}

// KickProbe is a standalone canary that verifies the Kick WebSocket
// watch-time transport is functional for a given session and channel.
//
// It dials the Kick viewer WebSocket, performs the initial auth handshake,
// and confirms the periodic channel_handshake cadence fires and a pong is
// received — the exact frame exchange our accrual testing tied to watch-time
// credit (see project-ws-accrual-retest-plan memory entry).
//
// IMPORTANT: a passing probe proves WS transport health (connect + periodic
// handshake cadence + server round-trip), NOT that watch-time was credited
// toward a drop. Accrual additionally requires a live stream and an active
// drop campaign — those are not verified here.
type KickProbe struct {
	backend wsProber
	window  time.Duration
}

// NewKickProbe creates a production KickProbe using the provided backend.
// Pass window ≥ 30s to observe at least one full periodic handshake cycle.
// Use NewKickProbeForTest for test doubles.
func NewKickProbe(b *kick.Backend, window time.Duration) KickProbe {
	return KickProbe{backend: b, window: window}
}

// NewKickProbeForTest creates a KickProbe pointed at test-server endpoints.
// tokenServerURL is the base URL of a fake HTTP server for the viewer-token
// endpoint; wsServerURL is the ws:// URL of a fake WS server.
// The probe window is fixed at 80ms so tests run without sleeping.
func NewKickProbeForTest(tokenServerURL, wsServerURL string) KickProbe {
	return KickProbe{
		backend: kick.NewKickBackendForTest(tokenServerURL, wsServerURL),
		window:  80 * time.Millisecond,
	}
}

// Run dials the Kick viewer WebSocket and runs the presence pump for the
// configured window, then returns a Result describing whether the transport
// is healthy.
//
// OK=true means:
//   - WS connected.
//   - ≥2 channel_handshake frames sent (initial + ≥1 periodic cadence tick).
//   - ≥1 inbound frame (pong) received from the server.
//
// OK=false means at least one of the above was not observed; Detail carries
// the specific failure.
//
// CheckedAt is set to time.Now() at call time so the Result is useful without
// a database round-trip.
func (p KickProbe) Run(ctx context.Context, sess platform.Session, channel string) Result {
	r := Result{CheckedAt: time.Now().UTC()}

	if err := p.backend.ProbeWS(ctx, sess, channel, p.window); err != nil {
		r.OK = false
		r.Detail = fmt.Sprintf("ws probe failed: %v", err)
		return r
	}

	r.OK = true
	r.Detail = "WS transport OK: channel_handshake cadence confirmed + pong received (accrual credit requires live stream + active campaign)"
	return r
}
