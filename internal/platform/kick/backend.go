package kick

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/aalejandrofer/dropsminer/internal/auth/browser"
	"github.com/aalejandrofer/dropsminer/internal/platform"
)

// Backend implements platform.Backend for Kick by delegating page
// interactions to the browser sidecar over gRPC.
type Backend struct {
	c *browser.Client

	mu           sync.Mutex
	handleByAcc  map[string]string // accountID -> watch handle
	channelByAcc map[string]string
}

var _ platform.Backend = (*Backend)(nil)

// New requires a connected browser.Client. Caller manages its lifecycle.
func New(c *browser.Client) *Backend {
	return &Backend{
		c:            c,
		handleByAcc:  map[string]string{},
		channelByAcc: map[string]string{},
	}
}

func (b *Backend) Name() string { return "kick" }

// RegisterChannel stores the channel an account wants to mine. Called
// from the GUI login flow (Plan 4 Task 11) after the user submits
// cookies + channel together.
func (b *Backend) RegisterChannel(accountID, channel string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.channelByAcc[accountID] = channel
}

// AllowedChannelCount returns the number of distinct channels currently
// registered across all accounts. Kick discovery doesn't surface a
// per-campaign allow-list — each account picks a single channel — so
// the campaignID argument is ignored and the dashboard treats the
// result as the campaign-wide eligible channel count.
func (b *Backend) AllowedChannelCount(_ string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	seen := make(map[string]struct{}, len(b.channelByAcc))
	for _, ch := range b.channelByAcc {
		if ch == "" {
			continue
		}
		seen[ch] = struct{}{}
	}
	return len(seen)
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
	// Kick currently only surfaces Rust drops via the sidecar (no
	// per-game discovery API). Honor the per-account whitelist by
	// short-circuiting when "Rust" is not on the list — saves an
	// inventory roundtrip and keeps the whitelist canonical.
	const kickGame = "Rust"
	if s.GameFilter != nil && !s.GameFilter(kickGame) {
		return nil, nil
	}
	ks, err := decodeSession(s)
	if err != nil {
		return nil, err
	}
	drops, err := b.c.Inventory(ctx, toProto(ks))
	if err != nil {
		return nil, fmt.Errorf("kick inventory: %w", err)
	}
	if len(drops) == 0 {
		return nil, nil
	}
	benefits := make([]platform.DropBenefit, 0, len(drops))
	for _, d := range drops {
		benefits = append(benefits, platform.DropBenefit{
			ID:              d.BenefitId,
			CampaignID:      "kick-inventory",
			Name:            d.BenefitId,
			RequiredMinutes: 120, // Kick drops typically require 2h; refine when sidecar surfaces per-drop threshold
		})
	}
	return []platform.Campaign{{
		ID: "kick-inventory", Platform: "kick", Game: kickGame,
		Name: "Kick Rust Drops", Status: "active",
		Benefits: benefits,
	}}, nil
}

func (b *Backend) ListEligibleChannels(_ context.Context, _ platform.Session, _ platform.Campaign) ([]platform.Stream, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]platform.Stream, 0, len(b.channelByAcc))
	for _, ch := range b.channelByAcc {
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
