package twitch

import (
	"net/url"
	"strings"
)

// Box art size the UI asks for: Twitch's 3:4 directory card.
const boxArtSize = "144x192"

const boxArtBase = "https://static-cdn.jtvnw.net/ttv-boxart/"

// BoxArtURL is a game's box art. raw is the GQL game.boxArtURL, whose size
// may be the {width}x{height} template; it wins when it is an https Twitch
// CDN URL. Otherwise the URL is built from the game id. IGDB-backed games,
// which is nearly every game added since 2021, only answer the "_IGDB"
// form: the bare "<id>-WxH" form redirects them to the placeholder. Empty
// when neither is known.
func BoxArtURL(raw, gameID string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.ReplaceAll(raw, "{width}x{height}", boxArtSize)
	raw = strings.ReplaceAll(raw, "%7Bwidth%7Dx%7Bheight%7D", boxArtSize)
	if u, err := url.Parse(raw); err == nil && u.Scheme == "https" && u.Host == "static-cdn.jtvnw.net" {
		return raw
	}
	if gameID = strings.TrimSpace(gameID); gameID != "" {
		return boxArtBase + url.PathEscape(gameID) + "_IGDB-" + boxArtSize + ".jpg"
	}
	return ""
}

// BoxArtByName is the name-keyed box art, which resolves only for older,
// non-IGDB games. Last resort when no id is known.
func BoxArtByName(name string) string {
	if name = strings.TrimSpace(name); name == "" {
		return ""
	}
	return boxArtBase + url.PathEscape(name) + "-" + boxArtSize + ".jpg"
}
