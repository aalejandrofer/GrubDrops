package twitch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/plan"
	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// DropCampaignDetails resolves its channelLogin variable as user(id:), so a
// login string makes Twitch answer data.user:null. Every campaign then had
// zero benefits and an Android session never queued a drop. This drives the
// Android path end to end: dashboard, details, inventory, then the planner.
func TestAndroidSession_ListActiveFeedsPlanQueue(t *testing.T) {
	const userID = "98765"
	const dashboard = `{"data":{"currentUser":{"dropCampaigns":[{
		"id":"1a262fb8-ab5d-4dbe-9284-bdc17aabe5c9","name":"WoW Drops","status":"ACTIVE",
		"startAt":"2026-10-01T00:00:00Z","endAt":"2099-11-03T17:59:59Z",
		"self":{"isAccountConnected":true},
		"game":{"id":"18122","displayName":"World of Warcraft"}}]}}}`
	const details = `{"data":{"user":{"id":"98765","dropCampaign":{
		"id":"1a262fb8-ab5d-4dbe-9284-bdc17aabe5c9","allow":{"isEnabled":false,"channels":[]},
		"timeBasedDrops":[{"id":"188909b5-c7ef-11f1-9ac8-0a58a9feac02","name":"Mount",
			"requiredMinutesWatched":240,
			"benefitEdges":[{"benefit":{"id":"ben-mount","name":"Mount","imageAssetURL":""}}]}]}}}}`
	const inventory = `{"data":{"currentUser":{"inventory":{"dropCampaignsInProgress":[{
		"id":"1a262fb8-ab5d-4dbe-9284-bdc17aabe5c9","status":"ACTIVE",
		"timeBasedDrops":[{"id":"188909b5-c7ef-11f1-9ac8-0a58a9feac02","requiredMinutesWatched":240,
			"self":{"currentMinutesWatched":84,"isClaimed":false}}]}],"gameEventDrops":[]}}}}`

	for _, tc := range []struct {
		name    string
		sessUID string
	}{
		{"user id on session", userID},
		{"user id from CurrentUser", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req gqlRequest
				require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
				switch req.OperationName {
				case OpCampaigns.Name:
					_, _ = w.Write([]byte(dashboard))
				case "CurrentUser":
					_, _ = w.Write([]byte(`{"data":{"currentUser":{"id":"98765","login":"ttik3r"}}}`))
				case OpDropCampaignDetails.Name:
					// Twitch's resolver: user(id: $channelLogin).
					if req.Variables["channelLogin"] != userID {
						_, _ = w.Write([]byte(`{"data":{"user":null}}`))
						return
					}
					_, _ = w.Write([]byte(details))
				case OpInventory.Name:
					_, _ = w.Write([]byte(inventory))
				default:
					t.Fatalf("unexpected op %q", req.OperationName)
				}
			}))
			defer srv.Close()

			games := []plan.Game{{Name: "World of Warcraft", Slug: "world-of-warcraft"}}
			b := newForTest(srv.URL)
			sess := platform.Session{
				AccessToken: "tok", ClientID: ClientAndroid, UserID: tc.sessUID,
				GameFilter: func(g string) bool { _, ok := plan.Match(games, g); return ok },
			}

			camps, err := b.ListActiveCampaigns(context.Background(), sess)
			require.NoError(t, err)
			progress, err := b.InventoryProgress(context.Background(), sess)
			require.NoError(t, err)

			res := plan.Build(plan.Input{Games: games, Campaigns: camps, Progress: progress})
			require.Len(t, res.Queue, 1, "android session must queue the WoW drop")
			require.Equal(t, "188909b5-c7ef-11f1-9ac8-0a58a9feac02", res.Queue[0].Benefit.ID)
			require.Equal(t, 84, res.Queue[0].Minutes)
		})
	}
}
