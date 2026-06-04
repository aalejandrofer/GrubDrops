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
}

type DropBenefit struct {
	ID              string
	CampaignID      string
	Name            string
	RequiredMinutes int
	ImageURL        string
}

type Stream struct {
	Channel      string
	ViewerCount  int
	DropsEnabled bool
}

type Progress struct {
	BenefitID      string
	MinutesWatched int
	Claimed        bool
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
