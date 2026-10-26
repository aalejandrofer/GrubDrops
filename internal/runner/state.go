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
type ClaimLog interface {
	// RecordClaim upserts a claim for t. A non-empty code is stored with it.
	RecordClaim(ctx context.Context, accountID string, t plan.Target, code string) error
	ClaimedBenefitIDs(ctx context.Context, accountID string) (map[string]bool, error)
}

// Scan is one completed scan of an account: campaigns, inventory and the plan.
type Scan struct {
	AccountID, Platform string
	At                  time.Time
	Err                 error // campaign list or claim log read failed; the other fields are empty
	Campaigns           []platform.Campaign
	Progress            []platform.Progress
	InventoryOK         bool            // Progress came from a successful read
	InventoryErr        string          // why the inventory read failed; "" when InventoryOK
	Logged              map[string]bool // claim-log benefit IDs the plan used
	Result              plan.Result
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
	// Hooks are nil-safe. Each may run on the Run goroutine or a PubSub
	// goroutine and must not block for long. Payloads (Scan slices and maps,
	// progress) are read-only and shared with the runner: copy before keeping
	// or changing them.
	OnScan     func(ctx context.Context, s Scan)
	OnProgress func(ctx context.Context, accountID string, progress []platform.Progress)
	OnBeat     func(ctx context.Context, accountID string) // after each successful Heartbeat
	// OnChange is nil-safe; called after every state change and progress update.
	// It may be called concurrently from the backend PubSub goroutine and the
	// Run goroutine, so implementations must be thread-safe.
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
	NextScanAt                        time.Time     // next scan by state: rescan pace, recheck deadline, zero when stopped or unscanned
	UpNext                            []plan.Target // queue minus Current, with Config.Order applied at read time
	Blocked                           []plan.Blocked
}
