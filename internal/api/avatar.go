package api

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

// avatarSrc returns the URL the SPA uses for a stored avatar: Twitch embeds
// the public CDN URL, Kick goes through the /api/img/kick proxy (Cloudflare
// 403s direct hotlinks). Empty when there is no avatar.
func avatarSrc(platformName, avatarURL string) string {
	avatarURL = strings.TrimSpace(avatarURL)
	if avatarURL == "" {
		return ""
	}
	if platformName == "kick" {
		return "/api/img/kick?u=" + url.QueryEscape(avatarURL)
	}
	return avatarURL
}
