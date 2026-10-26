package legacyapi

import (
	"context"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

// fetchAndStoreAvatar fetches the account's profile picture from the backend
// (if it supports platform.AvatarFetcher) and persists it on the account row.
// Best-effort: any failure is logged and swallowed so it never blocks login.
// Called on login; the periodic auth-health sweep keeps it fresh thereafter.
func fetchAndStoreAvatar(ctx context.Context, q *gen.Queries, backend platform.Backend, accountID string, sess platform.Session) {
	fetcher, ok := backend.(platform.AvatarFetcher)
	if !ok {
		return
	}
	fctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	avatarURL, err := fetcher.FetchAvatar(fctx, sess)
	if err != nil {
		slog.Debug("avatar fetch failed", "account", accountID, "err", err)
		return
	}
	if avatarURL == "" {
		return
	}
	if err := q.UpdateAccountAvatar(ctx, gen.UpdateAccountAvatarParams{
		AvatarUrl: avatarURL,
		UpdatedAt: time.Now().Unix(),
		ID:        accountID,
	}); err != nil {
		slog.Warn("persist avatar failed", "account", accountID, "err", err)
		return
	}
	slog.Info("avatar stored", "account", accountID)
}

// avatarSrc renders the <img> src for a stored avatar value, choosing direct
// embedding for Twitch (public static-cdn.jtvnw.net) versus the /img/kick
// proxy for Kick (Cloudflare 403s direct hotlinks). Returns "" when there is
// no avatar, so callers fall back to the letter circle.
func avatarSrc(platformName, avatarURL string) string {
	avatarURL = strings.TrimSpace(avatarURL)
	if avatarURL == "" {
		return ""
	}
	if platformName == "kick" {
		// Pass the stored value through the existing image proxy. The
		// handler trusts only the PATH, so an absolute or relative value
		// both work; it serves user avatars from files.kick.com.
		return "/img/kick?u=" + url.QueryEscape(avatarURL)
	}
	// Twitch (and any other platform): direct CDN URL.
	return avatarURL
}
