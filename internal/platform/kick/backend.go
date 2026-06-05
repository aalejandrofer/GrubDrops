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

// Backend implements platform.Backend for Kick by delegating page
// interactions to the browser sidecar over gRPC.
type Backend struct {
	c *browser.Client

	mu            sync.Mutex
	handleByAcc   map[string]string // accountID -> watch handle
	channelsByAcc map[string][]string
}

var _ platform.Backend = (*Backend)(nil)

// New requires a connected browser.Client. Caller manages its lifecycle.
func New(c *browser.Client) *Backend {
	return &Backend{
		c:             c,
		handleByAcc:   map[string]string{},
		channelsByAcc: map[string][]string{},
	}
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
	// Game-agnostic discovery: scrape https://kick.com/drops via the
	// sidecar and surface every active campaign Kick advertises. The
	// per-account whitelist filters the result — the backend never
	// hardcodes a game name.
	if b == nil || b.c == nil {
		return nil, nil
	}
	ks, err := decodeSession(s)
	if err != nil {
		return nil, err
	}
	camps, err := b.c.KickScrapeActiveDrops(ctx, s.AccountID, toProto(ks))
	if err != nil {
		// Cloudflare hard-blocks demote to soft-failure: return empty
		// list so the watcher sees "no campaigns this cycle" and sleeps
		// instead of looping into pick_campaign retries every 5 min.
		// The error stays in logs at scrape time so we can still observe
		// the block; here we just don't propagate it up.
		if strings.Contains(err.Error(), "cloudflare blocked") {
			slog.Warn("kick scrape blocked by cloudflare; treating as no campaigns this cycle", "account", s.AccountID)
			return nil, nil
		}
		return nil, fmt.Errorf("kick scrape drops: %w", err)
	}
	out := make([]platform.Campaign, 0, len(camps))
	for _, c := range camps {
		if s.GameFilter != nil && !s.GameFilter(c.Game) {
			continue
		}
		benefits := make([]platform.DropBenefit, 0, len(c.Benefits))
		for _, ben := range c.Benefits {
			required := int(ben.RequiredMinutes)
			if required <= 0 {
				required = 120 // Kick drops typically require ~2h
			}
			benefits = append(benefits, platform.DropBenefit{
				ID:              ben.Id,
				CampaignID:      c.Id,
				Name:            ben.Name,
				RequiredMinutes: required,
				ImageURL:        ben.ImageUrl,
			})
		}
		camp := platform.Campaign{
			ID:       c.Id,
			Platform: "kick",
			Game:     c.Game,
			Name:     c.Name,
			Status:   c.Status,
			Benefits: benefits,
		}
		if c.StartsAt > 0 {
			camp.StartsAt = time.Unix(c.StartsAt, 0).UTC()
		}
		if c.EndsAt > 0 {
			camp.EndsAt = time.Unix(c.EndsAt, 0).UTC()
		}
		if camp.Status == "" {
			camp.Status = "active"
		}
		out = append(out, camp)
	}
	return out, nil
}

func (b *Backend) ListEligibleChannels(_ context.Context, s platform.Session, _ platform.Campaign) ([]platform.Stream, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	// Scope channels to the calling session's account. Returning every
	// account's channels would let watcher A pick a stream that only
	// account B has been authenticated for — Kick rejects watches on
	// such mismatched sessions, and the heartbeat dies silently.
	chs := b.channelsByAcc[s.AccountID]
	out := make([]platform.Stream, 0, len(chs))
	for _, ch := range chs {
		out = append(out, platform.Stream{Channel: ch, DropsEnabled: true})
	}
	return out, nil
}

func (b *Backend) InventoryProgress(ctx context.Context, s platform.Session) ([]platform.Progress, error) {
	ks, err := decodeSession(s)
	if err != nil {
		return nil, err
	}
	drops, err := b.c.Inventory(ctx, toProto(ks))
	if err != nil {
		return nil, err
	}
	out := make([]platform.Progress, 0, len(drops))
	for _, d := range drops {
		out = append(out, platform.Progress{
			BenefitID:      d.BenefitId,
			MinutesWatched: int(d.MinutesWatched),
			Claimed:        d.Claimed,
		})
	}
	return out, nil
}

func (b *Backend) StartWatch(ctx context.Context, s platform.Session, stream platform.Stream) (platform.WatchHandle, error) {
	ks, err := decodeSession(s)
	if err != nil {
		return platform.WatchHandle{}, err
	}
	handle, err := b.c.StartWatch(ctx, toProto(ks), stream.Channel)
	if err != nil {
		return platform.WatchHandle{}, err
	}
	return platform.WatchHandle{Channel: stream.Channel, Internal: handle}, nil
}

func (b *Backend) Heartbeat(ctx context.Context, h platform.WatchHandle) error {
	handle, _ := h.Internal.(string)
	alive, err := b.c.Heartbeat(ctx, handle)
	if err != nil {
		return err
	}
	if !alive {
		return fmt.Errorf("kick: watch tab %q died", handle)
	}
	return nil
}

func (b *Backend) StopWatch(ctx context.Context, h platform.WatchHandle) error {
	handle, _ := h.Internal.(string)
	return b.c.StopWatch(ctx, handle)
}

func (b *Backend) Claim(ctx context.Context, s platform.Session, drop platform.DropBenefit) error {
	ks, err := decodeSession(s)
	if err != nil {
		return err
	}
	_, err = b.c.Claim(ctx, toProto(ks), drop.ID)
	return err
}
