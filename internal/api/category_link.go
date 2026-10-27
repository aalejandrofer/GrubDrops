package api

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// ErrNoCategoryAccount is what Deps.LookupCategory returns when no
// signed-in account on that platform can look a category up.
var ErrNoCategoryAccount = errors.New("no signed-in account to look up categories")

var categorySlugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,99}$`)

// parseCategoryLink reads a pasted category link:
// twitch.tv/directory/category/<slug> or kick.com/category/<slug>, with or
// without scheme and www./m. prefixes. isLink is false for anything else,
// which the caller treats as a typed game name.
func parseCategoryLink(raw string) (platform, slug string, isLink bool) {
	raw = strings.TrimSpace(raw)
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", "", false
	}
	host := strings.TrimPrefix(strings.TrimPrefix(strings.ToLower(u.Hostname()), "www."), "m.")
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	switch {
	case host == "twitch.tv" && len(parts) >= 3 && parts[0] == "directory" && parts[1] == "category":
		platform, slug = "twitch", parts[2]
	case host == "kick.com" && len(parts) >= 2 && parts[0] == "category":
		platform, slug = "kick", parts[1]
	default:
		return "", "", false
	}
	slug = strings.ToLower(slug)
	if !categorySlugRe.MatchString(slug) {
		return "", "", false
	}
	return platform, slug, true
}

// categoryName resolves a category slug to its game name, writing the
// error response and returning false when it can't.
func (s *server) categoryName(w http.ResponseWriter, r *http.Request, platform, slug string) (string, bool) {
	if s.d.LookupCategory == nil {
		writeErr(w, http.StatusBadRequest, "bad_request")
		return "", false
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	name, found, err := s.d.LookupCategory(ctx, platform, slug)
	switch {
	case errors.Is(err, ErrNoCategoryAccount):
		writeErr(w, http.StatusConflict, "no_account")
		return "", false
	case err != nil:
		writeErr(w, http.StatusBadGateway, "lookup_failed")
		return "", false
	case !found || strings.TrimSpace(name) == "":
		writeErr(w, http.StatusBadRequest, "unknown_category")
		return "", false
	}
	return strings.TrimSpace(name), true
}
