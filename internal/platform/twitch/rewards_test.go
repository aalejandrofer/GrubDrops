package twitch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// The real notification text, with a fake code.
const rewardNoticeBody = "You received a reward for : Blue Creeper Cape! Click here to redeem: AB12-CD34-EF56."

func TestParseRewardNotice(t *testing.T) {
	for name, tc := range map[string]struct {
		body, wantName, wantCode string
		ok                       bool
	}{
		"real text":      {rewardNoticeBody, "Blue Creeper Cape", "AB12-CD34-EF56", true},
		"markdown name":  {"You received a reward for : **Blue Creeper Cape**! Click here to redeem: AB12-CD34-EF56", "Blue Creeper Cape", "AB12-CD34-EF56", true},
		"no space colon": {"You received a reward for: Aurora Cape! Click here to redeem: ab12-cd34-ef56.", "Aurora Cape", "AB12-CD34-EF56", true},
		"5x5 code":       {"Your code: 7Y64H-GKPXP-YKHMG-H7634-79D2Z is ready", "", "7Y64H-GKPXP-YKHMG-H7634-79D2Z", true},
		"no code":        {"You earned a badge for watching! Click here to see it.", "", "", false},
		"plain text":     {"StreamerX is live: Playing Minecraft", "", "", false},
	} {
		t.Run(name, func(t *testing.T) {
			n, c, ok := parseRewardNotice(tc.body)
			assert.Equal(t, tc.ok, ok)
			assert.Equal(t, tc.wantName, n)
			assert.Equal(t, tc.wantCode, c)
		})
	}
}

func TestRedeemLink(t *testing.T) {
	assert.Equal(t, "https://www.twitch.tv/drops/inventory", redeemLink("", "nope", "https://www.twitch.tv/drops/inventory"))
	assert.Equal(t, "https://redeem.example/x?y=1", redeemLink(" https://redeem.example/x?y=1 "))
	assert.Empty(t, redeemLink("javascript:alert(1)", "/relative", "ftp://x.example/a"))
}

func TestInventoryReward_MayHaveCode(t *testing.T) {
	assert.True(t, inventoryReward{Game: "Minecraft", URL: "https://www.twitch.tv/x"}.mayHaveCode())
	assert.True(t, inventoryReward{URL: "https://www.minecraft.net/redeem"}.mayHaveCode())
	assert.False(t, inventoryReward{URL: "https://help.twitch.tv/badge"}.mayHaveCode(), "game-less twitch link is a badge")
	assert.False(t, inventoryReward{}.mayHaveCode())
}

type rewardReq struct {
	Op     string
	Hash   string
	Vars   map[string]any
	Header http.Header
}

// rewardServer answers each op with replies[op](vars) and records requests.
func rewardServer(t *testing.T, replies map[string]func(vars map[string]any) string) (*httptest.Server, func() []rewardReq) {
	t.Helper()
	var mu sync.Mutex
	var seen []rewardReq
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			OperationName string         `json:"operationName"`
			Variables     map[string]any `json:"variables"`
			Extensions    struct {
				PersistedQuery struct {
					Sha256Hash string `json:"sha256Hash"`
				} `json:"persistedQuery"`
			} `json:"extensions"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		mu.Lock()
		seen = append(seen, rewardReq{req.OperationName, req.Extensions.PersistedQuery.Sha256Hash, req.Variables, r.Header.Clone()})
		mu.Unlock()
		f, ok := replies[req.OperationName]
		if !ok {
			t.Errorf("unexpected op %q", req.OperationName)
			http.Error(w, "unexpected", http.StatusBadRequest)
			return
		}
		_, _ = fmt.Fprint(w, f(req.Variables))
	}))
	t.Cleanup(srv.Close)
	return srv, func() []rewardReq {
		mu.Lock()
		defer mu.Unlock()
		return append([]rewardReq(nil), seen...)
	}
}

func fixed(s string) func(map[string]any) string { return func(map[string]any) string { return s } }

// Recorded shapes: notifications with one reward code and one unrelated
// notification, and an inventory with a Minecraft cape quest plus a badge.
const notificationsFixture = `{"data":{"currentUser":{"id":"1","notifications":{"pageInfo":{"hasNextPage":false},"edges":[
 {"cursor":"c1","node":{"id":"notif-1","type":"user_drop_reward_reminder_notification","body":"You received a reward for : Blue Creeper Cape! Click here to redeem: AB12-CD34-EF56.","createdAt":"2026-10-13T10:00:00Z","isRead":false,
   "actions":[{"id":"a1","type":"click","url":"https://www.twitch.tv/drops/inventory?redeem=r-cape"}]}},
 {"cursor":"c2","node":{"id":"notif-2","type":"live_notification","body":"StreamerX went live","createdAt":"2026-10-13T09:00:00Z","actions":[]}}
]}}}}`

const inventoryRewardsFixture = `{"data":{"currentUser":{"id":"1","inventory":{"dropCampaignsInProgress":[],"completedRewardCampaigns":[
 {"id":"camp-cape","name":"Minecraft Creeper Cape","externalURL":"https://www.minecraft.net/","game":{"displayName":"Minecraft","name":"Minecraft"},
   "rewards":[{"id":"r-cape","name":"Blue Creeper Cape","redemptionURL":"https://www.twitch.tv/"}]},
 {"id":"camp-badge","name":"Watch Quest","game":null,
   "rewards":[{"id":"r-badge","name":"Quest Badge","redemptionURL":"https://help.twitch.tv/badges"}]}
]}}}}`

func codeModal(vars map[string]any) string {
	if vars["rewardID"] == "r-cape" && vars["rewardCampaignID"] == "camp-cape" {
		return `{"data":{"currentUser":{"inventory":{"rewardValue":{"value":"AB12-CD34-EF56","rewardID":"r-cape","rewardCampaignID":"camp-cape","expiresAt":"2026-10-20T06:58:59.999Z"}}}}}`
	}
	return `{"errors":[{"message":"server error"}],"data":{"currentUser":{"inventory":{"rewardValue":null}}}}`
}

func resetRewardLookups() {
	rewardLookups.mu.Lock()
	rewardLookups.failed = map[string]time.Time{}
	rewardLookups.mu.Unlock()
}

func TestRewardCodes_MergesSources(t *testing.T) {
	resetRewardLookups()
	t.Cleanup(resetRewardLookups)
	srv, seen := rewardServer(t, map[string]func(map[string]any) string{
		OpOnsiteNotifications.Name: fixed(notificationsFixture),
		OpInventory.Name:           fixed(inventoryRewardsFixture),
		OpRewardCodeModal.Name:     codeModal,
	})
	b := newForTest(srv.URL)
	got, err := b.RewardCodes(context.Background(), platform.Session{AccessToken: "tok", UserID: "1", ClientID: ClientTV}, nil)
	require.NoError(t, err)
	require.Len(t, got, 1, "the badge has no code; the notification joins the cape")
	assert.Equal(t, platform.RewardCode{
		Key: "r-cape", Name: "Blue Creeper Cape", Game: "Minecraft", Code: "AB12-CD34-EF56",
		RedeemURL:  "https://www.twitch.tv/drops/inventory?redeem=r-cape",
		ExpiresAt:  time.Date(2026, 10, 20, 6, 58, 59, 999_000_000, time.UTC),
		ReceivedAt: time.Date(2026, 10, 13, 10, 0, 0, 0, time.UTC),
	}, got[0])

	reqs := seen()
	ops := map[string]int{}
	for _, r := range reqs {
		ops[r.Op]++
		assert.Empty(t, r.Header.Get("Client-Integrity"), "never send Client-Integrity")
		switch r.Op {
		case OpInventory.Name:
			assert.Equal(t, OpInventory.Hash, r.Hash)
			assert.Equal(t, true, r.Vars["fetchRewardCampaigns"])
		case OpRewardCodeModal.Name:
			assert.Equal(t, OpRewardCodeModal.Hash, r.Hash)
		case OpOnsiteNotifications.Name:
			assert.Equal(t, OpOnsiteNotifications.Hash, r.Hash)
			assert.Equal(t, "VIEWER", r.Vars["displayType"])
		}
	}
	assert.Equal(t, map[string]int{OpOnsiteNotifications.Name: 1, OpInventory.Name: 1, OpRewardCodeModal.Name: 1}, ops,
		"one code read: the badge is game-less and points at Twitch")

	// A second pass with code and expiry stored makes no code read, and the
	// listed reward still carries the notification's code and link.
	got, err = b.RewardCodes(context.Background(), platform.Session{AccessToken: "tok", UserID: "1"},
		func(key string) (bool, bool) { return key == "r-cape", key == "r-cape" })
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "r-cape", got[0].Key)
	assert.Equal(t, 1, countOp(seen(), OpRewardCodeModal.Name), "no new code read")
}

func countOp(reqs []rewardReq, op string) int {
	n := 0
	for _, r := range reqs {
		if r.Op == op {
			n++
		}
	}
	return n
}

func TestRewardCodes_FailedReadWaits(t *testing.T) {
	resetRewardLookups()
	t.Cleanup(resetRewardLookups)
	srv, seen := rewardServer(t, map[string]func(map[string]any) string{
		OpOnsiteNotifications.Name: fixed(`{"data":{"currentUser":{"notifications":{"edges":[]}}}}`),
		OpInventory.Name:           fixed(inventoryRewardsFixture),
		OpRewardCodeModal.Name:     fixed(`{"errors":[{"message":"server error"}],"data":null}`),
	})
	b := newForTest(srv.URL)
	s := platform.Session{AccessToken: "tok", UserID: "1"}
	got, err := b.RewardCodes(context.Background(), s, nil)
	require.NoError(t, err)
	assert.Empty(t, got)
	_, err = b.RewardCodes(context.Background(), s, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, countOp(seen(), OpRewardCodeModal.Name), "a failed code read waits a day")
}

func TestRewardCodes_NotificationOnly(t *testing.T) {
	resetRewardLookups()
	t.Cleanup(resetRewardLookups)
	srv, _ := rewardServer(t, map[string]func(map[string]any) string{
		OpOnsiteNotifications.Name: fixed(notificationsFixture),
		// A rotated Inventory hash: notifications alone still find the code.
		OpInventory.Name: fixed(`{"errors":[{"message":"PersistedQueryNotFound"}],"data":null}`),
	})
	b := newForTest(srv.URL)
	got, err := b.RewardCodes(context.Background(), platform.Session{AccessToken: "tok", UserID: "1"}, nil)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "notification:notif-1", got[0].Key)
	assert.Equal(t, "Blue Creeper Cape", got[0].Name)
	assert.Equal(t, "AB12-CD34-EF56", got[0].Code)
	assert.Equal(t, "https://www.twitch.tv/drops/inventory?redeem=r-cape", got[0].RedeemURL)
	assert.True(t, got[0].ExpiresAt.IsZero())
}

func TestRewardCodes_TVRefusal(t *testing.T) {
	resetRewardLookups()
	t.Cleanup(resetRewardLookups)
	for name, replies := range map[string]map[string]func(map[string]any) string{
		"unauthorized errors": {
			OpOnsiteNotifications.Name: fixed(`{"errors":[{"message":"Unauthorized"}],"data":null}`),
			OpInventory.Name:           fixed(`{"errors":[{"message":"forbidden"}],"data":{}}`),
		},
		"no current user": {
			OpOnsiteNotifications.Name: fixed(`{"data":{"currentUser":null}}`),
			OpInventory.Name:           fixed(`{"data":{"currentUser":null}}`),
		},
		"integrity": {
			OpOnsiteNotifications.Name: fixed(`{"errors":[{"message":"failed integrity check"}],"data":null}`),
			OpInventory.Name:           fixed(`{"errors":[{"message":"failed integrity check"}],"data":null}`),
		},
	} {
		t.Run(name, func(t *testing.T) {
			srv, _ := rewardServer(t, replies)
			b := newForTest(srv.URL)
			_, err := b.RewardCodes(context.Background(), platform.Session{AccessToken: "tok", ClientID: ClientTV}, nil)
			require.Error(t, err)
			assert.True(t, errors.Is(err, platform.ErrRewardCodesUnavailable), "%v", err)
		})
	}

	// A plain failure on both is not a refusal: the caller retries sooner.
	srv, _ := rewardServer(t, map[string]func(map[string]any) string{
		OpOnsiteNotifications.Name: fixed(`{"errors":[{"message":"service timeout"}],"data":null}`),
		OpInventory.Name:           fixed(`{"errors":[{"message":"service timeout"}],"data":null}`),
	})
	_, err := newForTest(srv.URL).RewardCodes(context.Background(), platform.Session{AccessToken: "tok"}, nil)
	require.Error(t, err)
	assert.False(t, errors.Is(err, platform.ErrRewardCodesUnavailable))

	_, err = newForTest(srv.URL).RewardCodes(context.Background(), platform.Session{}, nil)
	assert.True(t, errors.Is(err, platform.ErrRewardCodesUnavailable), "no token")
}
