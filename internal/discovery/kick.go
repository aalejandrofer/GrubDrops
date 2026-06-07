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

// KickScraper reuses the chromedp sidecar (via the existing kick.Backend)
// to enumerate active drop campaigns. Per ref_kickdropsminer.md, Kick is
// browser-only — Cloudflare's JS challenge defeats every pure-HTTP path
// — so a working sidecar is non-negotiable. When the sidecar isn't
// configured (MINER_BROWSER_URL empty) the registry never registers a
// Kick backend, so this Scraper's Backend is nil and Scrape no-ops.
//
// Like TwitchScraper we borrow ONE enabled Kick account's session (the
// first ListEnabledAccounts row whose platform is "kick"). The session
// pins the Cloudflare cookie + xsrf token the sidecar needs to land on
// kick.com without tripping the challenge. Without any Kick account
// logged in we cannot scrape — Cloudflare will block the navigation.
//
// Whitelist note: kick.Backend.ListActiveCampaigns scrapes
// https://kick.com/drops via the sidecar and surfaces every active
// drop campaign Kick advertises, regardless of game. The per-account
// GameFilter on the session prunes results down to whitelisted games
// inside the backend (and we re-apply the same filter here as a
// belt-and-suspenders guard).
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
		// No sidecar configured (MINER_BROWSER_URL empty) — graceful
		// no-op so the Scraper just logs once and continues.
		return nil, nil
	}
	accountID, sess, ok, err := s.Source(ctx)
	if err != nil {
		return nil, fmt.Errorf("kick scraper: load session: %w", err)
	}
	if !ok {
		// No Kick account logged in — Cloudflare will reject the
		// sidecar navigation, so don't even try.
		return nil, nil
	}
	if accountID != "" {
		sess.AccountID = accountID
	}
	allow := buildAllowList(whitelist)
	sess.GameFilter = allow

	camps, err := s.Backend.ListActiveCampaigns(ctx, sess)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return nil, fmt.Errorf("kick ListActiveCampaigns: %w", err)
	}
	out := make([]platform.Campaign, 0, len(camps))
	for _, c := range camps {
		if allow(c.Game) {
			out = append(out, c)
		}
	}
	return out, nil
}

