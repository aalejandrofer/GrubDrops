package api

import (
	"context"

	"github.com/aalejandrofer/grubdrops/internal/i18n"
	"github.com/aalejandrofer/grubdrops/internal/platform/twitch"
	"github.com/aalejandrofer/grubdrops/internal/store"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

// tvDiscoveryStatus reports whether at least one ENABLED Twitch account's
// session was minted by the TV client (Session.ClientID == twitch.ClientTV —
// every login since Twitch blocked the Android device-code client, #48) and,
// when so, whether any such account's effective whitelist is empty. A TV
// session can't see Twitch's drops dashboard at all, so its campaign
// discovery depends entirely on the whitelist (own games, falling back to
// the global list — the same resolution gameNamesForAccount already uses);
// an empty one means the account finds nothing.
//
// Cheap by construction: scans ENABLED accounts only (few, per
// buildDashAlerts' own assumption) and decrypts each Twitch session once.
// Returns booleans only — never session/token material — so callers can
// pass the result straight to a template without risk.
func tvDiscoveryStatus(ctx context.Context, q *gen.Queries, sessions *store.SessionStore) (anyTV bool, whitelistEmpty bool) {
	if q == nil || sessions == nil {
		return false, false
	}
	accs, err := q.ListEnabledAccounts(ctx)
	if err != nil {
		return false, false
	}
	for _, a := range accs {
		if a.Platform != "twitch" {
			continue
		}
		sess, ok, err := sessions.Get(ctx, a.ID)
		if err != nil || !ok {
			continue
		}
		if sess.ClientID != twitch.ClientTV {
			continue
		}
		anyTV = true
		if len(gameNamesForAccount(ctx, q, a.ID)) == 0 {
			whitelistEmpty = true
		}
	}
	return anyTV, whitelistEmpty
}

// tvDiscoveryAlert builds the top-of-page banner warning that Twitch's
// TV-client login can't see the drops dashboard, so GrubDrops only finds
// campaigns for whitelisted games. It reuses the existing dashAlert
// component (the same one handlers_dashboard.go renders for needs_auth,
// etc.) so the dashboard and /drops share one code path and one CSS
// component. Returns nil when no enabled Twitch account has a TV session.
func tvDiscoveryAlert(ctx context.Context, q *gen.Queries, sessions *store.SessionStore, lang string) *dashAlert {
	anyTV, empty := tvDiscoveryStatus(ctx, q, sessions)
	if !anyTV {
		return nil
	}
	return &dashAlert{
		Kind:    "tv_discovery",
		Account: i18n.T(lang, "dashboard.tv_discovery_title"),
		URL:     "/priority",
		Action:  i18n.T(lang, "dashboard.action_whitelist_games"),
		Warning: empty,
	}
}
