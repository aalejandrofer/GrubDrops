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
	// AccountLinkURL is the campaign-specific link-account page the
	// operator should visit to make AccountLinked true. Empty if not
	// provided by the platform.
	AccountLinkURL string
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
