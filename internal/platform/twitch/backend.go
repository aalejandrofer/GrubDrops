package twitch

import (
	"context"
	"errors"
	"log/slog"
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
	adv   *advisory

	// allowedLoginsByCampaign caches the allow-list pulled from
	// dropCampaignDetails.allow.channels[].login. ListEligibleChannels
	// reads from this map.
	mu                      sync.Mutex
	allowedLoginsByCampaign map[string][]string

	// PubSub WebSocket — one per backend (per platform-account). Lazy
	// init on first ListActiveCampaigns once we have the user_id +
	// auth token. Real-time progress / claim / stream-down events feed
	// callbacks set via SetPubSubHandlers.
	pubsubMu       sync.Mutex
	pubsub         *PubSubClient
	pubsubCancel   context.CancelFunc
	pubsubHandlers PubSubHandlers
	pubsubDisabled bool // tests disable via newForTest
}

var _ platform.Backend = (*Backend)(nil)

// Backend must satisfy ChannelSubscriber so the watcher subscribes
// video-playback PubSub topics for event-driven stream-up/down, and
// PubSubAware so the watcher's real-time hooks actually receive events
// (both signatures drifted once and broke this silently).
var _ platform.ChannelSubscriber = (*Backend)(nil)
var _ platform.PubSubAware = (*Backend)(nil)

func New() *Backend {
	c := newClient()
	return &Backend{
		c:                       c,
		auth:                    newAuthFlow(),
		disc:                    &discovery{c: c},
		chans:                   &channels{c: c},
		watch:                   newWatch(),
		claim:                   &claimer{c: c},
		adv:                     &advisory{c: c},
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
		adv:                     &advisory{c: c},
		allowedLoginsByCampaign: map[string][]string{},
		pubsubDisabled:          true,
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
	// Best-effort PubSub bootstrap. Once-only — subsequent calls noop.
	// Failures are non-fatal: the watcher falls back to polling.
	b.ensurePubSub(s)
	return camps, nil
}

// SetPubSubHandlers wires real-time callbacks. Must be called before
// the first ListActiveCampaigns (which lazily connects). Safe to leave
// nil — PubSub still connects + logs events for diagnostic purposes.
func (b *Backend) SetPubSubHandlers(h PubSubHandlers) {
	b.pubsubMu.Lock()
	b.pubsubHandlers = h
	b.pubsubMu.Unlock()
}

// SetAccountPubSubHooks satisfies platform.PubSubAware — the method the
// Watcher actually calls in its constructor. Without it the direct
// backend received PubSub messages but had nil handlers, so every
// real-time event (drop-progress, drop-claim, stream-down, reward-code)
// was silently dropped and the watcher fell back to polling. The direct
// backend runs a single shared PubSub client today, so accountID is
// ignored; the hook fields map 1:1 onto the internal PubSubHandlers.
// Must be called before the first ListActiveCampaigns (lazy bootstrap).
func (b *Backend) SetAccountPubSubHooks(_ string, h platform.PubSubHooks) {
	b.pubsubMu.Lock()
	b.pubsubHandlers = PubSubHandlers{
		OnDropProgress: h.OnDropProgress,
		OnDropClaim:    h.OnDropClaim,
		OnStreamDown:   h.OnStreamDown,
		OnStreamUp:     h.OnStreamUp,
		OnRewardCode:   h.OnRewardCode,
	}
	b.pubsubMu.Unlock()
}

// ensurePubSub lazily starts the PubSub WebSocket on first use. Resolves
// the user_id from the session, subscribes to user-drop-events +
// onsite-notifications, then runs the read/ping loop in a goroutine.
// video-playback-by-id topics are added per-channel by SubscribeChannel.
func (b *Backend) ensurePubSub(s platform.Session) {
	b.pubsubMu.Lock()
	if b.pubsub != nil || b.pubsubDisabled {
		b.pubsubMu.Unlock()
		return
	}
	b.pubsubMu.Unlock()

	userID, err := b.watch.resolveUserID(context.Background(), s)
	if err != nil {
		slog.Warn("pubsub: resolve user id failed, deferring", "err", err)
		return
	}
	b.pubsubMu.Lock()
	if b.pubsub != nil {
		b.pubsubMu.Unlock()
		return
	}
	handlers := b.pubsubHandlers
	client := NewPubSubClient(s.AccessToken, handlers)
	ctx, cancel := context.WithCancel(context.Background())
	b.pubsub = client
	b.pubsubCancel = cancel
	b.pubsubMu.Unlock()
	go func() {
		topics := []string{
			TopicUserDropEvents(userID),
			TopicOnsiteNotifications(userID),
		}
		if err := client.Run(ctx, topics); err != nil && !errors.Is(err, context.Canceled) {
			slog.Warn("pubsub run exited", "err", err)
		}
	}()
}

// SubscribeChannel adds (or refreshes) a video-playback-by-id.<id>
// topic on the PubSub socket so stream-up/down events fire. Idempotent.
// Caller passes the broadcaster's numeric channel id. The accountID is
// accepted to satisfy platform.ChannelSubscriber (the direct backend
// runs a single shared PubSub client today, so it's unused) — without
// this 2-arg signature the watcher's ChannelSubscriber type assertion
// fails silently and stream-down events never reach the watcher.
func (b *Backend) SubscribeChannel(_ string, channelID string) {
	b.pubsubMu.Lock()
	client := b.pubsub
	b.pubsubMu.Unlock()
	if client == nil || channelID == "" {
		return
	}
	client.AddTopic(TopicVideoPlaybackByID(channelID))
}

// UnsubscribeChannel drops a video-playback-by-id.<id> topic.
func (b *Backend) UnsubscribeChannel(_ string, channelID string) {
	b.pubsubMu.Lock()
	client := b.pubsub
	b.pubsubMu.Unlock()
	if client == nil || channelID == "" {
		return
	}
	client.RemoveTopic(TopicVideoPlaybackByID(channelID))
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

// AvailableDropIDs satisfies platform.AvailableDropsChecker. Returns
// the set of drop template IDs the channel is currently serving.
// Empty result + nil error means "no info" — caller skips the gate.
func (b *Backend) AvailableDropIDs(ctx context.Context, s platform.Session, channelID string) (map[string]struct{}, error) {
	return b.adv.availableDropIDs(ctx, s, channelID)
}

// CurrentSession satisfies platform.CurrentSessionChecker. Returns the
// active drop session for the authenticated user, or zero-value when
// nothing is in flight.
func (b *Backend) CurrentSession(ctx context.Context, s platform.Session) (platform.CurrentSession, error) {
	return b.adv.currentSession(ctx, s)
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
