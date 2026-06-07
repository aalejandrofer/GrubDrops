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

	mu               sync.Mutex
	handleByAcc      map[string]string // accountID -> watch handle
	channelsByAcc    map[string][]string
	campaignChannels map[string][]kickChannel // campaignID -> eligible channels (slug+id)
}

var _ platform.Backend = (*Backend)(nil)

// New builds the Kick backend. The browser.Client may be nil (data flows over
// the utls HTTP client); it's kept for the interactive-login path only.
func New(c *browser.Client) *Backend {
	return &Backend{
		c:                c,
		api:              newAPI(),
		handleByAcc:      map[string]string{},
		channelsByAcc:    map[string][]string{},
		campaignChannels: map[string][]kickChannel{},
	}
}

// kickWatch is stored in WatchHandle.Internal; it owns the viewer-WS presence
// that accrues drops watch-time for the channel.
type kickWatch struct {
	conn *watchConn
}

func (b *Backend) Name() string { return "kick" }

// RegisterChannel stores a SINGLE channel for an account, replacing any
// existing list. Retained for backward compatibility with the
// dropsminer-helper CLI's one-channel flow; new code should call
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
	// Which campaigns the account is actually enrolled in (linked external
	// account). connect_url campaigns the account isn't participating in can't
	// earn — the watcher skips them so it doesn't burn time on PUBG/etc the
	// user never linked.
	participating, perr := b.api.ParticipatingCampaignIDs(ctx, s)
	if perr != nil {
		slog.Debug("kick participating fetch failed; treating none as linked", "err", perr)
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
		if c.ID != "" {
			b.mu.Lock()
			b.campaignChannels[c.ID] = c.Channels // slug+id, for the watch handshake
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
		// Connection gate: a campaign with a connect_url requires an external
		// account link; it's earnable only if the account is participating.
		// No connect_url = no link needed.
		if c.ConnectURL == "" {
			camp.AccountLinked = true
			camp.AccountLinkChecked = true
		} else {
			camp.AccountLinked = participating[c.ID]
			camp.AccountLinkChecked = true
			camp.AccountLinkURL = c.ConnectURL
		}
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
	// 1) Best: the campaign's own eligible channels (embedded in the campaigns
	// payload). Check each for liveness — only LIVE ones can accrue watch time,
	// and we need the livestream id for the watch ping. Cap the probes.
	const maxProbe = 12
	b.mu.Lock()
	chans := b.campaignChannels[c.ID]
	b.mu.Unlock()
	var live []platform.Stream
	probed := 0
	for _, ch := range chans {
		if probed >= maxProbe {
			break
		}
		probed++
		ok, _, viewers, category, err := b.api.ChannelLivestream(ctx, s, ch.Slug)
		if err != nil {
			slog.Debug("kick channel liveness check failed", "channel", ch.Slug, "err", err)
			continue
		}
		if !ok {
			continue
		}
		// Category gate: a campaign channel that's live but streaming a
		// DIFFERENT game accrues no drop progress. Skip it so the watcher
		// moves to a channel actually playing the campaign's game. Only
		// filter when both sides are known (unknown category falls
		// through — rare metadata gap).
		if c.Game != "" && category != "" && !strings.EqualFold(strings.TrimSpace(category), strings.TrimSpace(c.Game)) {
			slog.Debug("kick skip campaign channel on wrong category", "channel", ch.Slug, "streaming", category, "want", c.Game)
			continue
		}
		// ChannelID carries the CHANNEL id (for the viewer-WS handshake),
		// not the livestream id.
		live = append(live, platform.Stream{Channel: ch.Slug, ChannelID: ch.ID, ViewerCount: viewers, DropsEnabled: true})
	}
	if len(live) > 0 {
		return live, nil
	}
	// 2) Auto-discover live channels in the campaign's category (public).
	if slug := categorySlug(c.Game); slug != "" {
		if streams, err := b.api.DiscoverChannelsForCategory(ctx, s, slug); err == nil && len(streams) > 0 {
			return streams, nil
		} else if err != nil {
			slog.Debug("kick category discovery failed; falling back", "game", c.Game, "err", err)
		}
	}
	// 3) Fallback: any channels the operator registered manually.
	b.mu.Lock()
	chs := b.channelsByAcc[s.AccountID]
	b.mu.Unlock()
	out := make([]platform.Stream, 0, len(chs))
	for _, ch := range chs {
		out = append(out, platform.Stream{Channel: ch, DropsEnabled: true})
	}
	return out, nil
}

// categorySlug maps a campaign's game name to Kick's category slug for the
// public livestreams endpoint (e.g. "Rust" -> "rust").
func categorySlug(game string) string {
	g := strings.TrimSpace(strings.ToLower(game))
	if g == "" {
		return ""
	}
	return strings.ReplaceAll(g, " ", "-")
}

func (b *Backend) InventoryProgress(ctx context.Context, s platform.Session) ([]platform.Progress, error) {
	return b.api.Progress(ctx, s)
}

func (b *Backend) StartWatch(ctx context.Context, s platform.Session, stream platform.Stream) (platform.WatchHandle, error) {
	// Watching = a viewer-WS presence sending channel_handshake for the
	// channel (accrues drops watch-time). stream.ChannelID is the CHANNEL id.
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

func (b *Backend) Heartbeat(_ context.Context, h platform.WatchHandle) error {
	w, ok := h.Internal.(kickWatch)
	if !ok {
		return fmt.Errorf("kick: invalid watch handle")
	}
	// The viewer-WS writer goroutine sends channel_handshake + ping on its own
	// schedule; Heartbeat just confirms the presence is still alive so the
	// watcher swaps channels if it dropped.
	if !w.conn.Alive() {
		return fmt.Errorf("kick: viewer websocket closed for %q", h.Channel)
	}
	return nil
}

func (b *Backend) StopWatch(_ context.Context, h platform.WatchHandle) error {
	if w, ok := h.Internal.(kickWatch); ok {
		w.conn.Close()
	}
	return nil
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
