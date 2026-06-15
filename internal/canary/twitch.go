package canary

import (
	"context"
	"fmt"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/platform/twitch"
)

// beaconProber is the surface of twitch.Backend that TwitchProbe needs.
// Defined as an interface so tests can inject a fake if needed; in practice
// *twitch.Backend is the only real implementation.
type beaconProber interface {
	ProbeBeacon(ctx context.Context, sess platform.Session, channel string, interval time.Duration) error
}

// TwitchProbe is a standalone canary that verifies the Twitch watch-beacon
// transport is accepted for a given session and channel.
//
// IMPORTANT: a passing probe proves the beacon HTTP transport is accepted
// (HTTP 2xx from the SendEvents mutation), NOT that watch-time was credited
// toward a drop. Drop credit additionally requires an active campaign and a
// valid live stream — those are not verified here.
type TwitchProbe struct {
	backend        beaconProber
	beaconInterval time.Duration
}

// NewTwitchProbe creates a production TwitchProbe using the provided backend.
// Pass beaconInterval=60s to match the watcher's HeartbeatInterval (the minimum
// Twitch credits per beacon). Use NewTwitchProbeForTest for test doubles.
func NewTwitchProbe(b *twitch.Backend, beaconInterval time.Duration) TwitchProbe {
	return TwitchProbe{backend: b, beaconInterval: beaconInterval}
}

// NewTwitchProbeForTest creates a TwitchProbe pointed at a test endpoint.
// beaconInterval is fixed at 0 so tests don't sleep.
func NewTwitchProbeForTest(endpoint string) TwitchProbe {
	return TwitchProbe{
		backend:        twitch.NewForTest(endpoint),
		beaconInterval: 0,
	}
}

// Run sends two watch-beacons for the given channel and returns a Result
// describing whether the Twitch beacon transport is functional.
//
// OK=true means both beacons were accepted (HTTP 2xx, no GQL errors).
// OK=false means at least one beacon was rejected; Detail carries the error.
//
// The result CheckedAt is set by the caller (SaveResult stamps it); Run
// sets it to time.Now() so the Result is useful without a database round-trip.
func (p TwitchProbe) Run(ctx context.Context, sess platform.Session, channel string) Result {
	r := Result{CheckedAt: time.Now().UTC()}

	if err := p.backend.ProbeBeacon(ctx, sess, channel, p.beaconInterval); err != nil {
		r.OK = false
		r.Detail = fmt.Sprintf("beacon probe failed: %v", err)
		return r
	}

	r.OK = true
	r.Detail = "2 beacons accepted (transport OK — accrual credit requires an active campaign)"
	return r
}
