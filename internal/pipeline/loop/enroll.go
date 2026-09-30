package loop

import (
	"context"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// Watch-to-enroll discovery (Config.EnrollDiscovery). A TV-client Twitch
// token can't read the campaign list, but Inventory lists every campaign
// the viewer is enrolled in, and Twitch enrolls a viewer who watches a
// channel that counts toward a campaign. When the planner says Idle, the
// loop watches the top drops-enabled channel of the next whitelisted game
// for EnrollWatch, then reconciles so the enrolled campaigns become rows.

// maybeEnroll starts an enroll watch on the next whitelisted game (round-
// robin from enrollNext) that is off cooldown and has a usable channel.
// Called only when the planner said Idle and no enroll watch runs. Games
// probed within EnrollGameCooldown are skipped without I/O; a game whose
// lookup found nothing is skipped for LiveEvery, so an all-dark whitelist
// costs at most one directory call per game per LiveEvery.
func (l *Loop) maybeEnroll(ctx context.Context) {
	games := l.cfg.Games
	if !l.cfg.EnrollDiscovery || l.enroll != nil || l.authBlocked || len(games) == 0 {
		return
	}
	now := l.cfg.Now()
	n := len(games)
	for i := 0; i < n; i++ {
		idx := (l.enrollNext + i) % n
		g := games[idx]
		if g == "" {
			continue
		}
		k := strings.ToLower(g)
		if t, ok := l.enrollProbed[k]; ok && now.Sub(t) < l.cfg.EnrollGameCooldown {
			continue
		}
		if t, ok := l.enrollEmpty[k]; ok && now.Sub(t) < l.cfg.LiveEvery {
			continue
		}
		s, ok := l.enrollChannel(ctx, g, now)
		if !ok {
			l.enrollEmpty[k] = now
			continue
		}
		l.enrollNext = (idx + 1) % n
		l.startEnroll(ctx, g, idx, s)
		return
	}
}

// enrollChannel returns the top live drops-enabled channel for game that
// is not cooling down. It lists the channels of a synthetic open campaign
// (no ID, so no allow-list), which the Twitch backend answers from the
// DROPS_ENABLED game directory sorted by viewers.
func (l *Loop) enrollChannel(ctx context.Context, game string, now time.Time) (platform.Stream, bool) {
	streams, err := l.cfg.Backend.ListEligibleChannels(ctx, l.cfg.Session, platform.Campaign{Platform: l.cfg.Platform, Game: game})
	if err != nil {
		// WARN, not Debug: this is the only signal an operator gets that
		// enroll discovery can't see a game's directory. The caller
		// (maybeEnroll) backs the game off for LiveEvery on any failed
		// lookup (enrollEmpty), so this fires at most once per game per
		// backoff window, not on every idle nudge.
		slog.Warn("pipeline enroll: list channels failed", "account", l.cfg.AccountID, "game", game, "err", err)
		return platform.Stream{}, false
	}
	sort.SliceStable(streams, func(i, j int) bool { return streams[i].ViewerCount > streams[j].ViewerCount })
	for _, s := range streams {
		if s.Channel == "" || !s.DropsEnabled {
			continue
		}
		if until, ok := l.cooldowns[strings.ToLower(s.Channel)]; ok && now.Before(until) {
			continue
		}
		return s, true
	}
	return platform.Stream{}, false
}

// startEnroll runs a stall-free session on s and arms the EnrollWatch
// timer. No PubSub subscription: the watch serves no known drop.
func (l *Loop) startEnroll(ctx context.Context, game string, idx int, s platform.Stream) {
	l.enroll = &enrollRun{game: game, idx: idx, stream: s, started: l.cfg.Now()}
	l.runSession(ctx, s, nil, 0)
	l.stopEnrollTimer()
	l.enrollT = time.NewTimer(l.cfg.EnrollWatch)
	l.enrollC = l.enrollT.C
	slog.Info("pipeline enroll discovery", "kind", "discovery", "account", l.cfg.AccountID,
		"phase", "start", "game", game, "channel", s.Channel, "viewers", s.ViewerCount, "watch", l.cfg.EnrollWatch.String())
}

func (l *Loop) stopEnrollTimer() {
	if l.enrollT != nil {
		l.enrollT.Stop()
	}
	l.enrollT, l.enrollC = nil, nil
}

// endEnroll stops the enroll watch without marking its game probed (stream
// down, preempted, auth wall); the cursor rewinds so the game is retried.
func (l *Loop) endEnroll(reason string) {
	e := l.enroll
	if e == nil {
		return
	}
	l.stopEnrollTimer()
	l.haltSession()
	l.enroll = nil
	l.enrollNext = e.idx
	slog.Info("pipeline enroll discovery", "kind", "discovery", "account", l.cfg.AccountID,
		"phase", "stop", "game", e.game, "channel", e.stream.Channel, "reason", reason)
}

// finishEnroll ends an enroll watch whose EnrollWatch is up: stop the
// session, mark the game probed, then reconcile (Inventory now lists any
// campaign the watch enrolled the account in) and refresh channels so the
// replan that follows can mine what it found.
func (l *Loop) finishEnroll(ctx context.Context) {
	e := l.enroll
	l.enrollT, l.enrollC = nil, nil // already fired
	if e == nil {
		return
	}
	l.haltSession()
	l.enroll = nil
	l.enrollProbed[strings.ToLower(e.game)] = l.cfg.Now()
	before := make(map[string]bool, len(l.rows))
	for id := range l.rows {
		before[id] = true
	}
	l.reconcile(ctx)
	// campaigns_found/new_drops reflect reconcile alone (refreshLive only
	// probes channels for candidates already in l.rows, it adds no rows),
	// so this INFO line fires even when the reconcile itself trips
	// authBlocked (integrity wall) and refreshLive never runs. Without it,
	// an enroll watch that ends in an auth block would leave no
	// phase=stop reason=done line at all.
	newDrops := 0
	camps := map[string]bool{}
	for id, r := range l.rows {
		if !before[id] {
			newDrops++
			camps[r.CampaignID] = true
		}
	}
	slog.Info("pipeline enroll discovery", "kind", "discovery", "account", l.cfg.AccountID,
		"phase", "stop", "game", e.game, "channel", e.stream.Channel, "reason", "done",
		"campaigns_found", len(camps), "new_drops", newDrops)
	if l.authBlocked {
		return
	}
	l.refreshLive(ctx)
}
