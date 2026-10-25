package runner

import (
	"context"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/plan"
	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// State is the runner's lifecycle position.
type State string

const (
	StateIdle            State = "idle"
	StatePicking         State = "picking"
	StateStarting        State = "starting" // finding a live channel
	StateWatching        State = "watching"
	StateClaiming        State = "claiming"
	StateSleeping        State = "sleeping"
	StateAwaitingConnect State = "awaiting_connect"
	StateNeedsLogin      State = "needs_login"
	StateNoGames         State = "no_games"
)

// Notifier receives state, progress and claim events.
type Notifier interface {
	Notify(ctx context.Context, event string, fields map[string]any) error
}

// ClaimLog is the per-account record of claimed benefits.
// *store.ClaimRecorder satisfies it.
type ClaimLog interface {
	RecordClaim(ctx context.Context, accountID string, b platform.DropBenefit) error
	RecordClaimWithCode(ctx context.Context, accountID string, b platform.DropBenefit, code string) error
	ClaimedBenefitIDs(ctx context.Context, accountID string) (map[string]bool, error)
}

// SkipStore persists benefits the runner gave up on.
type SkipStore interface {
	Skips(accountID string) (map[string]bool, error)
	RecordSkip(accountID, benefitID string) error
	ClearSkip(accountID, benefitID string) error
}

// DefaultTick is the heartbeat/poll cadence. Twitch credits one minute per
// beacon, so this must not be slower.
const DefaultTick = 60 * time.Second

type Config struct {
	AccountID, AccountLabel, Platform string
	Backend                           platform.Backend
	Session                           platform.Session
	Games                             []plan.Game
	ForceLinked                       map[string]bool
	Notifier                          Notifier        // nil-safe
	Claims                            ClaimLog        // nil-safe
	Skips                             SkipStore       // nil-safe
	Order                             func() []string // nil = no override
	// OnInventory is nil-safe; plan 2 reconciles here.
	OnInventory func(ctx context.Context, accountID string, camps []platform.Campaign, progress []platform.Progress)
	// OnChange is nil-safe; called after every state change and progress update.
	OnChange              func(Snapshot)
	TickInterval          time.Duration // <=0 -> DefaultTick
	ProgressNotifyStepPct int           // 0 disables progress notifications
}

// Snapshot is a copy of the runner's observable state.
type Snapshot struct {
	AccountID, Platform               string
	State                             State
	Current                           *plan.Target
	Channel                           string
	ViewerCount                       int
	MinutesWatched                    int
	StartedAt, LastPollAt, LastScanAt time.Time
	UpNext                            []plan.Target // queue minus Current, with Config.Order applied at read time
	Blocked                           []plan.Blocked
}
