package api

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/web"
)

// The console shows every channel of a force-watch and links each one.
func TestConsoleShowsForceWatchChannels(t *testing.T) {
	tmpl, err := web.Templates()
	require.NoError(t, err)
	render := func(c dashMineCard) string {
		var buf bytes.Buffer
		require.NoError(t, tmpl.ExecuteTemplate(&buf, "dashboard_mining_columns", dashMiningColumns{Twitch: []dashMineCard{c}}))
		return buf.String()
	}

	two := render(dashMineCard{
		ID: "acc", Name: "me", State: "force_watch", StateSub: "mining.force_watching",
		Channel: "dychacs", ChannelURL: "https://www.twitch.tv/dychacs",
		ExtraChannels: []dashChannel{{Name: "byalli", URL: "https://www.twitch.tv/byalli"}},
	})
	assert.Contains(t, two, "dychacs + byalli")
	assert.Contains(t, two, `href="https://www.twitch.tv/byalli"`)

	three := render(dashMineCard{
		ID: "acc", Name: "me", State: "force_watch", StateSub: "mining.force_watching",
		Channel: "dychacs", ChannelURL: "https://www.twitch.tv/dychacs",
		ExtraChannels: []dashChannel{
			{Name: "byalli", URL: "https://www.twitch.tv/byalli"},
			{Name: "third", URL: "https://www.twitch.tv/third"},
		},
	})
	assert.Contains(t, three, "dychacs + byalli + third")
	assert.Contains(t, three, `href="https://www.twitch.tv/third"`)

	one := render(dashMineCard{ID: "acc", Name: "me", State: "force_watch", StateSub: "mining.force_watching",
		Channel: "dychacs", ChannelURL: "https://www.twitch.tv/dychacs"})
	assert.True(t, strings.Contains(one, "dychacs") && !strings.Contains(one, " + "), "single channel shows no second")
}

func TestConsoleShowsChannelPointsPerWatchedChannel(t *testing.T) {
	tmpl, err := web.Templates()
	require.NoError(t, err)
	var buf bytes.Buffer
	require.NoError(t, tmpl.ExecuteTemplate(&buf, "dashboard_mining_columns", dashMiningColumns{Twitch: []dashMineCard{{
		ID: "acc", Name: "me", State: "force_watch",
		ChannelBalances: []dashChannel{
			{Name: "streamer-a", URL: "https://www.twitch.tv/streamer-a", Points: "1250", PointsUpdated: "1h ago", PointsAvailable: true},
			{Name: "streamer-b", URL: "https://www.twitch.tv/streamer-b"},
		},
	}}}))

	assert.Contains(t, buf.String(), "streamer-a")
	assert.Contains(t, buf.String(), "1250")
	assert.Contains(t, buf.String(), "streamer-b")
	assert.Contains(t, buf.String(), "balance unavailable")
	assert.Contains(t, buf.String(), "1h ago")
}
