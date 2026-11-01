package runner

import (
	"context"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// Raids: with the global follow raids setting on, a watch (drop or force) on
// a backend that is a platform.RaidJoiner listens for raids from its channel.
// A raid announced there is joined once per raid ID, after a random delay,
// only if the same watch is still live. Joining is a side task on a
// watch-scoped context, like the points claims: it never touches the beacon,
// progress or claims, and the runner keeps watching the channel it chose.
// When the raided stream ends, the stream-down push re-picks as usual.

// raidSeenFor is how long a raid ID stays in the dedupe set. Twitch repeats
// raid_update_v2 for one raid only while its countdown runs (about 90 s).
const raidSeenFor = time.Hour

var (
	// raidDelay is the pause before joining a raid, random in [5s, 20s).
	// A var so tests can shrink it.
	raidDelay = func() time.Duration {
		return 5*time.Second + rand.N(15*time.Second)
	}
	// raidTimeout bounds one join attempt.
	raidTimeout = 10 * time.Second
)

// raidWatch is the raid side task of one watch: the raided channel and the
// context its join goroutines run under.
type raidWatch struct {
	channelID string
	cancel    context.CancelFunc
	ctx       context.Context
	wg        sync.WaitGroup // join goroutines in flight
}

// stop cancels pending joins, waits for their goroutines and drops the raid
// topic. nil-safe.
func (w *raidWatch) stop(r *Runner) {
	if w == nil {
		return
	}
	w.cancel()
	w.wg.Wait()
	if rj, ok := r.cfg.Backend.(platform.RaidJoiner); ok {
		rj.UnsubscribeRaids(r.cfg.AccountID, w.channelID)
	}
}

// followRaids reads the follow raids setting now.
func (r *Runner) followRaids() bool {
	return r.cfg.FollowRaids != nil && r.cfg.FollowRaids()
}

// raidJoiner is the backend's raid joiner when follow raids is on, else ok
// is false.
func (r *Runner) raidJoiner() (platform.RaidJoiner, bool) {
	if !r.followRaids() {
		return nil, false
	}
	rj, ok := r.cfg.Backend.(platform.RaidJoiner)
	return rj, ok
}

// startRaids subscribes the live watch's channel to raids, scoped to a child
// of ctx. It replaces any raid task still running. A no-op when follow raids
// is off, the backend cannot join raids, or no watch is live on the channel.
func (r *Runner) startRaids(ctx context.Context, channelID string) {
	rj, ok := r.raidJoiner()
	if !ok || channelID == "" {
		return
	}
	rctx, cancel := context.WithCancel(ctx)
	w := &raidWatch{channelID: channelID, ctx: rctx, cancel: cancel}
	r.mu.Lock()
	if r.handle == nil || r.stream == nil || r.stream.ChannelID != channelID {
		r.mu.Unlock()
		cancel()
		return
	}
	prev := r.raids
	r.raids = w
	r.mu.Unlock()
	prev.stop(r) // before the subscribe, so a same-channel prev cannot drop it
	rj.SubscribeRaids(r.cfg.AccountID, channelID)
}

// stopRaids ends the running raid task, if any.
func (r *Runner) stopRaids() {
	r.mu.Lock()
	w := r.raids
	r.raids = nil
	r.mu.Unlock()
	w.stop(r)
}

// syncRaids starts or stops the live watch's raid task to match the follow
// raids setting now, keeping the watch. Run goroutine only.
func (r *Runner) syncRaids(ctx context.Context) {
	_, on := r.raidJoiner()
	r.mu.Lock()
	running := r.raids != nil
	channelID := ""
	if r.handle != nil && r.stream != nil {
		channelID = r.stream.ChannelID
	}
	r.mu.Unlock()
	switch {
	case on && !running && channelID != "":
		r.startRaids(ctx, channelID)
	case !on && running:
		r.stopRaids()
	}
}

// onRaid schedules one join of raidID when it comes from the channel the
// account is watching with follow raids on. A repeat of a raid ID already
// scheduled is ignored.
func (r *Runner) onRaid(channelID, raidID string) {
	if raidID == "" {
		return
	}
	rj, ok := r.raidJoiner()
	if !ok {
		return
	}
	now := time.Now()
	r.mu.Lock()
	w := r.raids
	if w == nil || w.channelID != channelID || w.ctx.Err() != nil {
		r.mu.Unlock()
		return
	}
	if _, dup := r.raidSeen[raidID]; dup {
		r.mu.Unlock()
		return
	}
	if r.raidSeen == nil {
		r.raidSeen = map[string]time.Time{}
	}
	for id, at := range r.raidSeen {
		if now.Sub(at) > raidSeenFor {
			delete(r.raidSeen, id)
		}
	}
	r.raidSeen[raidID] = now
	// Added under r.mu: stop takes r.raids under it first, so no join is
	// added after its Wait starts.
	w.wg.Add(1)
	r.mu.Unlock()
	go r.joinRaid(w, rj, raidID, raidDelay())
}

// joinRaid waits delay, then joins raidID if w is still the live watch's
// raid task on its channel and follow raids is still on.
func (r *Runner) joinRaid(w *raidWatch, rj platform.RaidJoiner, raidID string, delay time.Duration) {
	defer w.wg.Done()
	t := time.NewTimer(delay)
	defer t.Stop()
	select {
	case <-w.ctx.Done():
		return
	case <-t.C:
	}
	r.mu.Lock()
	still := r.raids == w && r.handle != nil && r.stream != nil && r.stream.ChannelID == w.channelID
	channel := ""
	if r.stream != nil {
		channel = r.stream.Channel
	}
	r.mu.Unlock()
	if !still || !r.followRaids() {
		return
	}
	ctx, cancel := context.WithTimeout(w.ctx, raidTimeout)
	defer cancel()
	if err := rj.JoinRaid(ctx, r.cfg.Session, raidID); err != nil {
		if w.ctx.Err() == nil {
			slog.Warn("runner join raid failed", "kind", "error", "account", r.cfg.AccountID,
				"channel", channel, "raid", raidID, "err", err)
		}
		return
	}
	slog.Info("runner joined raid", "kind", "info", "account", r.cfg.AccountID,
		"channel", channel, "raid", raidID)
}
