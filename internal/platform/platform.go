package platform

import (
	"context"
	"errors"
)

// ErrIntegrityBlocked is returned by a Backend (Twitch today) when the
// integrity wall keeps refusing privileged gql fields even after the
// sidecar retry. Watchers catch this with errors.Is to transition the
// account into needs_auth.
var ErrIntegrityBlocked = errors.New("integrity check failed")

// ErrClaimNeedsLink is matched with errors.Is on a Claim error when the
// platform refused the claim until the user links an external account (Riot,
// Steam, ...). The runner then blocks the drop as not linked instead of
// counting a claim failure.
var ErrClaimNeedsLink = errors.New("claim: account link required")

// ErrClaimRefused is matched with errors.Is on a Claim error when the platform
// answered and turned the claim down, as opposed to a transport or server
// error. Twitch gives no reason, so the runner reads it as a link refusal on a
// campaign the platform reports unlinked.
var ErrClaimRefused = errors.New("claim refused")

// LinkRechecker is an optional backend capability: forget the account's
// link-refused claims, retry them now, and report what was claimed and how
// many campaigns still need a link.
type LinkRechecker interface {
	RecheckLinks(ctx context.Context, s Session) (claimed []ClaimedReward, stillNeedsLink int, err error)
}

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
	// OnRewardCode fires when an onsite-notification carries a Twitch
	// / Mojang-style redemption code (e.g. Minecraft cape codes).
	// notificationID dedupes; body is the raw notification text so
	// the receiver can extract game/drop context.
	OnRewardCode func(notificationID, code, body string)
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

// CampaignDetailer is an optional backend capability: fetch a single
// campaign's benefits on demand. Discovery only fetches details for
// whitelisted campaigns (to bound bandwidth), so non-whitelisted ones have
// no benefits persisted — the /drops items panel uses this to lazily
// backfill them the first time a user expands the row.
type CampaignDetailer interface {
	CampaignDetails(ctx context.Context, s Session, campaignID string) ([]DropBenefit, error)
}

// AuthChecker is an optional backend capability: a cheap liveness probe
// of the account's auth (Twitch token / Kick cookies) without starting a
// watch. Returns nil when auth is healthy, a non-nil error describing the
// problem otherwise. Used by the periodic auth-health sweep + the manual
// "check auth now" button.
type AuthChecker interface {
	VerifyAuth(ctx context.Context, s Session) error
}

// AvatarFetcher is an optional backend capability: fetch the
// authenticated account's own profile-picture URL. Returns the URL
// (empty if the platform has none) and any error. Twitch returns a
// direct static-cdn.jtvnw.net URL; Kick returns its profile_pic value
// (served through the /img/kick proxy, since Cloudflare 403s hotlinks).
// Called off the hot path — only on login + the periodic auth-health
// sweep, since avatars change rarely.
type AvatarFetcher interface {
	FetchAvatar(ctx context.Context, s Session) (string, error)
}

// CurrentSessionChecker is an optional backend capability for the
// DropCurrentSessionContext gql query (P6). Watcher uses it post-claim
// as a soft consistency check.
type CurrentSessionChecker interface {
	CurrentSession(ctx context.Context, s Session) (CurrentSession, error)
}

// CompletedSweeper is an optional backend capability for platforms where a
// SINGLE watch session accrues progress toward MANY rewards at once (Kick:
// watching one live category channel advances every open same-category
// campaign + any Team campaign that channel belongs to). The watcher tracks
// only one currentBenefit, so without this it would miss sibling rewards that
// complete in the same window. The watcher calls SweepCompletedClaims on each
// progress poll; the backend claims every reward that has reached 100% and is
// not already granted (POSTing the claim where the platform needs it, or
// no-op'ing when the platform auto-grants), and returns what it claimed so the
// watcher can record it. Backends where each watch maps to one reward (Twitch)
// do not implement this.
type CompletedSweeper interface {
	SweepCompletedClaims(ctx context.Context, s Session) ([]ClaimedReward, error)
}

// ChannelLooker is optional: one channel's live stream by login (Twitch) or slug (Kick).
type ChannelLooker interface {
	LookupChannel(ctx context.Context, s Session, login string) (stream Stream, live bool, err error)
}

// CategoryLooker is optional: a game's display name from its directory slug,
// e.g. the overwatch-2 in twitch.tv/directory/category/overwatch-2. ok is
// false when the platform knows no such category.
type CategoryLooker interface {
	LookupCategory(ctx context.Context, s Session, slug string) (name string, ok bool, err error)
}

// GameIDLooker is optional: a game's platform id from its name, for box art
// of games no campaign has named yet. ok is false when the platform knows
// no such game.
type GameIDLooker interface {
	LookupGameID(ctx context.Context, s Session, name string) (id string, ok bool, err error)
}

// ReadyDropLister is optional: drops in the inventory that are fully watched and unclaimed,
// whatever their game. Twitch only.
type ReadyDropLister interface {
	ReadyDrops(ctx context.Context, s Session) ([]ReadyDrop, error)
}

// ReadyDrop is one fully watched, unclaimed inventory drop and the campaign
// it belongs to. Campaign carries name, game and link state, no Benefits.
type ReadyDrop struct {
	Campaign Campaign
	Benefit  DropBenefit
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
