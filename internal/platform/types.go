package platform

import "time"

type Session struct {
	AccessToken  string            `json:"access_token,omitempty"`
	RefreshToken string            `json:"refresh_token,omitempty"`
	Cookies      map[string]string `json:"cookies,omitempty"`
	CSRF         string            `json:"csrf,omitempty"`
	ExpiresAt    time.Time         `json:"expires_at"`
	Fingerprint  string            `json:"fingerprint,omitempty"`
	// AccountID is set by the scheduler/watcher before passing the
	// session to backend methods. Not persisted — populated at use time
	// so backends like the Twitch BrowserBackend can route per-account
	// gRPC calls to the right sidecar tab.
	AccountID string `json:"-"`

	// GameFilter, when non-nil, returns true iff the given game name (or
	// slug) is on this account's whitelist. Backends consult it inside
	// ListActiveCampaigns to short-circuit non-whitelisted games BEFORE
	// fanning out to per-campaign detail fetches (saves bandwidth and
	// makes the whitelist canonical, not just a watcher-side filter).
	// Match should be lenient — compare lowercased name OR slug.
	GameFilter func(game string) bool `json:"-"`
}

type Campaign struct {
	ID       string
	Platform string
	Game     string
	Name     string
	StartsAt time.Time
	EndsAt   time.Time
	Status   string
	Benefits []DropBenefit

	// AccountLinked indicates whether the user has connected the
	// external account this campaign requires for claims (e.g. Mojang
	// for Minecraft, Battle.net for Diablo). Source: Twitch
	// dropCampaign.self.isAccountConnected. False = user cannot
	// actually receive the drop even if minutes are watched, so the
	// watcher must skip these campaigns and the dashboard should
	// surface a "Link account →" call to action.
	AccountLinked bool
	// AccountLinkChecked is true if AccountLinked was derived from a
	// real gql probe (self.isAccountConnected). False if it was set
	// optimistically by scrape — UI surfaces "?" for unverified.
	AccountLinkChecked bool
	// AccountLinkURL is the campaign-specific link-account page the
	// operator should visit to make AccountLinked true. Empty if not
	// provided by the platform.
	AccountLinkURL string
	// Kind distinguishes "drop" (watch-time mining required) from
	// "reward" (one-click claim from /drops/inventory once the account
	// is linked — e.g. Minecraft Builder Cape). Empty defaults to
	// "drop". The watcher's pickCampaign() loop skips kind="reward"
	// since there's no minutes to accrue; a separate reaper claims
	// those out-of-band.
	Kind string
	// AllowedChannelCount is the number of channels the campaign's
	// allow-list permits. 0 means unrestricted (any channel streaming
	// the game qualifies). Used by the "low_avbl_first" priority mode
	// to prefer scarcer campaigns first (DevilXD parity).
	AllowedChannelCount int
}

type DropBenefit struct {
	ID              string
	CampaignID      string
	Name            string
	RequiredMinutes int
	ImageURL        string
	// InstanceID is populated at progress time (from Inventory.self.
	// dropInstanceID) and passed into the claim mutation. Distinct
	// from ID — the latter is the drop template, this is the per-
	// account instance. Empty until progress is observed.
	InstanceID string
	// RewardID is the underlying benefit/reward id (DropCampaignDetails
	// benefitEdges[].benefit.id), distinct from ID (the drop id used for
	// claiming). Twitch's inventory.gameEventDrops[].id reports OWNED
	// benefits by this id, so it's how we detect a drop already
	// earned/claimed (incl. in a prior season) and skip re-mining it.
	RewardID string
	// Preconditions lists the drop IDs that must be claimed before this
	// drop can accrue watch-time (Twitch DropCampaign.timeBasedDrops[]
	// .preconditionDrops; DevilXD TimedDrop preconditions). Empty means
	// no gating — the common case. The watcher skips a benefit whose
	// preconditions aren't all claimed yet, so the earlier drop in the
	// chain gets mined first.
	Preconditions []string
}

type Stream struct {
	Channel      string
	ViewerCount  int
	DropsEnabled bool

	// Fields populated for Twitch when listEligible fetches stream
	// metadata. They feed the SendEvents heartbeat payload — without
	// them the heartbeat is silently dropped server-side and minutes
	// never accrue. Empty for backends that don't track them yet.
	ChannelID   string // broadcaster user id (e.g. "491062114")
	BroadcastID string // current live stream id
	GameID      string // game/category id
	Game        string // human-readable game name
}

type Progress struct {
	BenefitID      string
	MinutesWatched int
	Claimed        bool
	// InstanceID is the per-account drop instance id that the claim
	// mutation expects. Distinct from BenefitID (the drop's template
	// id) — Twitch issues a fresh instance id when the user enters a
	// campaign. Empty for backends that don't surface it.
	InstanceID string
}

type WatchHandle struct {
	Channel   string
	AccountID string
	Internal  any
}

type DeviceChallenge struct {
	UserCode        string
	VerificationURL string
	ExpiresAt       time.Time
	Interval        time.Duration
	Internal        any
}

type BrowserRPC interface {
	LoginInteractive(platform string) (Session, error)
}
