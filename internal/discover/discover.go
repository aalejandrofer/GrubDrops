// Package discover lists every current drop campaign on each platform, for the
// Discover page. Results are cached: the catalog changes slowly and the
// platforms rate-limit.
package discover

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/platform/twitch"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

// ErrNeedsAndroid means no enabled Twitch account has an Android-client
// token, the only kind that can list the drops dashboard.
var ErrNeedsAndroid = errors.New("discover: no twitch android account")

// Defaults for New.
const (
	DefaultTTL        = 15 * time.Minute
	DefaultErrTTL     = 2 * time.Minute
	DefaultMinRefresh = time.Minute
)

// Fetcher lists one platform's current campaigns.
type Fetcher func(ctx context.Context) ([]platform.Campaign, error)

// Source is one platform's fetcher and its time budget.
type Source struct {
	Platform string
	Fetch    Fetcher
	Timeout  time.Duration
}

// Result is one platform's outcome. Err is nil on success.
type Result struct {
	Campaigns []platform.Campaign
	Err       error
}

// Snapshot is one fetch of every source.
type Snapshot struct {
	FetchedAt time.Time
	Results   map[string]Result
}

// Service caches Snapshots. TTL bounds a clean snapshot, ErrTTL one with a
// failed platform, and MinRefresh how often a refresh may bypass the cache.
type Service struct {
	sources    []Source
	TTL        time.Duration
	ErrTTL     time.Duration
	MinRefresh time.Duration
	Now        func() time.Time

	mu   sync.Mutex
	snap *Snapshot
}

// New builds a Service with the default TTLs.
func New(sources ...Source) *Service {
	return &Service{
		sources: sources, TTL: DefaultTTL, ErrTTL: DefaultErrTTL,
		MinRefresh: DefaultMinRefresh, Now: time.Now,
	}
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Get returns the cached snapshot, fetching when it is missing or stale, or
// when refresh is set and the last fetch is older than MinRefresh. Callers
// share one fetch; the fetch outlives a cancelled request.
func (s *Service) Get(ctx context.Context, refresh bool) Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.snap != nil {
		age := s.now().Sub(s.snap.FetchedAt)
		stale := age >= s.TTL || (failed(*s.snap) && age >= s.ErrTTL)
		if !stale && (!refresh || age < s.MinRefresh) {
			return *s.snap
		}
	}
	snap := s.fetch(context.WithoutCancel(ctx))
	s.snap = &snap
	return snap
}

// NextRefresh is when a refresh will next bypass the cache. Zero before the
// first fetch.
func (s *Service) NextRefresh() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.snap == nil {
		return time.Time{}
	}
	return s.snap.FetchedAt.Add(s.MinRefresh)
}

func failed(sn Snapshot) bool {
	for _, r := range sn.Results {
		if r.Err != nil {
			return true
		}
	}
	return false
}

func (s *Service) fetch(ctx context.Context) Snapshot {
	out := Snapshot{FetchedAt: s.now(), Results: make(map[string]Result, len(s.sources))}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, src := range s.sources {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, cancel := context.WithTimeout(ctx, src.Timeout)
			defer cancel()
			camps, err := src.Fetch(c)
			if err != nil && !errors.Is(err, ErrNeedsAndroid) {
				slog.Warn("discover: fetch failed", "platform", src.Platform, "err", err)
			}
			if err != nil {
				camps = nil
			}
			mu.Lock()
			out.Results[src.Platform] = Result{Campaigns: camps, Err: err}
			mu.Unlock()
		}()
	}
	wg.Wait()
	return out
}

// ErrCode is the API error code for a platform error: "" for nil.
func ErrCode(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrNeedsAndroid):
		return "needs_android"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	default:
		return "failed"
	}
}

// Accounts lists accounts; SessionFor loads one account's session.
type (
	Accounts   func(ctx context.Context) ([]gen.Account, error)
	SessionFor func(ctx context.Context, accountID string) (platform.Session, bool, error)
	Catalog    func(ctx context.Context, s platform.Session) ([]platform.Campaign, error)
)

// sessions returns the sessions of enabled accounts on platformName, in
// account order.
func sessions(ctx context.Context, list Accounts, get SessionFor, platformName string) ([]platform.Session, error) {
	accts, err := list(ctx)
	if err != nil {
		return nil, err
	}
	var out []platform.Session
	for _, a := range accts {
		if a.Platform != platformName || a.Enabled == 0 {
			continue
		}
		s, ok, err := get(ctx, a.ID)
		if err != nil || !ok {
			continue
		}
		s.AccountID = a.ID
		out = append(out, s)
	}
	return out, nil
}

// TwitchFetcher lists the drops dashboard through the first enabled Twitch
// account with an Android-client token that answers it.
func TwitchFetcher(list Accounts, get SessionFor, catalog Catalog) Fetcher {
	return func(ctx context.Context) ([]platform.Campaign, error) {
		ss, err := sessions(ctx, list, get, "twitch")
		if err != nil {
			return nil, err
		}
		for _, s := range ss {
			if s.ClientID == twitch.ClientTV || s.AccessToken == "" {
				continue
			}
			camps, err := catalog(ctx, s)
			if errors.Is(err, twitch.ErrCatalogUnavailable) {
				continue
			}
			return camps, err
		}
		return nil, ErrNeedsAndroid
	}
}

// KickFetcher lists Kick campaigns with the first enabled Kick account's
// session, falling back to no session: the list is public.
func KickFetcher(list Accounts, get SessionFor, catalog Catalog) Fetcher {
	return func(ctx context.Context) ([]platform.Campaign, error) {
		ss, err := sessions(ctx, list, get, "kick")
		if err == nil && len(ss) > 0 {
			if camps, err := catalog(ctx, ss[0]); err == nil {
				return camps, nil
			}
		}
		return catalog(ctx, platform.Session{})
	}
}
