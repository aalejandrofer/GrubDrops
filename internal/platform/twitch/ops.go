package twitch

// Captured 2026-06-04 from DevilXD/TwitchDropsMiner@c5e6286c41dab46e1189333eede734e3b1995dc4. See
// docs/superpowers/notes/2026-06-04-twitch-ops-source.md for source links.
// Refresh these constants if production sees PersistedQueryNotFound errors.
const (
	clientID  = "kimne78kx3ncx6brgo4mv6wki5h1ko"
	userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.0.0 Safari/537.36"

	gqlEndpoint   = "https://gql.twitch.tv/gql"
	integrityURL  = "https://gql.twitch.tv/integrity"
	deviceAuthURL = "https://id.twitch.tv/oauth2/device"
	tokenURL      = "https://id.twitch.tv/oauth2/token"

	// minuteWatchedURL is the same as gqlEndpoint: as of DevilXD@c5e6286, the heartbeat is
	// sent as a GQL mutation (SendEvents / sendSpadeEvents) rather than to a static analytics
	// endpoint. See docs/superpowers/notes/2026-06-04-twitch-ops-source.md for payload shape.
	minuteWatchedURL = "https://gql.twitch.tv/gql"
)

// Operation identifies a persisted GraphQL operation.
// Name is the operationName sent on the wire; Hash is the sha256Hash for the persisted query.
type Operation struct {
	Name string
	Hash string
}

// DevilXD key → operationName (wire) / sha256Hash.
// Source: constants.py GQL_QUERIES dict, commit c5e6286.
var (
	// OpGetStreamInfo checks whether a channel is live and fetches stream metadata.
	// DevilXD key: "GetStreamInfo". Equivalent to plan-spec "WithIsStreamLiveQuery".
	OpGetStreamInfo = Operation{
		Name: "VideoPlayerStreamInfoOverlayChannel",
		Hash: "198492e0857f6aedead9665c81c5a06d67b25b58034649687124083ff288597d",
	}

	// OpClaimDrop claims a completed drop reward.
	// DevilXD key: "ClaimDrop". On-wire name differs from dict key.
	OpClaimDrop = Operation{
		Name: "DropsPage_ClaimDropRewards",
		Hash: "a455deea71bdc9015b78eb49f4acfbce8baa7ccbedd28e549bb025bd0f751930",
	}

	// OpInventory returns all in-progress drop campaigns for the authenticated user.
	// DevilXD key: "Inventory".
	OpInventory = Operation{
		Name: "Inventory",
		Hash: "d86775d0ef16a63a33ad52e80eaff963b2d5b72fada7c991504a57496e1d8e4b",
	}

	// OpCampaigns lists all available drop campaigns.
	// DevilXD key: "Campaigns". Equivalent to plan-spec "DropsPage_ContentList".
	OpCampaigns = Operation{
		Name: "ViewerDropsDashboard",
		Hash: "5a4da2ab3d5b47c9f9ce864e727b2cb346af1e3ea8b897fe8f704a97ff017619",
	}

	// OpDropCampaignDetails returns extended information about a particular campaign.
	// DevilXD key: "CampaignDetails".
	OpDropCampaignDetails = Operation{
		Name: "DropCampaignDetails",
		Hash: "039277bf98f3130929262cc7c6efd9c141ca3749cb6dca442fc8ead9a53f77c1",
	}

	// OpPlaybackAccessToken fetches the stream playback access token required to get an HLS URL.
	// DevilXD key: "PlaybackAccessToken".
	OpPlaybackAccessToken = Operation{
		Name: "PlaybackAccessToken",
		Hash: "ed230aa1e33e07eebb8928504583da78a5173989fadfb1ac94be06a04f3cdbe9",
	}

	// OpAvailableDrops returns drop campaigns available for a particular channel.
	// DevilXD key: "AvailableDrops".
	OpAvailableDrops = Operation{
		Name: "DropsHighlightService_AvailableDrops",
		Hash: "782dad0f032942260171d2d80a654f88bdd0c5a9dddc392e9bc92218a0f42d20",
	}

	// OpCurrentDrop returns the current in-progress drop state for a watched channel.
	// DevilXD key: "CurrentDrop".
	OpCurrentDrop = Operation{
		Name: "DropCurrentSessionContext",
		Hash: "4d06b702d25d652afb9ef835d2a550031f1cf762b193523a92166f40ea3d142b",
	}
)
