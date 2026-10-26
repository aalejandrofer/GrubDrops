package api

import (
	"context"

	pb "github.com/aalejandrofer/grubdrops/internal/auth/browser/gen/browser/v1"
	"github.com/aalejandrofer/grubdrops/internal/runner"
)

// RunnerPool is the part of *runner.Pool the API reads and steers.
type RunnerPool interface {
	Snapshots() []runner.Snapshot
	SetOrder(accountID string, benefitIDs []string)
	ResetOrder(accountID string)
	OrderFunc(accountID string) func() []string
}

var _ RunnerPool = (*runner.Pool)(nil)

// Notifier sends one notification event.
type Notifier interface {
	Notify(ctx context.Context, event string, fields map[string]any) error
}

// KickBrowserClient verifies a Kick session through the browser sidecar.
type KickBrowserClient interface {
	Authenticate(ctx context.Context, s *pb.KickSession) (*pb.AuthenticateResponse, error)
}

// KickChannelRegistrar stores the channels picked at Kick login.
type KickChannelRegistrar interface {
	RegisterChannels(accountID string, channels []string)
}
