package twitch

// ClientTV marks a session minted by the Twitch for TV OAuth client.
// Twitch blocked device-code login for the Android client ~2026-09-18
// (#48, DevilXD #1165); the TV client still accepts it.
const ClientTV = "tv"

type clientProfile struct {
	ID        string
	UserAgent string
}

var (
	profileAndroid = clientProfile{ID: clientID, UserAgent: userAgent}
	profileTV      = clientProfile{
		ID:        "ue6666qo983tsx6so1t0vnawi233wa",
		UserAgent: "Mozilla/5.0 (Linux; Android 9; SHIELD Android TV) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0 Safari/537.36",
	}
)

// profileFor maps a persisted Session.ClientID to its request profile.
// Empty/unknown = legacy Android, so pre-#48 sessions are untouched.
func profileFor(id string) clientProfile {
	if id == ClientTV {
		return profileTV
	}
	return profileAndroid
}
