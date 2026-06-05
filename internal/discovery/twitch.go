package discovery

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/aalejandrofer/dropsminer/internal/platform"
	"github.com/aalejandrofer/dropsminer/internal/store"
	"github.com/aalejandrofer/dropsminer/internal/store/gen"
)

// twitchSessionSource returns (account_id, session, ok). ok=false means
// no enabled Twitch account is currently loginable; callers must treat
// that as "no-op, log a warning".
type twitchSessionSource func(ctx context.Context) (string, platform.Session, bool, error)

// TwitchScraper borrows the first enabled Twitch account's session and
// proxies ListActiveCampaigns to whichever Twitch backend the registry
// has (HTTP backend, or BrowserBackend when MINER_TWITCH_BROWSER=1 —
// the latter is required when Twitch's integrity wall is up). Scrape
// applies a GameFilter built from the union whitelist so the backend
// short-circuits non-whitelisted games BEFORE the per-campaign detail
// fetch fan-out — saves bandwidth and keeps the project_goal.md rule
// honored (never consider non-whitelisted drops).
//
// When no enabled Twitch account exists or its session is missing /
// expired, Scrape returns (nil, nil) — the Scraper logs once and moves
// on. There is intentionally no fallback to a fully anonymous Twitch
// flow because dropCampaigns is gated on currentUser; without a session
// the gql call returns null.
type TwitchScraper struct {
	Backend platform.Backend
	Source  twitchSessionSource
}

// NewTwitchScraper wires a scraper against the running registry. The
// returned Provider's Name() is "twitch".
func NewTwitchScraper(backend platform.Backend, source twitchSessionSource) *TwitchScraper {
	return &TwitchScraper{Backend: backend, Source: source}
}

// NewTwitchScraperFromStore is the production wiring: it locates the
// first enabled Twitch account in the accounts table, fetches its
// session from the SessionStore, and uses that to satisfy Scrape. If
// the session is expired and has a refresh token, the backend's
// RefreshSession is called (but the refreshed session is NOT persisted
// here — that's the watcher's job).
//
// `backend` must be the Twitch backend (or BrowserBackend) the watchers
// also use, so the per-account sidecar tab can be reused.
func NewTwitchScraperFromStore(q *gen.Queries, sessions *store.SessionStore, backend platform.Backend) *TwitchScraper {
	return &TwitchScraper{
		Backend: backend,
		Source: func(ctx context.Context) (string, platform.Session, bool, error) {
			accs, err := q.ListEnabledAccounts(ctx)
			if err != nil {
				return "", platform.Session{}, false, err
			}
			for _, a := range accs {
				if a.Platform != "twitch" {
					continue
				}
				s, ok, err := sessions.Get(ctx, a.ID)
				if err != nil {
					return "", platform.Session{}, false, err
				}
				if !ok {
					continue
				}
				s.AccountID = a.ID
				return a.ID, s, true, nil
			}
			return "", platform.Session{}, false, nil
		},
	}
}

func (s *TwitchScraper) Name() string { return "twitch" }

func (s *TwitchScraper) Scrape(ctx context.Context, whitelist []string) ([]platform.Campaign, error) {
	if s == nil || s.Backend == nil || s.Source == nil {
		return nil, nil
	}
	accountID, sess, ok, err := s.Source(ctx)
	if err != nil {
		return nil, fmt.Errorf("twitch scraper: load session: %w", err)
	}
	if !ok {
		// No twitch account / no session — graceful no-op per task spec.
		return nil, nil
	}
	if accountID != "" {
		sess.AccountID = accountID
	}
	// GameFilter gates the per-campaign DropCampaignDetails fan-out so
	// bandwidth stays bounded by whitelist size. Non-whitelisted
	// campaigns are still emitted (without benefits) so the /drops
	// Discoverable tab can list them.
	sess.GameFilter = buildAllowList(whitelist)

	camps, err := s.Backend.ListActiveCampaigns(ctx, sess)
	if err != nil {
		// Cancellation propagates as-is; everything else is logged by
		// the Scraper.
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return nil, fmt.Errorf("twitch ListActiveCampaigns: %w", err)
	}
	// Emit every campaign the backend returned — whitelisted with full
	// benefits, non-whitelisted as shell rows (Benefits empty, set by
	// listActive when GameFilter rejected the game). The /drops
	// Discoverable tab consumes the shell rows; the watcher's mining
	// loop ignores them via its own AllowGame check.
	return camps, nil
}

// buildAllowList collapses a slice of whitelist tokens (already
// lowercased by NewQueriesWhitelist, but normalised again defensively)
// into a closure that returns true iff the game's lowercased name
// matches. Backends pass either game.id or game.name; we accept both.
func buildAllowList(whitelist []string) func(string) bool {
	if len(whitelist) == 0 {
		// Empty whitelist = reject everything. This is the safe default
		// — without an opt-in we never scrape.
		return func(string) bool { return false }
	}
	set := make(map[string]struct{}, len(whitelist))
	for _, g := range whitelist {
		set[strings.ToLower(strings.TrimSpace(g))] = struct{}{}
	}
	return func(game string) bool {
		_, ok := set[strings.ToLower(strings.TrimSpace(game))]
		return ok
	}
}
