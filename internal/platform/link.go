package platform

import (
	"net/url"
	"strings"
)

// ExternalLinkURL reports whether raw is an http(s) account-link page hosted
// off Twitch, i.e. a publisher's connect page (Riot, Ubisoft, Mojang, ...).
// Twitch sends an empty accountLinkURL, or a twitch.tv drops page, for
// campaigns that need no link, and leaves self.isAccountConnected false on
// many of them, so only an off-Twitch URL means a link is required.
func ExternalLinkURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if host == "" {
		return false
	}
	return host != "twitch.tv" && !strings.HasSuffix(host, ".twitch.tv")
}

// TwitchNeedsLink reports whether a Twitch campaign's drops cannot be claimed
// until the account links a publisher account: Twitch says it is not
// connected and the link page is a publisher's.
func TwitchNeedsLink(c Campaign) bool {
	return !c.AccountLinked && ExternalLinkURL(c.AccountLinkURL)
}
