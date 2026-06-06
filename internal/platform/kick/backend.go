package kick

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/aalejandrofer/dropsminer/internal/auth/browser"
	"github.com/aalejandrofer/dropsminer/internal/platform"
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

	mu            sync.Mutex
	handleByAcc   map[string]string // accountID -> watch handle
	channelsByAcc map[string][]string
}

var _ platform.Backend = (*Backend)(nil)

// New builds the Kick backend. The browser.Client may be nil (data flows over
// the utls HTTP client); it's kept for the interactive-login path only.
func New(c *browser.Client) *Backend {
	return &Backend{
		c:             c,
		api:           newAPI(),
		handleByAcc:   map[string]string{},
		channelsByAcc: map[string][]string{},
	}
}

// kickWatch is stored in WatchHandle.Internal so Heartbeat can send the
// periodic view ping (Kick accrues watch time from POST /video/views/{id}).
type kickWatch struct {
	livestreamID string
	session      platform.Session
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
		camp := platform.Campaign{
			ID:              c.ID,
			Platform:        "kick",
			Game:            c.Game,
			Name:            c.Name,
			Benefits:        benefits,
			AllowedChannels: c.Channels,
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
	var live []platform.Stream
	probed := 0
	for _, slug := range c.AllowedChannels {
		if probed >= maxProbe {
			break
		}
		probed++
		ok, lsID, viewers, err := b.api.ChannelLivestream(ctx, s, slug)
		if err != nil {
			slog.Debug("kick channel liveness check failed", "channel", slug, "err", err)
			continue
		}
		if ok {
			live = append(live, platform.Stream{Channel: slug, ChannelID: lsID, ViewerCount: viewers, DropsEnabled: true})
		}
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

func (b *Backend) StartWatch(_ context.Context, s platform.Session, stream platform.Stream) (platform.WatchHandle, error) {
	// No browser tab. Watching = periodic POST /video/views/{livestreamID}
	// (sent by Heartbeat). stream.ChannelID carries the livestream id from
	// discovery. The first ping is sent on the next Heartbeat tick.
	return platform.WatchHandle{
		Channel:  stream.Channel,
		Internal: kickWatch{livestreamID: stream.ChannelID, session: s},
	}, nil
}

func (b *Backend) Heartbeat(_ context.Context, h platform.WatchHandle) error {
	// NOTE: Kick accrues drops watch-time via a VIEWER WEBSOCKET presence
	// (wss://websockets.kick.com/viewer/v1/connect), NOT an HTTP ping — the
	// /api/v1/video/views endpoint is VOD view-counting and 404s on live
	// streams. Until that websocket viewer client is implemented, Heartbeat is
	// a soft no-op so the watcher holds the channel without error-spamming.
	// TODO(kick-watch): connect the viewer websocket to actually accrue time.
	if _, ok := h.Internal.(kickWatch); !ok {
		return fmt.Errorf("kick: invalid watch handle")
	}
	return nil
}

func (b *Backend) StopWatch(_ context.Context, _ platform.WatchHandle) error {
	// Nothing to tear down — no tab, no persistent connection.
	return nil
}

func (b *Backend) Claim(ctx context.Context, s platform.Session, drop platform.DropBenefit) error {
	// Kick claim needs reward_id + campaign_id. drop.ID is the reward id;
	// drop.CampaignID is the campaign.
	return b.api.Claim(ctx, s, drop.ID, drop.CampaignID)
}
