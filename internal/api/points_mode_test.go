package api

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAccounts_PointsMode(t *testing.T) {
	e, c := accountsEnv(t)
	id := e.createAccount("twitch", "Alpha")
	ctx := context.Background()
	_, tok := e.session()

	mode := func() string {
		t.Helper()
		_, body := e.do("GET", "/api/accounts", "", nil)
		var list []map[string]any
		unmarshal(t, body, &list)
		require.Len(t, list, 1)
		v, ok := list[0]["points_mode"].(string)
		require.True(t, ok, "points_mode missing: %v", list[0])
		return v
	}
	assert.Equal(t, "off", mode(), "off by default")
	before := len(c.changedList())
	live := len(c.settingsList())

	resp, b := e.do("PUT", "/api/accounts/"+id+"/points-mode", tok, map[string]string{"points_mode": "drops_points"})
	require.Equal(t, 204, resp.StatusCode, string(b))
	a, err := e.s.d.Q.GetAccount(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, "drops_points", a.PointsMode)
	assert.Equal(t, "drops_points", mode())
	assert.Equal(t, before, len(c.changedList()), "no rebuild")
	assert.Equal(t, []string{id}, c.settingsList()[live:], "the runner takes the mode live")

	resp, _ = e.do("PUT", "/api/accounts/"+id+"/points-mode", tok, map[string]string{"points_mode": "off"})
	require.Equal(t, 204, resp.StatusCode)
	assert.Equal(t, "off", mode())

	resp, b = e.do("PUT", "/api/accounts/"+id+"/points-mode", tok, map[string]string{"points_mode": "points_only"})
	require.Equal(t, 204, resp.StatusCode, string(b))
	assert.Equal(t, "points_only", mode())
	assert.Equal(t, before, len(c.changedList()), "points only is taken live, no rebuild")
	assert.Equal(t, id, c.settingsList()[len(c.settingsList())-1])

	resp, _ = e.do("PUT", "/api/accounts/"+id+"/points-mode", tok, map[string]string{"points_mode": "off"})
	require.Equal(t, 204, resp.StatusCode)
	assert.Equal(t, "off", mode())

	for _, body := range []any{map[string]any{}, map[string]string{"points_mode": "bogus"}, map[string]any{"points_mode": true}} {
		resp, _ = e.do("PUT", "/api/accounts/"+id+"/points-mode", tok, body)
		assert.Equal(t, 400, resp.StatusCode, "%v", body)
	}
	resp, _ = e.do("PUT", "/api/accounts/nope/points-mode", tok, map[string]string{"points_mode": "off"})
	assert.Equal(t, 404, resp.StatusCode)
	resp, _ = e.do("PUT", "/api/accounts/"+id+"/points-mode", "", map[string]string{"points_mode": "off"})
	assert.Equal(t, 403, resp.StatusCode)
	assert.Equal(t, before, len(c.changedList()), "no request rebuilds")
	assert.Len(t, c.settingsList(), live+4, "refused requests nudge nothing")
}

// Kick has no channel points: setting a points mode on a Kick account is
// refused and the stored mode is left alone.
func TestAccounts_PointsModeKickRefused(t *testing.T) {
	e, c := accountsEnv(t)
	id := e.createAccount("kick", "Kicker")
	_, tok := e.session()
	live := len(c.settingsList())
	for _, m := range []string{"off", "drops_points", "points_only"} {
		resp, b := e.do("PUT", "/api/accounts/"+id+"/points-mode", tok, map[string]string{"points_mode": m})
		assert.Equal(t, 400, resp.StatusCode, m)
		assert.Equal(t, "points_unsupported_platform", jsonField(t, b, "error"), m)
	}
	a, err := e.s.d.Q.GetAccount(context.Background(), id)
	require.NoError(t, err)
	assert.Equal(t, "off", a.PointsMode, "stored mode untouched")
	assert.Len(t, c.settingsList(), live, "refused requests nudge nothing")
}
