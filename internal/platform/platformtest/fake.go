package platformtest

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// Fake is a scriptable platform.Backend for planner / runner tests. Tests
// seed Campaigns, Streams and Progress, inject errors, and read Calls.
// All exported fields are guarded by Mu; tests that touch them while a
// runner goroutine is live must hold Mu.
type Fake struct {
	Mu        sync.Mutex
	Campaigns []platform.Campaign
	Streams   map[string][]platform.Stream  // campaign ID -> live streams
	Progress  map[string]*platform.Progress // benefit ID -> inventory row; absent = not in inventory
	// OnHeartbeat is called with Mu held. nil = +1 minute to every unclaimed row.
	OnHeartbeat func(f *Fake)

	// Lookups scripts LookupChannel per login; absent = offline.
	Lookups map[string]Lookup
	// Ready is returned by ReadyDrops.
	Ready    []platform.ReadyDrop
	ReadyErr error

	ListErr, InventoryErr, StartErr, HeartbeatErr, ClaimErr error
	// StartErrs fails StartWatch for one channel; StartErr wins when set.
	StartErrs map[string]error

	// Calls records "List", "Inventory", "Channels:<campID>",
	// "Start:<channel>", "Heartbeat:<channel>", "Stop:<channel>",
	// "Claim:<benefitID>".
	Calls []string
}

// Lookup is one scripted LookupChannel result.
type Lookup struct {
	Stream platform.Stream
	Live   bool
	Err    error
}

// NewFake returns an empty Fake.
func NewFake() *Fake {
	return &Fake{
		Streams:  map[string][]platform.Stream{},
		Progress: map[string]*platform.Progress{},
		Lookups:  map[string]Lookup{},
	}
}

var (
	_ platform.ChannelLooker   = (*Fake)(nil)
	_ platform.ReadyDropLister = (*Fake)(nil)
)

// LookupChannel records "Lookup:<login>" and returns the scripted result.
func (f *Fake) LookupChannel(_ context.Context, _ platform.Session, login string) (platform.Stream, bool, error) {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	f.Calls = append(f.Calls, "Lookup:"+login)
	l := f.Lookups[login]
	return l.Stream, l.Live, l.Err
}

// ReadyDrops records "ReadyDrops" and returns Ready.
func (f *Fake) ReadyDrops(_ context.Context, _ platform.Session) ([]platform.ReadyDrop, error) {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	f.Calls = append(f.Calls, "ReadyDrops")
	if f.ReadyErr != nil {
		return nil, f.ReadyErr
	}
	return append([]platform.ReadyDrop(nil), f.Ready...), nil
}

var _ platform.Backend = (*Fake)(nil)

// CallCount returns how many recorded calls start with prefix.
func (f *Fake) CallCount(prefix string) int {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	n := 0
	for _, c := range f.Calls {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

func (f *Fake) Name() string { return "fake" }

func (f *Fake) StartDeviceLogin(_ context.Context) (platform.DeviceChallenge, error) {
	return platform.DeviceChallenge{UserCode: "FAKE", ExpiresAt: time.Now().Add(time.Hour)}, nil
}

func (f *Fake) PollDeviceLogin(_ context.Context, _ platform.DeviceChallenge) (platform.Session, error) {
	return platform.Session{AccessToken: "fake-token", ExpiresAt: time.Now().Add(time.Hour)}, nil
}

func (f *Fake) LoginViaBrowser(_ context.Context, _ platform.BrowserRPC) (platform.Session, error) {
	return platform.Session{}, nil
}

func (f *Fake) RefreshSession(_ context.Context, s platform.Session) (platform.Session, error) {
	return s, nil
}

func (f *Fake) ListActiveCampaigns(_ context.Context, _ platform.Session) ([]platform.Campaign, error) {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	f.Calls = append(f.Calls, "List")
	if f.ListErr != nil {
		return nil, f.ListErr
	}
	return append([]platform.Campaign(nil), f.Campaigns...), nil
}

func (f *Fake) ListEligibleChannels(_ context.Context, _ platform.Session, c platform.Campaign) ([]platform.Stream, error) {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	f.Calls = append(f.Calls, "Channels:"+c.ID)
	return append([]platform.Stream(nil), f.Streams[c.ID]...), nil
}

func (f *Fake) InventoryProgress(_ context.Context, _ platform.Session) ([]platform.Progress, error) {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	f.Calls = append(f.Calls, "Inventory")
	if f.InventoryErr != nil {
		return nil, f.InventoryErr
	}
	out := make([]platform.Progress, 0, len(f.Progress))
	for _, p := range f.Progress {
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].BenefitID < out[j].BenefitID })
	return out, nil
}

func (f *Fake) StartWatch(_ context.Context, _ platform.Session, s platform.Stream) (platform.WatchHandle, error) {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	f.Calls = append(f.Calls, "Start:"+s.Channel)
	if f.StartErr != nil {
		return platform.WatchHandle{}, f.StartErr
	}
	if err := f.StartErrs[s.Channel]; err != nil {
		return platform.WatchHandle{}, err
	}
	return platform.WatchHandle{Channel: s.Channel}, nil
}

func (f *Fake) Heartbeat(_ context.Context, h platform.WatchHandle) error {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	f.Calls = append(f.Calls, "Heartbeat:"+h.Channel)
	if f.HeartbeatErr != nil {
		return f.HeartbeatErr
	}
	if f.OnHeartbeat != nil {
		f.OnHeartbeat(f)
		return nil
	}
	for _, p := range f.Progress {
		if !p.Claimed {
			p.MinutesWatched++
		}
	}
	return nil
}

func (f *Fake) StopWatch(_ context.Context, h platform.WatchHandle) error {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	f.Calls = append(f.Calls, "Stop:"+h.Channel)
	return nil
}

func (f *Fake) Claim(_ context.Context, _ platform.Session, b platform.DropBenefit) error {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	f.Calls = append(f.Calls, "Claim:"+b.ID)
	if f.ClaimErr != nil {
		return f.ClaimErr
	}
	p := f.Progress[b.ID]
	if p == nil {
		p = &platform.Progress{BenefitID: b.ID}
		f.Progress[b.ID] = p
	}
	p.Claimed = true
	return nil
}

// PubSubFake adds platform.PubSubAware with the real backends' capture
// rule: SetAccountPubSubHooks only stages hooks, the account's first
// ListActiveCampaigns or InventoryProgress bootstraps its PubSub client
// with whatever is staged, and later SetAccountPubSubHooks calls for a
// bootstrapped account are ignored. Hooks holds the bootstrapped hooks.
type PubSubFake struct {
	*Fake
	Hooks  map[string]platform.PubSubHooks // account ID -> hooks the client captured
	staged map[string]platform.PubSubHooks
}

var _ platform.Backend = (*PubSubFake)(nil)
var _ platform.PubSubAware = (*PubSubFake)(nil)

func (p *PubSubFake) SetAccountPubSubHooks(accountID string, hooks platform.PubSubHooks) {
	p.Mu.Lock()
	defer p.Mu.Unlock()
	if _, booted := p.Hooks[accountID]; booted {
		return
	}
	if p.staged == nil {
		p.staged = map[string]platform.PubSubHooks{}
	}
	p.staged[accountID] = hooks
}

func (p *PubSubFake) bootstrap(accountID string) {
	p.Mu.Lock()
	defer p.Mu.Unlock()
	if _, booted := p.Hooks[accountID]; booted {
		return
	}
	if p.Hooks == nil {
		p.Hooks = map[string]platform.PubSubHooks{}
	}
	p.Hooks[accountID] = p.staged[accountID]
}

func (p *PubSubFake) ListActiveCampaigns(ctx context.Context, s platform.Session) ([]platform.Campaign, error) {
	out, err := p.Fake.ListActiveCampaigns(ctx, s)
	p.bootstrap(s.AccountID)
	return out, err
}

func (p *PubSubFake) InventoryProgress(ctx context.Context, s platform.Session) ([]platform.Progress, error) {
	out, err := p.Fake.InventoryProgress(ctx, s)
	p.bootstrap(s.AccountID)
	return out, err
}

// SweepFake adds platform.CompletedSweeper. Sweeps counts the calls under Mu.
type SweepFake struct {
	*Fake
	Sweep  func() []platform.ClaimedReward
	Sweeps int
}

var _ platform.Backend = (*SweepFake)(nil)
var _ platform.CompletedSweeper = (*SweepFake)(nil)

// SweepCount returns how many times SweepCompletedClaims ran.
func (s *SweepFake) SweepCount() int {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	return s.Sweeps
}

func (s *SweepFake) SweepCompletedClaims(_ context.Context, _ platform.Session) ([]platform.ClaimedReward, error) {
	s.Mu.Lock()
	s.Sweeps++
	s.Mu.Unlock()
	if s.Sweep == nil {
		return nil, nil
	}
	return s.Sweep(), nil
}

// RewardFake adds platform.RewardClaimer and records allowedGames per call.
type RewardFake struct {
	*Fake
	Rewards     []platform.ClaimedReward
	RewardCalls [][]string
}

var _ platform.Backend = (*RewardFake)(nil)
var _ platform.RewardClaimer = (*RewardFake)(nil)

func (r *RewardFake) ClaimRewards(_ context.Context, _ platform.Session, allowedGames []string) ([]platform.ClaimedReward, error) {
	r.Mu.Lock()
	defer r.Mu.Unlock()
	r.RewardCalls = append(r.RewardCalls, append([]string(nil), allowedGames...))
	return append([]platform.ClaimedReward(nil), r.Rewards...), nil
}

// ProbeFake adds platform.AvailableDropsChecker keyed by channel ID. A
// channel absent from Available returns nil, nil ("unknown").
type ProbeFake struct {
	*Fake
	Available map[string]map[string]struct{}
}

var _ platform.Backend = (*ProbeFake)(nil)
var _ platform.AvailableDropsChecker = (*ProbeFake)(nil)

func (p *ProbeFake) AvailableDropIDs(_ context.Context, _ platform.Session, channelID string) (map[string]struct{}, error) {
	p.Mu.Lock()
	defer p.Mu.Unlock()
	return p.Available[channelID], nil
}
