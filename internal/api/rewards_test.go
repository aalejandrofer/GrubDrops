package api

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/store"
)

var rewardNow = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

func newRewardsEnv(t *testing.T) (*resEnv, *store.RewardCodes) {
	t.Helper()
	var rc *store.RewardCodes
	e := newResEnv(t, func(d *Deps) {
		cr, err := store.NewCryptor(testAgeKey)
		require.NoError(t, err)
		rc = store.NewRewardCodes(d.Q, cr)
		d.RewardCodes = rc
		d.Now = func() time.Time { return rewardNow }
	})
	e.s.routesRewards(e.s.protected)
	return e, rc
}

func saveReward(t *testing.T, rc *store.RewardCodes, acct string, r platform.RewardCode) int64 {
	t.Helper()
	got, _, err := rc.Save(context.Background(), acct, r, rewardNow.Add(-time.Hour))
	require.NoError(t, err)
	return got.ID
}

func TestRewardCodes_List(t *testing.T) {
	e, rc := newRewardsEnv(t)
	saveReward(t, rc, "a1", platform.RewardCode{
		Key: "r1", Name: "Blue Creeper Cape", Game: "Minecraft", Code: "AB12-CD34-EF56",
		RedeemURL: "https://www.minecraft.net/redeem", ExpiresAt: rewardNow.Add(72 * time.Hour),
	})
	saveReward(t, rc, "a1", platform.RewardCode{Key: "badge", Name: "Badge"})
	saveReward(t, rc, "a1", platform.RewardCode{Key: "old", Name: "Old", Code: "ZZZZ", ExpiresAt: rewardNow.Add(-time.Minute)})
	gone := saveReward(t, rc, "a1", platform.RewardCode{Key: "gone", Name: "Gone", Code: "YYYY"})
	_, err := rc.SetRedeemed(context.Background(), gone, rewardNow)
	require.NoError(t, err)

	resp, b := e.do("GET", "/api/reward-codes", "", nil)
	require.Equal(t, 200, resp.StatusCode, string(b))
	assert.Equal(t, "no-store", resp.Header.Get("Cache-Control"))
	var got struct {
		Codes []map[string]any `json:"codes"`
	}
	require.NoError(t, json.Unmarshal(b, &got))
	require.Len(t, got.Codes, 1, "only open, unexpired codes")
	c := got.Codes[0]
	assert.Equal(t, "Alice", c["account_name"])
	assert.Equal(t, "a1", c["account_id"])
	assert.Equal(t, "Blue Creeper Cape", c["name"])
	assert.Equal(t, "Minecraft", c["game"])
	assert.Equal(t, "AB12-CD34-EF56", c["code"])
	assert.Equal(t, "https://www.minecraft.net/redeem", c["redeem_url"])
	assert.Equal(t, "2026-11-04T12:00:00Z", c["expires_at"])
	assert.Nil(t, c["received_at"])
	assert.NotNil(t, c["first_seen"])
}

func TestRewardCodes_EmptyIsList(t *testing.T) {
	e, _ := newRewardsEnv(t)
	resp, b := e.do("GET", "/api/reward-codes", "", nil)
	require.Equal(t, 200, resp.StatusCode)
	assert.JSONEq(t, `{"codes":[]}`, string(b))
}

func TestRewardCodes_Mark(t *testing.T) {
	e, rc := newRewardsEnv(t)
	id := saveReward(t, rc, "a1", platform.RewardCode{Key: "r1", Name: "Cape", Code: "AB12"})
	id2 := saveReward(t, rc, "a1", platform.RewardCode{Key: "r2", Name: "Hat", Code: "CD34"})
	_, tok := e.session()
	path := func(id int64) string { return "/api/reward-codes/" + jsonInt(id) }

	resp, b := e.do("PUT", path(id), tok, map[string]string{"state": "redeemed"})
	require.Equal(t, 204, resp.StatusCode, string(b))
	resp, _ = e.do("PUT", path(id2), tok, map[string]string{"state": "dismissed"})
	require.Equal(t, 204, resp.StatusCode)

	r, ok, err := rc.Get(context.Background(), id)
	require.NoError(t, err)
	require.True(t, ok)
	assert.True(t, rewardNow.Equal(r.RedeemedAt))
	r, _, _ = rc.Get(context.Background(), id2)
	assert.True(t, rewardNow.Equal(r.DismissedAt))

	_, b = e.do("GET", "/api/reward-codes", "", nil)
	assert.JSONEq(t, `{"codes":[]}`, string(b))
}

func TestRewardCodes_MarkErrors(t *testing.T) {
	e, rc := newRewardsEnv(t)
	id := saveReward(t, rc, "a1", platform.RewardCode{Key: "r1", Name: "Cape", Code: "AB12"})
	_, tok := e.session()
	p := "/api/reward-codes/" + jsonInt(id)

	resp, b := e.do("PUT", p, "", map[string]string{"state": "redeemed"})
	assert.Equal(t, 403, resp.StatusCode)
	assert.JSONEq(t, `{"error":"csrf"}`, string(b))

	resp, _ = e.do("PUT", p, tok, map[string]string{"state": "lost"})
	assert.Equal(t, 400, resp.StatusCode)
	resp, _ = e.do("PUT", p, tok, map[string]any{"state": "redeemed", "extra": 1})
	assert.Equal(t, 400, resp.StatusCode)
	resp, _ = e.do("PUT", "/api/reward-codes/999", tok, map[string]string{"state": "redeemed"})
	assert.Equal(t, 404, resp.StatusCode)
	resp, _ = e.do("PUT", "/api/reward-codes/abc", tok, map[string]string{"state": "redeemed"})
	assert.Equal(t, 404, resp.StatusCode)

	r, _, _ := rc.Get(context.Background(), id)
	assert.True(t, r.Open(), "nothing changed")
}

func TestRewardCodes_NeedsLogin(t *testing.T) {
	e := newEnv(t)
	e.s.routesRewards(e.s.protected)
	resp, b := e.do("GET", "/api/reward-codes", "", nil)
	assert.Equal(t, 401, resp.StatusCode)
	assert.NotContains(t, string(b), "code\":")
	_, tok := e.session()
	resp, _ = e.do("PUT", "/api/reward-codes/1", tok, map[string]string{"state": "redeemed"})
	assert.Equal(t, 401, resp.StatusCode)
}

func TestRewardCodes_Unavailable(t *testing.T) {
	e := newResEnv(t, nil)
	e.s.routesRewards(e.s.protected)
	resp, _ := e.do("GET", "/api/reward-codes", "", nil)
	assert.Equal(t, 503, resp.StatusCode)
	_, tok := e.session()
	resp, _ = e.do("PUT", "/api/reward-codes/1", tok, map[string]string{"state": "redeemed"})
	assert.Equal(t, 503, resp.StatusCode)
}

func jsonInt(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}
