package twitch

import (
	"context"
	"errors"
	"sync"

	"github.com/aalejandrofer/dropsminer/internal/platform"
)

// Backend implements platform.Backend for Twitch using GraphQL persisted
// queries (mirrored from DevilXD/TwitchDropsMiner).
type Backend struct {
	c     *client
	auth  *authFlow
	disc  *discovery
	chans *channels
	watch *watch
	claim *claimer

	// allowedLoginsByCampaign caches the allow-list pulled from
	// dropCampaignDetails.allow.channels[].login. ListEligibleChannels
	// reads from this map.
	mu                      sync.Mutex
	allowedLoginsByCampaign map[string][]string
}

var _ platform.Backend = (*Backend)(nil)

func New() *Backend {
	c := newClient()
	return &Backend{
		c:                       c,
		auth:                    newAuthFlow(),
		disc:                    &discovery{c: c},
		chans:                   &channels{c: c},
		watch:                   newWatch(),
		claim:                   &claimer{c: c},
		allowedLoginsByCampaign: map[string][]string{},
	}
}

// newForTest builds a Backend pointed at a test endpoint. Used by tests
// that need to drive the whole interface against an httptest server.
func newForTest(endpoint string) *Backend {
	c := newTestClient(endpoint)
	return &Backend{
		c:                       c,
		auth:                    newAuthFlow(),
		disc:                    &discovery{c: c},
		chans:                   &channels{c: c},
		watch:                   &watch{c: c},
		claim:                   &claimer{c: c},
		allowedLoginsByCampaign: map[string][]string{},
	}
}

func (b *Backend) Name() string { return "twitch" }

func (b *Backend) StartDeviceLogin(ctx context.Context) (platform.DeviceChallenge, error) {
	return b.auth.start(ctx)
}

func (b *Backend) PollDeviceLogin(ctx context.Context, ch platform.DeviceChallenge) (platform.Session, error) {
	internal, ok := ch.Internal.(deviceInternal)
	if !ok {
		return platform.Session{}, errors.New("invalid challenge internal")
	}
	return b.auth.poll(ctx, internal)
}

func (b *Backend) LoginViaBrowser(_ context.Context, _ platform.BrowserRPC) (platform.Session, error) {
	return platform.Session{}, errors.New("not supported")
}

func (b *Backend) RefreshSession(ctx context.Context, s platform.Session) (platform.Session, error) {
	return b.auth.refresh(ctx, s)
}

func (b *Backend) ListActiveCampaigns(ctx context.Context, s platform.Session) ([]platform.Campaign, error) {
	camps, err := b.disc.listActive(ctx, s)
	if err != nil {
		return nil, err
	}
	// Drain the allow-lists captured as a side-effect of fetchDetails and
	// merge them into our cache. ListEligibleChannels reads from this map.
	allowed := b.disc.drainAllowed()
	b.mu.Lock()
	for cid, logins := range allowed {
		b.allowedLoginsByCampaign[cid] = logins
	}
	b.mu.Unlock()
	return camps, nil
}

func (b *Backend) ListEligibleChannels(ctx context.Context, s platform.Session, c platform.Campaign) ([]platform.Stream, error) {
	b.mu.Lock()
	allowed := b.allowedLoginsByCampaign[c.ID]
	b.mu.Unlock()
	if len(allowed) > 0 {
		return b.chans.listEligible(ctx, s, c, allowed)
	}
	// Empty allow-list = campaign accepts ANY live drops-enabled stream
	// of the game. Fall back to the DirectoryPage_Game query — without
	// this most public campaigns (Minecraft, Apex, etc) have nothing
	// to watch and the watcher sleeps forever.
	slug := slugify(c.Game)
	return b.chans.listForGameDirectory(ctx, s, slug)
}

// slugify converts a game's display name to its Twitch directory slug.
// Lowercase + spaces → dashes + drop apostrophes/periods. Good enough
// for the popular cases (Minecraft → "minecraft", "Apex Legends" →
// "apex-legends", "Counter-Strike 2" → "counter-strike-2"). Per-game
// special-casing can be added when the heuristic misses.
func slugify(name string) string {
	if name == "" {
		return ""
	}
	out := make([]byte, 0, len(name))
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'A' && c <= 'Z':
			out = append(out, c+32)
		case c >= 'a' && c <= 'z':
			out = append(out, c)
		case c >= '0' && c <= '9':
			out = append(out, c)
		case c == ' ' || c == '-' || c == '_':
			if len(out) == 0 || out[len(out)-1] != '-' {
				out = append(out, '-')
			}
		}
		// drop everything else (apostrophes, periods, colons, etc.)
	}
	for len(out) > 0 && out[len(out)-1] == '-' {
		out = out[:len(out)-1]
	}
	return string(out)
}

func (b *Backend) InventoryProgress(ctx context.Context, s platform.Session) ([]platform.Progress, error) {
	return b.disc.inventory(ctx, s)
}

func (b *Backend) StartWatch(ctx context.Context, s platform.Session, stream platform.Stream) (platform.WatchHandle, error) {
	return b.watch.start(ctx, s, stream)
}

func (b *Backend) Heartbeat(ctx context.Context, h platform.WatchHandle) error {
	return b.watch.heartbeat(ctx, h)
}

func (b *Backend) StopWatch(ctx context.Context, h platform.WatchHandle) error {
	return b.watch.stop(ctx, h)
}

func (b *Backend) Claim(ctx context.Context, s platform.Session, drop platform.DropBenefit) error {
	return b.claim.claim(ctx, s, drop)
}

// setAllowedLogins is exposed for tests / future allow-list wiring.
func (b *Backend) setAllowedLogins(campaignID string, logins []string) {
	b.mu.Lock()
	b.allowedLoginsByCampaign[campaignID] = logins
	b.mu.Unlock()
}

// AllowedChannelCount returns the number of channels in the cached
// allow-list for campaignID. Zero when the campaign hasn't been seen
// yet or has no allow-list. Used by the dashboard to fill in the
// "channels" column on each Active Campaigns row.
func (b *Backend) AllowedChannelCount(campaignID string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.allowedLoginsByCampaign[campaignID])
}
