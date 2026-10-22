package api

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/web"
)

// The console shows both channels of a two-channel force-watch (Twitch), and a
// single one for a one-channel watch.
func TestConsoleShowsBothForceWatchChannels(t *testing.T) {
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
		Channel2: "byalli", Channel2URL: "https://www.twitch.tv/byalli",
	})
	assert.Contains(t, two, "dychacs + byalli", "collapsed card names both channels")
	assert.Contains(t, two, `href="https://www.twitch.tv/byalli"`, "expanded row links the second channel")

	one := render(dashMineCard{ID: "acc", Name: "me", State: "force_watch", StateSub: "mining.force_watching",
		Channel: "dychacs", ChannelURL: "https://www.twitch.tv/dychacs"})
	assert.True(t, strings.Contains(one, "dychacs") && !strings.Contains(one, " + "), "single channel shows no second")
}
