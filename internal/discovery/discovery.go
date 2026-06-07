// Package discovery scrapes active drop campaigns from each platform
// independently of the watcher loop. The goal is to keep the /drops page
// (and the campaigns table that backs it) populated with every active
// whitelisted campaign even when no account is signed in or no watcher
// has ticked yet.
//
// A Scraper is a small orchestrator: on every tick it computes the union
// of every enabled account's per-account game whitelist, asks each
// Provider for active campaigns matching that union, and pushes the
// results into the existing CampaignPersister (which UPSERTs into the
// campaigns + benefits tables — idempotent and safe to interleave with
// watcher-driven discovery).
//
// Providers are platform-shaped: TwitchScraper borrows the first enabled
// Twitch account's session and reuses the existing Twitch backend's
// gql/ViewerDropsDashboard call. KickScraper does the analogous thing
// for Kick via the sidecar. When the prerequisite session is missing,
// a Provider must Scrape gracefully (return nil, nil) and the Scraper
// logs a single warning.
//
// Whitelist enforcement: providers MUST honor the game allow-list — the
// Scraper materialises it as a GameFilter closure and either (a) sets it
// on the platform.Session passed to the backend, or (b) filters
// post-hoc. Non-whitelisted games NEVER reach the persister.
package discovery

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/aalejandrofer/dropsminer/internal/platform"
	"github.com/aalejandrofer/dropsminer/internal/store"
	"github.com/aalejandrofer/dropsminer/internal/store/gen"
)

// Provider scrapes active campaigns for a single platform. Implementations
// must:
//   - filter to games in `whitelist` (lowercased names + slugs)
//   - return (nil, nil) when auth context is missing — never an error
//   - tag each Campaign.Platform with the provider's platform name
//
// Returning an error is reserved for transient failures (network,
// sidecar restart, gql 5xx). The Scraper logs + continues; one provider
// failing must not poison the others.
type Provider interface {
	Name() string
	Scrape(ctx context.Context, whitelist []string) ([]platform.Campaign, error)
}

// CampaignPersister mirrors store.CampaignPersister — defined here as
// an interface so the package can be tested without a SQLite dependency.
type CampaignPersister interface {
	PersistCampaigns(ctx context.Context, camps []platform.Campaign) error
}

// WhitelistSource returns the union (deduped, lowercased) of every
// enabled account's game whitelist. The Scraper calls it once per tick
// so config changes (new game opted-in via the GUI) take effect on the
// next pass without restart.
type WhitelistSource func(ctx context.Context) ([]string, error)

// NewQueriesWhitelist builds a WhitelistSource backed by the generated
// sqlc queries: union(ListAccountGames(a) for a in ListEnabledAccounts()).
// The returned slice is lowercased + sorted; both names and slugs are
// emitted so providers that only see one or the other still match.
func NewQueriesWhitelist(q *gen.Queries) WhitelistSource {
	return func(ctx context.Context) ([]string, error) {
		accs, err := q.ListEnabledAccounts(ctx)
		if err != nil {
			return nil, err
		}
		set := make(map[string]struct{}, 8)
		for _, a := range accs {
			rows, err := q.ListAccountGames(ctx, a.ID)
			if err != nil {
				return nil, err
			}
			for _, r := range rows {
				if n := strings.ToLower(strings.TrimSpace(r.Name)); n != "" {
					set[n] = struct{}{}
				}
				if s := strings.ToLower(strings.TrimSpace(r.Slug)); s != "" {
					set[s] = struct{}{}
				}
			}
		}
		// Also union the GLOBAL priority list. Accounts that haven't picked
		// a per-account whitelist mine from global_games (account_games is
		// empty for them), so without this the discovery whitelist comes
		// back empty and EVERY tick no-ops — campaigns never refresh and
		// new ones never appear. The watcher applies the same global
		// fallback per-account; discovery must mirror it.
		gg, err := q.ListGlobalGames(ctx)
		if err != nil {
			return nil, err
		}
		for _, r := range gg {
			if n := strings.ToLower(strings.TrimSpace(r.Name)); n != "" {
				set[n] = struct{}{}
			}
			if s := strings.ToLower(strings.TrimSpace(r.Slug)); s != "" {
				set[s] = struct{}{}
			}
		}
		out := make([]string, 0, len(set))
		for k := range set {
			out = append(out, k)
		}
		sort.Strings(out)
		return out, nil
	}
}

// Scraper orchestrates a slice of Providers.
type Scraper struct {
	Providers []Provider
	Whitelist WhitelistSource
	Persister CampaignPersister
	Logger    *slog.Logger

	// tickMu serialises Tick so concurrent Run + manual Tick invocations
	// can't race on a provider's internal state. Cheap because a tick
	// runs once per interval (default 5m).
	tickMu sync.Mutex
}

// New returns a Scraper. Nil persister or whitelist means the Scraper
// is a no-op — useful when the caller wants to build it conditionally
// (e.g. when no providers are wired up).
func New(persister CampaignPersister, whitelist WhitelistSource, providers ...Provider) *Scraper {
	logger := slog.Default().With("component", "discovery")
	return &Scraper{
		Providers: providers,
		Whitelist: whitelist,
		Persister: persister,
		Logger:    logger,
	}
}

// Run blocks until ctx is cancelled, calling Tick every `interval`
// (and once immediately on entry).
func (s *Scraper) Run(ctx context.Context, interval time.Duration) {
	if s == nil || len(s.Providers) == 0 {
		return
	}
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	// Run once immediately so the /drops page is populated before the
	// first tick fires.
	s.Tick(ctx)
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.Tick(ctx)
		}
	}
}

// Tick runs every provider once, fans the results into the persister.
// Safe to call manually (used by tests) and concurrently with Run (a
// mutex serialises the provider sweep so the persister isn't hit twice
// for the same campaign in the same tick).
func (s *Scraper) Tick(ctx context.Context) {
	if s == nil || len(s.Providers) == 0 {
		return
	}
	s.tickMu.Lock()
	defer s.tickMu.Unlock()

	whitelist, err := s.loadWhitelist(ctx)
	if err != nil {
		s.Logger.Warn("discovery: failed to load whitelist; skipping tick", "err", err)
		return
	}
	if len(whitelist) == 0 {
		// No account has opted into any game yet. Per project_goal.md
		// we NEVER scrape non-whitelisted games, so without a whitelist
		// the only safe action is to no-op.
		s.Logger.Debug("discovery: whitelist empty, skipping tick")
		return
	}

	for _, p := range s.Providers {
		camps, err := p.Scrape(ctx, whitelist)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return
			}
			s.Logger.Warn("discovery: provider scrape failed",
				"provider", p.Name(), "err", err)
			continue
		}
		if len(camps) == 0 {
			s.Logger.Debug("discovery: provider returned no campaigns",
				"provider", p.Name())
			continue
		}
		if s.Persister != nil {
			if err := s.Persister.PersistCampaigns(ctx, camps); err != nil {
				s.Logger.Warn("discovery: persister failed",
					"provider", p.Name(), "err", err)
				continue
			}
		}
		s.Logger.Info("discovery: campaigns persisted",
			"provider", p.Name(), "count", len(camps))
	}
}

func (s *Scraper) loadWhitelist(ctx context.Context) ([]string, error) {
	if s.Whitelist == nil {
		return nil, nil
	}
	return s.Whitelist(ctx)
}

// Compile-time guard that the store.CampaignPersister satisfies our
// local interface (so callers can pass *store.CampaignPersister directly).
var _ CampaignPersister = (*store.CampaignPersister)(nil)
