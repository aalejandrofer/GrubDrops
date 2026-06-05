package platform

import "context"

type Backend interface {
	Name() string

	StartDeviceLogin(ctx context.Context) (DeviceChallenge, error)
	PollDeviceLogin(ctx context.Context, ch DeviceChallenge) (Session, error)
	LoginViaBrowser(ctx context.Context, rpc BrowserRPC) (Session, error)
	RefreshSession(ctx context.Context, s Session) (Session, error)

	ListActiveCampaigns(ctx context.Context, s Session) ([]Campaign, error)
	ListEligibleChannels(ctx context.Context, s Session, c Campaign) ([]Stream, error)
	InventoryProgress(ctx context.Context, s Session) ([]Progress, error)

	StartWatch(ctx context.Context, s Session, stream Stream) (WatchHandle, error)
	Heartbeat(ctx context.Context, h WatchHandle) error
	StopWatch(ctx context.Context, h WatchHandle) error
	Claim(ctx context.Context, s Session, b DropBenefit) error
}

// RewardClaimer is an optional capability for backends that surface
// one-click reward claims (e.g. Twitch /drops/inventory). Backends
// that don't support this don't implement the interface; the watcher
// type-asserts to discover support.
type RewardClaimer interface {
	ClaimRewards(ctx context.Context, s Session, allowedGames []string) ([]ClaimedReward, error)
}

// ClaimedReward is one entry returned by RewardClaimer.ClaimRewards.
type ClaimedReward struct {
	Game  string
	Title string
}

// PubSubHooks is the per-account real-time callback surface a Watcher
// installs into a PubSubAware backend. All callbacks may fire from any
// goroutine; implementations must not block.
type PubSubHooks struct {
	// OnDropProgress fires when Twitch's user-drop-events.<uid> emits a
	// drop-progress message. curMin/reqMin come straight from the
	// payload (in MINUTES, not seconds — Twitch's field name is
	// current_progress_min).
	OnDropProgress func(dropID string, curMin, reqMin int64)
	// OnDropClaim fires when user-drop-events.<uid> emits a drop-claim.
	// instanceID is the per-account drop_instance_id needed by the
	// claim mutation.
	OnDropClaim func(dropID, instanceID string)
	// OnStreamDown fires when video-playback-by-id.<channelID> emits
	// stream-down. Watcher should jump to pickStream rather than wait
	// for the next tick.
	OnStreamDown func(channelID string)
	// OnStreamUp mirrors OnStreamDown — fires on stream-up.
	OnStreamUp func(channelID string)
}

// PubSubAware is an optional backend capability. Backends that expose
// real-time events accept per-account PubSubHooks before the account's
// first ListActiveCampaigns triggers PubSub bootstrap. Watchers
// register hooks in their constructor.
type PubSubAware interface {
	SetAccountPubSubHooks(accountID string, hooks PubSubHooks)
}

// ChannelSubscriber is an optional backend capability for adding /
// removing per-channel real-time subscriptions during the watch
// lifetime (Twitch video-playback-by-id topic).
type ChannelSubscriber interface {
	SubscribeChannel(accountID, channelID string)
	UnsubscribeChannel(accountID, channelID string)
}

// CurrentSession is the post-claim verification result. Empty
// DropID/ChannelID mean "no active session" (no in-flight drop).
type CurrentSession struct {
	DropID         string
	ChannelID      string
	CurrentMinute  int
	RequiredMinute int
}

// AvailableDropsChecker is an optional backend capability for verifying
// a channel actually serves a target drop before committing watch time.
// Returns nil set + nil error for "unknown" (caller treats as
// permissive and proceeds).
type AvailableDropsChecker interface {
	AvailableDropIDs(ctx context.Context, s Session, channelID string) (map[string]struct{}, error)
}

// CurrentSessionChecker is an optional backend capability for the
// DropCurrentSessionContext gql query (P6). Watcher uses it post-claim
// as a soft consistency check.
type CurrentSessionChecker interface {
	CurrentSession(ctx context.Context, s Session) (CurrentSession, error)
}

type Registry struct {
	backends map[string]Backend
}

func NewRegistry() *Registry {
	return &Registry{backends: map[string]Backend{}}
}

func (r *Registry) Register(b Backend) {
	r.backends[b.Name()] = b
}

func (r *Registry) Get(name string) (Backend, bool) {
	b, ok := r.backends[name]
	return b, ok
}
