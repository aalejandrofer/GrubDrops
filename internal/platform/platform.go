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
