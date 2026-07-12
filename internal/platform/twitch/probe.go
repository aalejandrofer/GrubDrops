package twitch

import (
	"context"
	"fmt"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// NewForTest builds a Backend pointed at the given endpoint URL.
// Intended for use by external test packages (e.g. internal/canary)
// that need a Backend without a real Twitch connection. PubSub is disabled.
func NewForTest(endpoint string) *Backend {
	return newForTest(endpoint)
}

// ProbeBeacon is the canary entry point for the Twitch watch-beacon transport.
//
// It verifies that the Spade minute-watched beacon is accepted by Twitch for
// the given session and channel — without running a full watcher and without
// requiring a live drop campaign.
//
// IMPORTANT: a successful probe proves the beacon HTTP transport is accepted
// (HTTP 204), not that Twitch credited watch-time for a drop. Twitch silently
// discards watch-time that doesn't match an active campaign or a valid
// user_id, so transport acceptance and accrual credit are distinct.
//
// The probe:
//  1. Resolves the authenticated user's ID (the same call the watcher makes
//     at StartWatch time — ensures the token is valid and the user is reachable).
//  2. Calls heartbeat twice with a synthetic platform.Stream built from
//     channel (real IDs are not needed; the beacon server accepts any values).
//  3. Returns nil only when both beacons are accepted without error.
//
// beaconInterval controls the gap between the two heartbeats. Use 0 or a
// very short duration in tests. Production should pass 60s (the watcher's
// HeartbeatInterval, which is the minimum Twitch credits per beacon).
func (b *Backend) ProbeBeacon(ctx context.Context, sess platform.Session, channel string, beaconInterval time.Duration) error {
	// Build a synthetic stream. The beacon body uses these fields to populate
	// the minute-watched event properties. Synthetic IDs are fine here because
	// we're testing transport acceptance, not drop credit.
	stream := platform.Stream{
		Channel:     channel,
		ChannelID:   "0",
		BroadcastID: "0",
		GameID:      "0",
		Game:        "unknown",
	}

	handle, err := b.watch.start(ctx, sess, stream)
	if err != nil {
		return fmt.Errorf("probe: start watch: %w", err)
	}

	for i := 1; i <= 2; i++ {
		if err := b.watch.heartbeat(ctx, handle); err != nil {
			return fmt.Errorf("probe: beacon %d/%d: %w", i, 2, err)
		}
		if i < 2 {
			// Wait for the inter-beacon interval, but honour context cancellation.
			if beaconInterval > 0 {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(beaconInterval):
				}
			}
		}
	}

	return nil
}
