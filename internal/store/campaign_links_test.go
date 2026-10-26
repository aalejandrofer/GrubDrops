package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCampaignLinks_SetClearRoundTrip(t *testing.T) {
	_, q := openV2(t)
	seedAccount(t, q, "a1")
	seedAccount(t, q, "a2")
	l := NewCampaignLinks(q)
	ctx := context.Background()

	// campaign_id has no campaigns row: v2 never writes that table.
	require.NoError(t, l.SetNeedsLink(ctx, "a1", "camp1", "https://auth.riotgames.com/authorize?x=1"))
	require.NoError(t, l.SetNeedsLink(ctx, "a2", "camp1", "javascript:alert(1)"))

	m, err := l.NeedsLink(ctx)
	require.NoError(t, err)
	require.Equal(t, map[string]string{
		"a1:camp1": "https://auth.riotgames.com/authorize?x=1",
		"a2:camp1": "",
	}, m)

	require.NoError(t, l.ClearNeedsLink(ctx, "a1", "camp1"))
	m, err = NewCampaignLinks(q).NeedsLink(ctx)
	require.NoError(t, err)
	require.Equal(t, map[string]string{"a2:camp1": ""}, m)
}
