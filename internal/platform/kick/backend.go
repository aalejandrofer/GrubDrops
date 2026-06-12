package kick

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/auth/browser"
	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// Backend implements platform.Backend for Kick over a pure-HTTP utls client
// that mimics a real Chrome TLS/HTTP2 fingerprint. Kick's API 403s any
// CDP-driven browser (chromedp included) but accepts this fingerprint, so the
// chromedp sidecar is no longer used for Kick data (see
// project_kick_breakthrough_utls memory / kick_issues.md). The browser.Client
// is retained only for the legacy interactive-login path.
type Backend struct {
	c   *browser.Client
	api *api

	// browserWatch, when true AND c != nil, routes StartWatch/Heartbeat/
	// StopWatch to the chromedp sidecar so a real IVS <video> session
	// accrues drop watch-time. The pure-HTTP viewer-WS (openWatch) does
	// NOT accrue (proven 2026-06-12), so this is the only credit-earning
	// watch path. All other calls (campaigns/progress/claim/discovery)
	// stay on the utls HTTP transport regardless. When false, the
	// non-accruing WS path is used (kept as a fallback / for envs with no
	// sidecar).
	browserWatch bool

	mu               sync.Mutex
	handleByAcc      map[string]string // accountID -> watch handle
	channelsByAcc    map[string][]string
	campaignChannels map[string][]kickChannel // campaignID -> eligible channels (slug+id)
	categoryChannels map[string][]kickChannel // game/category -> union of participating channels across campaigns
}

var _ platform.Backend = (*Backend)(nil)

// New builds the Kick backend. The browser.Client may be nil (data flows over
// the utls HTTP client); it's kept for the interactive-login path and, when
// EnableBrowserWatch is set, for the credit-earning IVS watch path.
func New(c *browser.Client) *Backend {
	return &Backend{
		c:                c,
		api:              newAPI(),
		handleByAcc:      map[string]string{},
		channelsByAcc:    map[string][]string{},
		campaignChannels: map[string][]kickChannel{},
		categoryChannels: map[string][]kickChannel{},
	}
}

// EnableBrowserWatch switches StartWatch/Heartbeat/StopWatch onto the
// chromedp sidecar (a real, playing IVS <video> per watch — the only path
// that accrues Kick drop watch-time). No-op without a sidecar client: the
// backend logs and stays on the (non-accruing) viewer-WS path so it never
// silently drops to a broken watch. Call once at construction, before the
// watcher starts.
func (b *Backend) EnableBrowserWatch() {
	if b.c == nil {
		slog.Warn("kick: browser-watch requested but no sidecar client configured; staying on (non-accruing) viewer-WS")
		return
	}
	b.browserWatch = true
}

// kickWatch is stored in WatchHandle.Internal for the (legacy, non-accruing)
// viewer-WS presence path.
type kickWatch struct {
	conn *watchConn
}

// kickBrowserWatch is stored in WatchHandle.Internal for the sidecar
// IVS-video watch path. It carries the opaque sidecar tab handle so
// Heartbeat/StopWatch can target the same browser tab.
type kickBrowserWatch struct {
	handle string
}

func (b *Backend) Name() string { return "kick" }

// RegisterChannel stores a SINGLE channel for an account, replacing any
// existing list. Retained for backward compatibility with the
// grubdrops-helper CLI's one-channel flow; new code should call
// RegisterChannels.
func (b *Backend) RegisterChannel(accountID, channel string) {
	if channel == "" {
		b.RegisterChannels(accountID, nil)
		return
	}
	b.RegisterChannels(accountID, []string{channel})
}

// RegisterChannels stores the full channel list an account wants to
// mine. Replaces any previous list. Duplicate entries are deduplicated;
// empty strings dropped. Pass nil/empty to unregister the account.
func (b *Backend) RegisterChannels(accountID string, channels []string) {
	cleaned := dedupeChannels(channels)
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(cleaned) == 0 {
		delete(b.channelsByAcc, accountID)
		return
	}
	b.channelsByAcc[accountID] = cleaned
}

// Channels returns the registered channel list for an account. Returns
// nil when none registered. Caller must not mutate the returned slice.
func (b *Backend) Channels(accountID string) []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	chs := b.channelsByAcc[accountID]
	if len(chs) == 0 {
		return nil
	}
	out := make([]string, len(chs))
	copy(out, chs)
	return out
}

// AllowedChannelCount returns the number of distinct channels currently
// registered across all accounts. Kick discovery doesn't surface a
// per-campaign allow-list, so the campaignID argument is ignored and
// the dashboard treats the result as the campaign-wide eligible channel
// count.
func (b *Backend) AllowedChannelCount(_ string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	seen := make(map[string]struct{})
	for _, chs := range b.channelsByAcc {
		for _, ch := range chs {
			if ch == "" {
				continue
			}
			seen[ch] = struct{}{}
		}
	}
	return len(seen)
}

func dedupeChannels(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, ch := range in {
		ch = strings.TrimSpace(ch)
		if ch == "" {
			continue
		}
		key := strings.ToLower(ch)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, ch)
	}
	return out
}

func (b *Backend) StartDeviceLogin(_ context.Context) (platform.DeviceChallenge, error) {
	return platform.DeviceChallenge{}, errors.New("kick: use cookie-paste login")
}

func (b *Backend) PollDeviceLogin(_ context.Context, _ platform.DeviceChallenge) (platform.Session, error) {
	return platform.Session{}, errors.New("kick: use cookie-paste login")
}

func (b *Backend) LoginViaBrowser(_ context.Context, rpc platform.BrowserRPC) (platform.Session, error) {
	return rpc.LoginInteractive("kick")
}

func (b *Backend) RefreshSession(_ context.Context, s platform.Session) (platform.Session, error) {
	// Kick cookies don't refresh server-side. Return unchanged — invalid
	// sessions surface as 401s on the next API call.
	return s, nil
}

func (b *Backend) ListActiveCampaigns(ctx context.Context, s platform.Session) ([]platform.Campaign, error) {
	// Drops campaigns over the utls HTTP client (GET /api/v1/drops/campaigns).
	// The per-account whitelist filters the result — never hardcode a game.
	camps, err := b.api.Campaigns(ctx, s)
	if err != nil {
		return nil, fmt.Errorf("kick campaigns: %w", err)
	}
	out := make([]platform.Campaign, 0, len(camps))
	for _, c := range camps {
		if s.GameFilter != nil && c.Game != "" && !s.GameFilter(c.Game) {
			continue
		}
		benefits := make([]platform.DropBenefit, 0, len(c.Rewards))
		for _, ben := range c.Rewards {
			required := ben.RequiredMinutes
			if required <= 0 {
				required = 120 // Kick drops typically require ~2h
			}
			benefits = append(benefits, platform.DropBenefit{
				ID:              ben.ID,
				CampaignID:      c.ID,
				Name:            ben.Name,
				RequiredMinutes: required,
				ImageURL:        ben.ImageURL,
			})
		}
		slugs := make([]string, 0, len(c.Channels))
		for _, ch := range c.Channels {
			slugs = append(slugs, ch.Slug)
		}
		if c.ID != "" && len(c.Channels) > 0 {
			b.mu.Lock()
			b.campaignChannels[c.ID] = c.Channels // slug+id, for the watch handshake
			// Kick drops accrue on ANY participating live channel in the
			// campaign's category, so pool every campaign's channels by game.
			// Open campaigns (channels: []) borrow this pool — that's how the
			// daemon finds a live Rust channel for "Kick Off 2" et al.
			if c.Game != "" {
				b.categoryChannels[c.Game] = mergeChannels(b.categoryChannels[c.Game], c.Channels)
			}
			b.mu.Unlock()
		}
		camp := platform.Campaign{
			ID:              c.ID,
			Platform:        "kick",
			Game:            c.Game,
			Name:            c.Name,
			Benefits:        benefits,
			AllowedChannels: slugs,
		}
		// Kick gives no per-campaign "is the external account linked" signal
		// (unlike Twitch). Inferring it from /drops/progress deadlocks: progress
		// only appears after watch time, watch time was blocked until "linked",
		// "linked" was read from progress — so a freshly-linked campaign stayed
		// "unlinked" forever. So assume linked and mine; just surface the
		// connect_url so the user can link manually if a drop never progresses.
		camp.AccountLinked = true
		camp.AccountLinkChecked = true
		camp.AccountLinkURL = c.ConnectURL
		// Parse RFC3339 start/end so the /drops past|current|upcoming tabs work.
		if t, err := time.Parse(time.RFC3339, c.StartsAt); err == nil {
			camp.StartsAt = t.UTC()
		}
		if t, err := time.Parse(time.RFC3339, c.EndsAt); err == nil {
			camp.EndsAt = t.UTC()
		}
		// Normalise status to active|upcoming|expired (Kick also supports
		// upcoming campaigns). Trust explicit "expired"; derive the rest from
		// the window so the watcher only mines truly-active ones.
		now := time.Now()
		switch {
		case strings.EqualFold(c.Status, "expired"), !camp.EndsAt.IsZero() && camp.EndsAt.Before(now):
			camp.Status = "expired"
		case !camp.StartsAt.IsZero() && camp.StartsAt.After(now):
			camp.Status = "upcoming"
		default:
			camp.Status = "active"
		}
		out = append(out, camp)
	}
	return out, nil
}

func (b *Backend) ListEligibleChannels(ctx context.Context, s platform.Session, c platform.Campaign) ([]platform.Stream, error) {
	// Candidate pool:
	//  - Restricted campaign (has its own channels, e.g. "Team Oilrats"): ONLY
	//    those channels can accrue, so use them.
	//  - Open campaign (channels: []): Kick drops accrue on ANY participating
	//    live channel in the same category, so use the category-wide union we
	//    pooled across all campaigns in ListActiveCampaigns.
	// Every candidate is verified LIVE + actually streaming the campaign's
	// category before we commit (a live channel on a DIFFERENT game accrues
	// nothing; an offline one can't be watched). This replaces the old generic
	// /stream/livestreams feed, which ignored the category slug and returned
	// unrelated games (smite, slots, …) — the bot would watch them for nothing.
	b.mu.Lock()
	pool := append([]kickChannel(nil), b.campaignChannels[c.ID]...)
	if len(pool) == 0 {
		pool = append(pool, b.categoryChannels[c.Game]...)
	}
	b.mu.Unlock()

	if live := b.probeLive(ctx, s, c, pool); len(live) > 0 {
		return live, nil
	}

	// Fallback: channels the operator registered manually. Returned as-is (no
	// liveness probe) — an explicit operator override, and the watch loop will
	// drop a dead one on the next heartbeat.
	b.mu.Lock()
	manual := b.channelsByAcc[s.AccountID]
	b.mu.Unlock()
	out := make([]platform.Stream, 0, len(manual))
	for _, ch := range manual {
		out = append(out, platform.Stream{Channel: ch, DropsEnabled: true})
	}
	return out, nil
}

// probeLive checks each candidate channel's livestream and returns the ones
// that are LIVE and streaming the campaign's category. Caps the number of
// network probes. ChannelLivestream returns the channel id when known (campaign
// channels carry it; bare slugs fall back to the livestream id, which the watch
// handshake also accepts).
func (b *Backend) probeLive(ctx context.Context, s platform.Session, c platform.Campaign, pool []kickChannel) []platform.Stream {
	const maxProbe = 12
	var live []platform.Stream
	for i, ch := range pool {
		if i >= maxProbe {
			break
		}
		ok, lsID, viewers, category, err := b.api.ChannelLivestream(ctx, s, ch.Slug)
		if err != nil {
			slog.Debug("kick channel liveness check failed", "channel", ch.Slug, "err", err)
			continue
		}
		if !ok {
			continue
		}
		if c.Game != "" && category != "" && !strings.EqualFold(strings.TrimSpace(category), strings.TrimSpace(c.Game)) {
			slog.Debug("kick skip channel on wrong category", "channel", ch.Slug, "streaming", category, "want", c.Game)
			continue
		}
		id := ch.ID
		if id == "" {
			id = lsID // bare slug: best-effort, watch handshake accepts the livestream id
		}
		live = append(live, platform.Stream{Channel: ch.Slug, ChannelID: id, ViewerCount: viewers, DropsEnabled: true})
	}
	return live
}

// mergeChannels appends src to dst, deduping by lowercased slug.
func mergeChannels(dst, src []kickChannel) []kickChannel {
	seen := make(map[string]struct{}, len(dst))
	for _, c := range dst {
		seen[strings.ToLower(c.Slug)] = struct{}{}
	}
	for _, c := range src {
		k := strings.ToLower(c.Slug)
		if c.Slug == "" {
			continue
		}
		if _, dup := seen[k]; dup {
			continue
		}
		seen[k] = struct{}{}
		dst = append(dst, c)
	}
	return dst
}

func (b *Backend) InventoryProgress(ctx context.Context, s platform.Session) ([]platform.Progress, error) {
	return b.api.Progress(ctx, s)
}

func (b *Backend) StartWatch(ctx context.Context, s platform.Session, stream platform.Stream) (platform.WatchHandle, error) {
	// Browser-watch path: drive a real, playing IVS <video> in the sidecar
	// — the ONLY path that accrues Kick drop watch-time. The sidecar needs
	// the channel SLUG (it navigates kick.com/<slug>) plus the session
	// cookies, which it injects before navigation.
	if b.browserWatch && b.c != nil {
		ks, err := decodeSession(s)
		if err != nil {
			return platform.WatchHandle{}, fmt.Errorf("kick start watch: decode session: %w", err)
		}
		handle, err := b.c.StartWatch(ctx, toProto(ks), stream.Channel)
		if err != nil {
			return platform.WatchHandle{}, fmt.Errorf("kick start watch (browser) %s: %w", stream.Channel, err)
		}
		return platform.WatchHandle{Channel: stream.Channel, Internal: kickBrowserWatch{handle: handle}}, nil
	}

	// Legacy viewer-WS presence path (NON-ACCRUING — kept only as a
	// fallback when no sidecar is configured). stream.ChannelID is the
	// CHANNEL id used in the channel_handshake.
	cookieHeader := cookieHeaderForSession(s)
	if cookieHeader == "" {
		return platform.WatchHandle{}, fmt.Errorf("kick: session has no cookies")
	}
	wc, err := openWatch(ctx, cookieHeader, stream.ChannelID)
	if err != nil {
		return platform.WatchHandle{}, fmt.Errorf("kick start watch %s: %w", stream.Channel, err)
	}
	return platform.WatchHandle{Channel: stream.Channel, Internal: kickWatch{conn: wc}}, nil
}

func (b *Backend) Heartbeat(ctx context.Context, h platform.WatchHandle) error {
	switch w := h.Internal.(type) {
	case kickBrowserWatch:
		alive, err := b.c.Heartbeat(ctx, w.handle)
		if err != nil {
			return fmt.Errorf("kick heartbeat (browser) %q: %w", h.Channel, err)
		}
		if !alive {
			return fmt.Errorf("kick: browser watch not playing for %q", h.Channel)
		}
		return nil
	case kickWatch:
		// The viewer-WS writer goroutine sends channel_handshake + ping on its
		// own schedule; Heartbeat just confirms the presence is still alive so
		// the watcher swaps channels if it dropped.
		if !w.conn.Alive() {
			return fmt.Errorf("kick: viewer websocket closed for %q", h.Channel)
		}
		return nil
	default:
		return fmt.Errorf("kick: invalid watch handle")
	}
}

func (b *Backend) StopWatch(ctx context.Context, h platform.WatchHandle) error {
	switch w := h.Internal.(type) {
	case kickBrowserWatch:
		if err := b.c.StopWatch(ctx, w.handle); err != nil {
			return fmt.Errorf("kick stop watch (browser) %q: %w", h.Channel, err)
		}
		return nil
	case kickWatch:
		w.conn.Close()
		return nil
	default:
		return nil
	}
}

func (b *Backend) Claim(ctx context.Context, s platform.Session, drop platform.DropBenefit) error {
	// Kick claim needs reward_id + campaign_id. drop.ID is the reward id;
	// drop.CampaignID is the campaign.
	return b.api.Claim(ctx, s, drop.ID, drop.CampaignID)
}

// FetchImage proxies a Kick CDN asset over the utls transport so the
// browser can render it (files.kick.com 403s direct hotlinks). Returns
// the bytes + Content-Type + upstream status.
func (b *Backend) FetchImage(ctx context.Context, rawURL string) ([]byte, string, int, error) {
	return b.api.FetchImage(ctx, rawURL)
}

// VerifyAuth probes the Kick session by fetching the drops campaigns over
// the authed utls transport. A hard failure (dead cookies / CF block /
// expired session) surfaces as an error. Satisfies platform.AuthChecker.
func (b *Backend) VerifyAuth(ctx context.Context, s platform.Session) error {
	if _, err := b.api.Campaigns(ctx, s); err != nil {
		return fmt.Errorf("kick campaigns probe: %w", err)
	}
	return nil
}

var _ platform.AuthChecker = (*Backend)(nil)
