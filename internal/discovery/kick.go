package discovery

import (
	"context"
	"errors"
	"fmt"

	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/store"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

// kickSessionSource is the Kick analogue of twitchSessionSource. ok=false
// means no enabled Kick account has a usable session right now.
type kickSessionSource func(ctx context.Context) (string, platform.Session, bool, error)

// KickScraper enumerates active drop campaigns through the shared kick.Backend
// (pure-HTTP utls client; the chromedp sidecar is no longer used for Kick
// data — Kick's API 403s CDP browsers but accepts the Chrome TLS fingerprint).
// The backend needs no sidecar, but the registry only registers it when a
// Kick account exists, so a nil Backend still means "graceful no-op".
//
// Like TwitchScraper we borrow ONE enabled Kick account's session (the
// first ListEnabledAccounts row whose platform is "kick") — the authed
// /api/v1/drops/campaigns call needs its cookies.
//
// Whitelist note: the session's GameFilter (built from the whitelist union)
// makes the backend emit non-whitelisted campaigns as SHELL rows — status +
// game + name, no Benefits — exactly like the Twitch scraper. Those shells
// persist to the campaigns table so the /drops Discoverable tab can list
// them and the user can opt the game in; the watcher's own AllowGame keeps
// them out of mining.
type KickScraper struct {
	Backend platform.Backend
	Source  kickSessionSource
}

// NewKickScraper wires a Provider against a backend + session source.
func NewKickScraper(backend platform.Backend, source kickSessionSource) *KickScraper {
	return &KickScraper{Backend: backend, Source: source}
}

// NewKickScraperFromStore builds the production wiring. The session
// comes from the first enabled "kick" account.
func NewKickScraperFromStore(q *gen.Queries, sessions *store.SessionStore, backend platform.Backend) *KickScraper {
	return &KickScraper{
		Backend: backend,
		Source: func(ctx context.Context) (string, platform.Session, bool, error) {
			accs, err := q.ListEnabledAccounts(ctx)
			if err != nil {
				return "", platform.Session{}, false, err
			}
			for _, a := range accs {
				if a.Platform != "kick" {
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

func (s *KickScraper) Name() string { return "kick" }

func (s *KickScraper) Scrape(ctx context.Context, whitelist []string) ([]platform.Campaign, error) {
	if s == nil || s.Backend == nil || s.Source == nil {
		// No Kick backend registered — graceful no-op so the Scraper just
		// logs once and continues.
		return nil, nil
	}
	accountID, sess, ok, err := s.Source(ctx)
	if err != nil {
		return nil, fmt.Errorf("kick scraper: load session: %w", err)
	}
	if !ok {
		// No Kick account logged in — nothing to scrape with.
		return nil, nil
	}
	if accountID != "" {
		sess.AccountID = accountID
	}
	sess.GameFilter = buildAllowList(whitelist)

	camps, err := s.Backend.ListActiveCampaigns(ctx, sess)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return nil, fmt.Errorf("kick ListActiveCampaigns: %w", err)
	}
	// Emit every campaign the backend returned — whitelisted with full
	// benefits, non-whitelisted as shell rows (Benefits empty, set by
	// ListActiveCampaigns when GameFilter rejected the game). The /drops
	// Discoverable tab consumes the shell rows; the watcher's mining loop
	// ignores them via its own AllowGame check.
	return camps, nil
}
