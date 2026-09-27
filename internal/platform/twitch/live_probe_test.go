//go:build live

package twitch

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// TestLive_DetailsSelf dumps raw DropCampaignDetails for a campaign the
// account is enrolled in (ideally one fully claimed), to confirm
// timeBasedDrops[].self carries isClaimed + currentMinutesWatched.
//
//	GRUB_LIVE_TWITCH_TOKEN=... GRUB_LIVE_CAMPAIGN_ID=... \
//	  go test -tags live -run TestLive_DetailsSelf -v ./internal/platform/twitch/
func TestLive_DetailsSelf(t *testing.T) {
	tok, camp := os.Getenv("GRUB_LIVE_TWITCH_TOKEN"), os.Getenv("GRUB_LIVE_CAMPAIGN_ID")
	if tok == "" || camp == "" {
		t.Skip("set GRUB_LIVE_TWITCH_TOKEN, GRUB_LIVE_CAMPAIGN_ID")
	}
	ctx := context.Background()
	b := New()
	login, err := b.disc.resolveCurrentLogin(ctx, platform.Session{AccessToken: tok})
	require.NoError(t, err)
	var raw json.RawMessage
	require.NoError(t, b.c.gql(ctx, tok, OpDropCampaignDetails,
		map[string]any{"dropID": camp, "channelLogin": login}, &raw))
	t.Logf("%s", raw)
}
