// Package session runs one accrual session on one channel and reports what
// it sees. It makes no decisions.
package session

import (
	"context"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

type Kind int

const (
	Progress Kind = iota
	StreamDown
	Stalled
)

type Event struct {
	Kind     Kind
	Channel  string
	Progress []platform.Progress
	Err      error
	// Gen is the generation stamp of the session that produced this event
	// (Config.Gen, echoed verbatim). The loop bumps its generation on every
	// startSession and drops events whose Gen doesn't match the current one,
	// so a stale event queued by a session that already got stopped (e.g. on
	// a serves-set restart) can't be mistaken for one from the live session.
	Gen int
}

type Config struct {
	Backend platform.Backend
	Session platform.Session
	Stream  platform.Stream
	Serves  []string
	// Ticks drives beats. Production passes a 60s ticker: Twitch credits one
	// minute per beacon, so a slower cadence under-credits.
	Ticks      <-chan time.Time
	StallPolls int
	// Gen is stamped onto every Event this run produces. See Event.Gen.
	Gen int
}

// Run starts the watch, beats immediately and on every tick, and stops the
// watch when ctx ends or the stream goes down.
func Run(ctx context.Context, cfg Config, out chan<- Event) {
	ch := cfg.Stream.Channel
	send := func(e Event) {
		e.Channel = ch
		e.Gen = cfg.Gen
		select {
		case out <- e:
		case <-ctx.Done():
		}
	}
	h, err := cfg.Backend.StartWatch(ctx, cfg.Session, cfg.Stream)
	if err != nil {
		send(Event{Kind: StreamDown, Err: err})
		return
	}
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = cfg.Backend.StopWatch(stopCtx, h)
	}()

	serves := make(map[string]bool, len(cfg.Serves))
	for _, id := range cfg.Serves {
		serves[id] = true
	}
	best := make(map[string]int)
	still, stalled := 0, false
	beat := func() bool {
		if err := cfg.Backend.Heartbeat(ctx, h); err != nil {
			if ctx.Err() == nil {
				send(Event{Kind: StreamDown, Err: err})
			}
			return false
		}
		prog, err := cfg.Backend.InventoryProgress(ctx, cfg.Session)
		if err != nil {
			return true // transient; the next tick retries
		}
		send(Event{Kind: Progress, Progress: prog})
		gained := false
		for _, p := range prog {
			if serves[p.BenefitID] {
				// First time seeing this id counts as gain (baseline), or if it improved
				if _, seen := best[p.BenefitID]; !seen || p.MinutesWatched > best[p.BenefitID] {
					best[p.BenefitID] = p.MinutesWatched
					gained = true
				}
			}
		}
		if gained {
			still = 0
		} else {
			still++
		}
		if !stalled && cfg.StallPolls > 0 && still >= cfg.StallPolls {
			stalled = true
			send(Event{Kind: Stalled})
		}
		return true
	}
	if !beat() {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-cfg.Ticks:
			if !beat() {
				return
			}
		}
	}
}
