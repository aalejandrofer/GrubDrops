package twitch

import (
	"context"
	"errors"
	"sync"

	"github.com/chano-fernandez/rust-drops-miner/internal/platform"
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
	if len(allowed) == 0 {
		return nil, nil
	}
	return b.chans.listEligible(ctx, s, c, allowed)
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
