package api

import (
	"context"

	"github.com/aalejandrofer/grubdrops/internal/runner"
)

// RunnerPool is the part of *runner.Pool the API reads and steers.
type RunnerPool interface {
	Snapshots() []runner.Snapshot
	SetOrder(accountID string, benefitIDs []string)
	ResetOrder(accountID string)
	OrderFunc(accountID string) func() []string
	RescanAll()
	// RescanPlatform rescans every unit on plat except account except.
	RescanPlatform(plat, except string)
	// SettingsChangedAll tells every runner a live-read engine setting
	// changed (sleep window, timezone, follow raids).
	SettingsChangedAll()
	// MarkCollected and MarkUncollected tell the account's runner about a
	// manual claim edit without a rebuild.
	MarkCollected(accountID, benefitID string)
	MarkUncollected(accountID, benefitID string)
	MineNow(accountID string) (string, error)
	RecheckLinks(ctx context.Context, accountID string) (runner.LinkRecheck, error)
}

var _ RunnerPool = (*runner.Pool)(nil)

// Notifier sends one notification event.
type Notifier interface {
	Notify(ctx context.Context, event string, fields map[string]any) error
}

// KickChannelRegistrar stores the channels picked at Kick login.
type KickChannelRegistrar interface {
	RegisterChannels(accountID string, channels []string)
}
