package sidecar

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand"
	"sync"
	"time"

	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"

	pb "github.com/aalejandrofer/grubdrops/internal/auth/browser/gen/browser/v1"
)

// Kick credits drop watch-time ONLY for a real, actively-playing IVS
// video session WITH periodic viewer activity (proven: a CDP-driven
// Chrome playing kick.com/<channel> with synthetic mouse activity accrued
// ~real-time; the same player with NO activity went flat — anti-AFK; the
// pure-HTTP utls viewer-WS does NOT accrue at all). This file drives a
// headless Chrome tab through the player AND simulates a viewer moving the
// mouse over it so watch-time accrues.
//
// Resource notes: one video-playing tab per active watch. We mute it and
// pin the lowest available quality to keep CPU/bandwidth down. Because
// watching ANY live channel in a campaign's category accrues ALL
// same-category open campaigns at once, the caller should keep at most
// one watch tab per Kick account.

const (
	// kickWatchSettleWait is how long we give the SPA + IVS player to
	// mount the <video> element after navigation before driving it.
	kickWatchSettleWait = 8 * time.Second
	// kickWatchKeepAliveEvery is the cadence of the background nudge that
	// un-pauses / re-mutes the player if Kick's UI or an ad break paused
	// it, AND dispatches a trusted mouse-move to defeat the anti-AFK
	// gate. Kick accrues per-minute, so a sub-minute cadence is plenty;
	// ~15-20s keeps us comfortably inside any idle window.
	kickWatchKeepAliveEvery = 17 * time.Second
)

// watchState tracks the last observed <video> currentTime per handle so
// WatchAlive can tell "playing and advancing" from "claims playing but
// stalled/looping a buffer" (e.g. the stream went offline mid-watch and
// the player froze on the last frame without flipping paused/ended).
type watchState struct {
	mu       sync.Mutex
	lastTime float64
	lastSeen time.Time
	// stalls counts consecutive liveness probes where currentTime did not
	// advance. One stalled probe can be a transient buffer hiccup; several
	// in a row means the video is genuinely not advancing.
	stalls int
}

// kickPlayerDriveScript locates the IVS <video>, mutes it, sets the
// lowest quality if a quality menu is reachable, and calls play(). It is
// idempotent and safe to run repeatedly (used for both the initial drive
// and the keep-alive nudge). Returns a small JSON status string for
// logging/liveness. Pure DOM/JS — no element-selector brittleness beyond
// the <video> tag itself, which IVS always renders.
const kickPlayerDriveScript = `(() => {
  const out = {video: false, playing: false, muted: false, readyState: -1, currentTime: 0};
  try {
    const v = document.querySelector('video');
    if (!v) return JSON.stringify(out);
    out.video = true;
    // Mute first so autoplay policies don't block play().
    try { v.muted = true; v.volume = 0; } catch (e) {}
    out.muted = !!v.muted;
    out.readyState = v.readyState;
    out.currentTime = v.currentTime || 0;
    if (v.paused) {
      const p = v.play();
      if (p && typeof p.catch === 'function') { p.catch(() => {}); }
    }
    out.playing = !v.paused && !v.ended && v.readyState >= 2;
    // Best-effort lowest-quality: Kick exposes a settings/quality gear in
    // its custom player. We can't reliably click it headless, but if the
    // page wires a global hook we use it. Most builds don't, so this is a
    // no-op fallback — the muted low-volume tab is already cheap.
    try {
      if (window.__kickPlayer && typeof window.__kickPlayer.setQuality === 'function') {
        window.__kickPlayer.setQuality('lowest');
      }
    } catch (e) {}
  } catch (e) {
    out.err = String(e);
  }
  return JSON.stringify(out);
})()`

// OpenStreamWatch opens kick.com/<channel> in a fresh tab with the
// account's cookies injected, settles past Cloudflare, drives the IVS
// player to muted/playing, and starts a keep-alive goroutine that nudges
// the player AND simulates viewer mouse activity every
// kickWatchKeepAliveEvery. Returns the tab handle (used as the watch id)
// so Heartbeat/StopWatch can target it.
//
// This supersedes the fire-and-forget OpenStream for the browser-watch
// path. OpenStream is retained for callers that only need a passive tab.
func (k *Kick) OpenStreamWatch(channel string, session *pb.KickSession) (string, error) {
	if channel == "" {
		return "", fmt.Errorf("kick watch: empty channel")
	}
	handle, ctx, err := k.b.OpenTab()
	if err != nil {
		return "", err
	}
	// Install stealth + cookies before navigation.
	if err := chromedp.Run(ctx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			_, err := page.AddScriptToEvaluateOnNewDocument(StealthScript).Do(ctx)
			return err
		}),
	); err != nil {
		k.b.CloseTab(handle)
		return "", fmt.Errorf("kick watch install stealth: %w", err)
	}
	if err := k.InstallCookies(ctx, session); err != nil {
		k.b.CloseTab(handle)
		return "", fmt.Errorf("kick watch install cookies: %w", err)
	}
	if err := chromedp.Run(ctx,
		chromedp.Navigate(fmt.Sprintf("https://kick.com/%s", channel)),
		chromedp.Sleep(3*time.Second),
	); err != nil {
		k.b.CloseTab(handle)
		return "", fmt.Errorf("kick watch navigate %s: %w", channel, err)
	}
	if err := waitCloudflareSettled(ctx, 20*time.Second); err != nil {
		slog.Warn("kick watch: cloudflare interstitial did not settle", "channel", channel, "err", err.Error())
	}
	// Let the SPA mount the player, then drive it.
	var status string
	if err := chromedp.Run(ctx,
		chromedp.Sleep(kickWatchSettleWait),
		chromedp.Evaluate(kickPlayerDriveScript, &status),
	); err != nil {
		k.b.CloseTab(handle)
		return "", fmt.Errorf("kick watch drive player %s: %w", channel, err)
	}
	slog.Info("kick watch opened", "channel", channel, "handle", handle, "player", status)

	// Register liveness state for this handle so WatchAlive can detect a
	// stalled (non-advancing) <video>.
	k.mu.Lock()
	if k.watches == nil {
		k.watches = map[string]*watchState{}
	}
	k.watches[handle] = &watchState{lastSeen: time.Now()}
	k.mu.Unlock()

	// Keep-alive: re-drive the player + simulate viewer activity on an
	// interval so (a) an ad break, a stall, or Kick's UI pausing on
	// tab-blur doesn't silently stop accrual, and (b) the anti-AFK gate
	// keeps crediting. Bound to the tab context so it exits when the tab
	// closes.
	go k.watchKeepAlive(ctx, channel, handle)
	return handle, nil
}

// watchKeepAlive periodically re-runs the player drive script AND
// dispatches a trusted CDP mouse-move over the player area for an open
// watch tab. Exits when the tab context is cancelled (StopWatch / browser
// close) or when the tab can no longer be driven.
func (k *Kick) watchKeepAlive(ctx context.Context, channel, handle string) {
	t := time.NewTicker(kickWatchKeepAliveEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			// Tab may have been closed by StopWatch between ticks.
			if _, ok := k.b.Tab(handle); !ok {
				return
			}
			var status string
			driveCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := chromedp.Run(driveCtx,
				chromedp.Evaluate(kickPlayerDriveScript, &status),
				kickSimulateActivity(),
			)
			cancel()
			if err != nil {
				slog.Debug("kick watch keepalive drive failed", "channel", channel, "handle", handle, "err", err)
				continue
			}
			slog.Debug("kick watch keepalive", "channel", channel, "handle", handle, "player", status)
		}
	}
}

// kickSimulateActivity dispatches a couple of TRUSTED mouse-move events
// over the player region via CDP Input.dispatchMouseEvent. These are
// browser-level trusted events (event.isTrusted === true), unlike
// JS-synthetic dispatchEvent, so Kick's anti-AFK detection treats them as
// genuine viewer activity. Coordinates jitter within the top-left video
// area (the player fills the upper portion of the viewport on a channel
// page) to look organic.
func kickSimulateActivity() chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		// Two small moves a beat apart — a single point can be ignored as
		// noise; a short path reads as a real cursor nudge.
		x1 := 200 + rand.Float64()*400
		y1 := 150 + rand.Float64()*250
		x2 := x1 + (rand.Float64()*60 - 30)
		y2 := y1 + (rand.Float64()*60 - 30)
		if err := input.DispatchMouseEvent(input.MouseMoved, x1, y1).Do(ctx); err != nil {
			return err
		}
		select {
		case <-time.After(120 * time.Millisecond):
		case <-ctx.Done():
			return ctx.Err()
		}
		return input.DispatchMouseEvent(input.MouseMoved, x2, y2).Do(ctx)
	})
}

// StopWatch closes the watch tab and drops its liveness state.
func (k *Kick) StopWatch(handle string) {
	k.b.CloseTab(handle)
	k.mu.Lock()
	delete(k.watches, handle)
	k.mu.Unlock()
}

// WatchAlive reports whether the watch tab still exists AND its <video>
// element is actually PLAYING AND ADVANCING (not merely that the tab is
// open or that the player claims to be playing while frozen on a stale
// buffer). Used by the Heartbeat RPC so the watcher swaps channels when
// playback dies (stream ended, player errored, channel went offline)
// rather than holding a dead tab that accrues nothing.
func (k *Kick) WatchAlive(handle string) bool {
	ctx, ok := k.b.Tab(handle)
	if !ok {
		return false
	}
	var status string
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := chromedp.Run(probeCtx, chromedp.Evaluate(kickPlayerProbeScript, &status)); err != nil {
		// A failed probe doesn't necessarily mean the watch is dead (a
		// transient CDP hiccup), but the tab is unusable for accrual right
		// now — report not-alive so the watcher re-picks.
		slog.Debug("kick watch probe failed", "handle", handle, "err", err)
		return false
	}
	return k.evalWatchAlive(handle, status)
}

// kickPlayerProbeScript reports whether the IVS <video> is currently
// playing plus its currentTime so the caller can detect a stalled player.
// Read-only (no play()/mute side effects) so Heartbeat stays a pure
// check; the keep-alive goroutine owns the corrective nudges.
const kickPlayerProbeScript = `(() => {
  try {
    const v = document.querySelector('video');
    if (!v) return JSON.stringify({video: false, playing: false, currentTime: 0});
    const playing = !v.paused && !v.ended && v.readyState >= 2;
    return JSON.stringify({video: true, playing: playing, readyState: v.readyState, currentTime: v.currentTime || 0});
  } catch (e) {
    return JSON.stringify({video: false, playing: false, currentTime: 0, err: String(e)});
  }
})()`

// maxWatchStalls is how many consecutive non-advancing probes we tolerate
// before declaring the watch dead. One stall can be a normal buffer
// hiccup; a few in a row (over ~Heartbeat cadence, ~1/min) means the video
// is genuinely frozen — re-pick a channel.
const maxWatchStalls = 2

// evalWatchAlive interprets the probe JSON AND folds in per-handle
// progression: a player that reports playing but whose currentTime hasn't
// advanced across maxWatchStalls consecutive probes is treated as dead.
func (k *Kick) evalWatchAlive(handle, status string) bool {
	s := parseWatchProbe(status)
	if !s.Video || !s.Playing {
		return false
	}

	k.mu.Lock()
	if k.watches == nil {
		k.watches = map[string]*watchState{}
	}
	ws := k.watches[handle]
	if ws == nil {
		ws = &watchState{}
		k.watches[handle] = ws
	}
	k.mu.Unlock()

	ws.mu.Lock()
	defer ws.mu.Unlock()
	// First probe for this handle: seed the baseline, treat as alive.
	if ws.lastSeen.IsZero() {
		ws.lastTime = s.CurrentTime
		ws.lastSeen = time.Now()
		ws.stalls = 0
		return true
	}
	// currentTime advanced (allowing for tiny float noise) => healthy.
	if s.CurrentTime > ws.lastTime+0.05 {
		ws.lastTime = s.CurrentTime
		ws.lastSeen = time.Now()
		ws.stalls = 0
		return true
	}
	// Not advancing. Tolerate a few transient stalls; beyond that, dead.
	ws.stalls++
	ws.lastTime = s.CurrentTime
	ws.lastSeen = time.Now()
	if ws.stalls > maxWatchStalls {
		slog.Debug("kick watch stalled (currentTime not advancing)", "handle", handle, "stalls", ws.stalls, "currentTime", s.CurrentTime)
		return false
	}
	return true
}

type watchProbe struct {
	Video       bool    `json:"video"`
	Playing     bool    `json:"playing"`
	CurrentTime float64 `json:"currentTime"`
}

// parseWatchProbe decodes the probe script's JSON. A malformed/empty body
// yields a zero value (not video, not playing).
func parseWatchProbe(status string) watchProbe {
	var s watchProbe
	if status == "" {
		return s
	}
	if err := json.Unmarshal([]byte(status), &s); err != nil {
		return watchProbe{}
	}
	return s
}

// parseWatchAlive is the stateless interpretation used by tests and any
// caller that only cares whether the player reports video+playing (no
// progression check). "Alive" means the <video> exists and is playing.
func parseWatchAlive(status string) bool {
	s := parseWatchProbe(status)
	return s.Video && s.Playing
}
