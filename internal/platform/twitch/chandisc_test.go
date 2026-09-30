package twitch

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// fakeGQL routes persisted ops by operationName.
func fakeGQL(t *testing.T, byOp map[string]func(vars map[string]any) string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var req struct {
			OperationName string         `json:"operationName"`
			Variables     map[string]any `json:"variables"`
		}
		require.NoError(t, json.Unmarshal(raw, &req))
		f, ok := byOp[req.OperationName]
		if !ok {
			_, _ = w.Write([]byte(`{"data":{}}`))
			return
		}
		_, _ = w.Write([]byte(f(req.Variables)))
	}))
}

const dirRust = `{"data":{"game":{"streams":{"edges":[
 {"node":{"id":"s1","viewersCount":10,"broadcaster":{"id":"c1","login":"alpha"},"game":{"id":"263490","displayName":"Rust"}}},
 {"node":{"id":"s2","viewersCount":5,"broadcaster":{"id":"c2","login":"beta"},"game":{"id":"263490","displayName":"Rust"}}}]}}}}`

const availAlpha = `{"data":{"channel":{"viewerDropCampaigns":[{"id":"campA","name":"Rust Isles General","game":{"id":"263490","name":"Rust"},"endAt":"2030-01-01T00:00:00Z",
 "timeBasedDrops":[
  {"id":"dWatch","name":"Box","requiredMinutesWatched":60,"requiredSubs":0,"benefitEdges":[{"benefit":{"id":"bW","name":"Box","imageAssetURL":"http://img/b.png"}}]},
  {"id":"dSub","name":"Sub Badge","requiredMinutesWatched":30,"requiredSubs":1,"benefitEdges":[{"benefit":{"id":"bS","name":"Badge","imageAssetURL":""}}]}]}]}}}`

const inventoryTV = `{"data":{"currentUser":{"inventory":{"dropCampaignsInProgress":[
 {"id":"campB","name":"Rust Isles Tac Gloves","game":{"id":"263490","name":"Rust"},"endAt":"2030-01-01T00:00:00Z",
  "status":"ACTIVE","self":{"isAccountConnected":true},"accountLinkURL":"https://www.twitch.tv/drops/campaigns?id=campB",
  "allow":{"channels":[{"id":"c9","name":"welyn"}]},
  "timeBasedDrops":[{"id":"dG","name":"Gloves","requiredMinutesWatched":60,"requiredSubs":0,
    "benefitEdges":[{"benefit":{"id":"bG","name":"Gloves","imageAssetURL":""}}],
    "self":{"currentMinutesWatched":14,"isClaimed":false,"dropInstanceID":"i1"}}]}],
 "gameEventDrops":[]}}}}`

func TestListByChannels_TVSession(t *testing.T) {
	srv := fakeGQL(t, map[string]func(map[string]any) string{
		"DirectoryPage_Game": func(v map[string]any) string {
			if v["slug"] == "rust" {
				return dirRust
			}
			return `{"data":{"game":{"streams":{"edges":[]}}}}` // Review Focus #3
		},
		"DropsHighlightService_AvailableDrops": func(v map[string]any) string {
			if v["channelID"] == "c1" {
				return availAlpha
			}
			return `{"data":{"channel":{"viewerDropCampaigns":[]}}}`
		},
		"Inventory": func(map[string]any) string { return inventoryTV },
	})
	defer srv.Close()

	b := newForTest(srv.URL)
	sess := platform.Session{AccessToken: "tv", ClientID: ClientTV, Games: []string{"Rust", "Offline Game"}}
	camps, err := b.ListActiveCampaigns(context.Background(), sess)
	require.NoError(t, err)

	byID := map[string]platform.Campaign{}
	for _, c := range camps {
		byID[c.ID] = c
	}
	require.Contains(t, byID, "campA")
	require.Contains(t, byID, "campB")

	a := byID["campA"]
	assert.Equal(t, "Rust", a.Game)
	assert.Equal(t, "active", a.Status)
	mins := map[string]int{}
	for _, bn := range a.Benefits {
		mins[bn.ID] = bn.RequiredMinutes
		assert.Equal(t, "campA", bn.CampaignID)
	}
	assert.Equal(t, 60, mins["dWatch"])
	assert.Equal(t, 0, mins["dSub"], "Review Focus #4: sub-gated drop is not watch-earnable")

	// Channel-seen campaigns are restricted to channels actually serving
	// them (alpha only; beta served nothing).
	assert.Equal(t, 1, b.AllowedChannelCount("campA"))
	// Inventory campaign keeps its own allow-list.
	assert.Equal(t, 1, b.AllowedChannelCount("campB"))

	// Inventory-sourced campaigns report the real (authoritative) link
	// state and status, not the AvailableDrops-path optimistic defaults.
	bcamp := byID["campB"]
	assert.True(t, bcamp.AccountLinked)
	assert.True(t, bcamp.AccountLinkChecked, "inventory link state is authoritative, unlike AvailableDrops")
	assert.Equal(t, "https://www.twitch.tv/drops/campaigns?id=campB", bcamp.AccountLinkURL)
	assert.Equal(t, "active", bcamp.Status)
}

// TestListByChannels_InventoryUnlinkedAccount: an Inventory campaign with
// self.isAccountConnected=false must report AccountLinked=false and
// AccountLinkChecked=true (not the optimistic scrape-sourced defaults) so
// the watcher's account-link gate can skip it -- mining a campaign the
// user never linked can never claim.
func TestListByChannels_InventoryUnlinkedAccount(t *testing.T) {
	const inventoryUnlinked = `{"data":{"currentUser":{"inventory":{"dropCampaignsInProgress":[
 {"id":"campB","name":"Rust Isles Tac Gloves","game":{"id":"263490","name":"Rust"},"endAt":"2030-01-01T00:00:00Z",
  "self":{"isAccountConnected":false},"accountLinkURL":"https://www.twitch.tv/drops/campaigns?id=campB",
  "allow":{"channels":[{"id":"c9","name":"welyn"}]},
  "timeBasedDrops":[{"id":"dG","name":"Gloves","requiredMinutesWatched":60,"requiredSubs":0,
    "benefitEdges":[{"benefit":{"id":"bG","name":"Gloves","imageAssetURL":""}}],
    "self":{"currentMinutesWatched":14,"isClaimed":false,"dropInstanceID":"i1"}}]}],
 "gameEventDrops":[]}}}}`
	srv := fakeGQL(t, map[string]func(map[string]any) string{
		"DirectoryPage_Game": func(v map[string]any) string { return dirRust },
		"DropsHighlightService_AvailableDrops": func(v map[string]any) string {
			return `{"data":{"channel":{"viewerDropCampaigns":[]}}}`
		},
		"Inventory": func(map[string]any) string { return inventoryUnlinked },
	})
	defer srv.Close()

	b := newForTest(srv.URL)
	sess := platform.Session{AccessToken: "tv", ClientID: ClientTV, Games: []string{"Rust"}}
	camps, err := b.ListActiveCampaigns(context.Background(), sess)
	require.NoError(t, err)

	byID := map[string]platform.Campaign{}
	for _, c := range camps {
		byID[c.ID] = c
	}
	require.Contains(t, byID, "campB")
	campB := byID["campB"]
	assert.False(t, campB.AccountLinked, "unlinked account must not be reported as linked")
	assert.True(t, campB.AccountLinkChecked, "inventory link state is authoritative")
	assert.Equal(t, "https://www.twitch.tv/drops/campaigns?id=campB", campB.AccountLinkURL)
}

// TestListByChannels_InventoryOverridesAvailableDropsLinkState: when the
// same campaign ID is seen from both AvailableDrops (optimistic, unknown
// link state) and Inventory (authoritative, unlinked), the merged
// campaign must report the Inventory link state, and benefits present in
// both sources must not be duplicated.
func TestListByChannels_InventoryOverridesAvailableDropsLinkState(t *testing.T) {
	const inventoryUnlinkedCampA = `{"data":{"currentUser":{"inventory":{"dropCampaignsInProgress":[
 {"id":"campA","name":"Rust Isles General","game":{"id":"263490","name":"Rust"},"endAt":"2030-01-01T00:00:00Z",
  "self":{"isAccountConnected":false},"accountLinkURL":"https://www.twitch.tv/drops/campaigns?id=campA",
  "timeBasedDrops":[{"id":"dWatch","name":"Box","requiredMinutesWatched":60,"requiredSubs":0,
    "benefitEdges":[{"benefit":{"id":"bW","name":"Box","imageAssetURL":"http://img/b.png"}}],
    "self":{"currentMinutesWatched":10,"isClaimed":false,"dropInstanceID":"iA"}}]}],
 "gameEventDrops":[]}}}}`
	srv := fakeGQL(t, map[string]func(map[string]any) string{
		"DirectoryPage_Game": func(v map[string]any) string { return dirRust },
		"DropsHighlightService_AvailableDrops": func(v map[string]any) string {
			if v["channelID"] == "c1" {
				return availAlpha
			}
			return `{"data":{"channel":{"viewerDropCampaigns":[]}}}`
		},
		"Inventory": func(map[string]any) string { return inventoryUnlinkedCampA },
	})
	defer srv.Close()

	b := newForTest(srv.URL)
	sess := platform.Session{AccessToken: "tv", ClientID: ClientTV, Games: []string{"Rust"}}
	camps, err := b.ListActiveCampaigns(context.Background(), sess)
	require.NoError(t, err)

	byID := map[string]platform.Campaign{}
	for _, c := range camps {
		byID[c.ID] = c
	}
	require.Contains(t, byID, "campA")
	campA := byID["campA"]
	assert.False(t, campA.AccountLinked, "Inventory's authoritative link state must win over AvailableDrops' optimistic default")
	assert.True(t, campA.AccountLinkChecked)

	seen := map[string]int{}
	for _, bn := range campA.Benefits {
		seen[bn.ID]++
	}
	assert.Equal(t, 1, seen["dWatch"], "benefit seen from both sources must not be duplicated")
	assert.Equal(t, 1, seen["dSub"], "AvailableDrops-only benefit must be kept")
	assert.Len(t, campA.Benefits, 2)
}

// TestListByChannels_InventoryStatusOverridesWindow: Inventory's own
// status enum ("ACTIVE"/"UPCOMING"/"EXPIRED") is authoritative and maps
// exactly like the dashboard path (listActive) -- it takes precedence
// over the startAt/endAt window heuristic used when Twitch omits status.
func TestListByChannels_InventoryStatusOverridesWindow(t *testing.T) {
	const inventoryExpired = `{"data":{"currentUser":{"inventory":{"dropCampaignsInProgress":[
 {"id":"campB","name":"Rust Isles Tac Gloves","game":{"id":"263490","name":"Rust"},
  "startAt":"2020-01-01T00:00:00Z","endAt":"2030-01-01T00:00:00Z",
  "status":"EXPIRED","self":{"isAccountConnected":true},"accountLinkURL":"",
  "timeBasedDrops":[{"id":"dG","name":"Gloves","requiredMinutesWatched":60,"requiredSubs":0,
    "benefitEdges":[{"benefit":{"id":"bG","name":"Gloves","imageAssetURL":""}}],
    "self":{"currentMinutesWatched":14,"isClaimed":false,"dropInstanceID":"i1"}}]}],
 "gameEventDrops":[]}}}}`
	srv := fakeGQL(t, map[string]func(map[string]any) string{
		"DirectoryPage_Game": func(v map[string]any) string { return dirRust },
		"DropsHighlightService_AvailableDrops": func(v map[string]any) string {
			return `{"data":{"channel":{"viewerDropCampaigns":[]}}}`
		},
		"Inventory": func(map[string]any) string { return inventoryExpired },
	})
	defer srv.Close()

	b := newForTest(srv.URL)
	sess := platform.Session{AccessToken: "tv", ClientID: ClientTV, Games: []string{"Rust"}}
	camps, err := b.ListActiveCampaigns(context.Background(), sess)
	require.NoError(t, err)

	byID := map[string]platform.Campaign{}
	for _, c := range camps {
		byID[c.ID] = c
	}
	require.Contains(t, byID, "campB")
	// Window alone (startAt in the past, endAt in the future) would say
	// "active" -- status "EXPIRED" must win.
	assert.Equal(t, "expired", byID["campB"].Status)
}

// TestListByChannels_DedupesGamesBySlug proves duplicate whitelist tokens
// that slugify to the same game (discovery's whitelist union emits both a
// game's lowercased display name and its lowercased slug, e.g.
// "grand theft auto v" and "grand-theft-auto-v") issue exactly one
// DirectoryPage_Game request, not one per token — otherwise every
// multi-word game doubles Twitch's request volume each tick.
func TestListByChannels_DedupesGamesBySlug(t *testing.T) {
	var mu sync.Mutex
	dirCalls := map[string]int{}
	srv := fakeGQL(t, map[string]func(map[string]any) string{
		"DirectoryPage_Game": func(v map[string]any) string {
			slug, _ := v["slug"].(string)
			mu.Lock()
			dirCalls[slug]++
			mu.Unlock()
			if slug == "rust" {
				return dirRust
			}
			return `{"data":{"game":{"streams":{"edges":[]}}}}`
		},
		"DropsHighlightService_AvailableDrops": func(v map[string]any) string {
			if v["channelID"] == "c1" {
				return availAlpha
			}
			return `{"data":{"channel":{"viewerDropCampaigns":[]}}}`
		},
		"Inventory": func(map[string]any) string { return inventoryTV },
	})
	defer srv.Close()

	b := newForTest(srv.URL)
	// "Rust" and "rust" both slugify to "rust" — must be deduped to a
	// single directory + AvailableDrops fan-out.
	sess := platform.Session{AccessToken: "tv", ClientID: ClientTV, Games: []string{"Rust", "rust"}}
	camps, err := b.ListActiveCampaigns(context.Background(), sess)
	require.NoError(t, err)

	byID := map[string]platform.Campaign{}
	for _, c := range camps {
		byID[c.ID] = c
	}
	require.Contains(t, byID, "campA", "campaign discovery must still work after dedupe")

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, 1, dirCalls["rust"], "duplicate slug must issue exactly one DirectoryPage_Game request")
}

// tvDiscoveryServer serves the canned TV channel-first fixtures and counts
// every GQL request it receives.
func tvDiscoveryServer(t *testing.T, calls *int, mu *sync.Mutex) *httptest.Server {
	count := func(f func(map[string]any) string) func(map[string]any) string {
		return func(v map[string]any) string {
			mu.Lock()
			*calls++
			mu.Unlock()
			return f(v)
		}
	}
	return fakeGQL(t, map[string]func(map[string]any) string{
		"DirectoryPage_Game": count(func(v map[string]any) string {
			if v["slug"] == "rust" {
				return dirRust
			}
			return `{"data":{"game":{"streams":{"edges":[]}}}}`
		}),
		"DropsHighlightService_AvailableDrops": count(func(v map[string]any) string {
			if v["channelID"] == "c1" {
				return availAlpha
			}
			return `{"data":{"channel":{"viewerDropCampaigns":[]}}}`
		}),
		"Inventory": count(func(map[string]any) string { return inventoryTV }),
	})
}

// TestCampaignDetails_TVSession_ServedFromDiscoveryCache: TV CampaignDetails
// runs inside the /drops HTTP request, so it must never do the channel-first
// walk itself — it serves the benefits the last ListActiveCampaigns pass
// found, with zero GQL calls.
func TestCampaignDetails_TVSession_ServedFromDiscoveryCache(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	srv := tvDiscoveryServer(t, &calls, &mu)
	defer srv.Close()

	b := newForTest(srv.URL)
	sess := platform.Session{AccessToken: "tv", ClientID: ClientTV, Games: []string{"Rust"}}
	_, err := b.ListActiveCampaigns(context.Background(), sess)
	require.NoError(t, err)

	mu.Lock()
	calls = 0
	mu.Unlock()

	benefits, err := b.CampaignDetails(context.Background(), sess, "campA")
	require.NoError(t, err)
	require.NotEmpty(t, benefits)
	for _, bn := range benefits {
		assert.Equal(t, "campA", bn.CampaignID)
	}
	benefits, err = b.CampaignDetails(context.Background(), sess, "campB")
	require.NoError(t, err)
	require.NotEmpty(t, benefits, "inventory-sourced campaigns are cached too")

	mu.Lock()
	assert.Equal(t, 0, calls, "CampaignDetails must be served from the discovery cache")
	mu.Unlock()
}

// TestCampaignDetails_TVSession_MissReturnsNil: a cache miss (no discovery
// pass yet, unknown campaign, or stale entry) returns (nil, nil) without
// touching the network.
func TestCampaignDetails_TVSession_MissReturnsNil(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	srv := tvDiscoveryServer(t, &calls, &mu)
	defer srv.Close()

	b := newForTest(srv.URL)
	sess := platform.Session{AccessToken: "tv", ClientID: ClientTV, Games: []string{"Rust"}}

	benefits, err := b.CampaignDetails(context.Background(), sess, "campA")
	require.NoError(t, err)
	assert.Nil(t, benefits, "no discovery pass yet: miss")
	mu.Lock()
	assert.Equal(t, 0, calls, "a miss must not trigger the channel-first walk")
	mu.Unlock()

	_, err = b.ListActiveCampaigns(context.Background(), sess)
	require.NoError(t, err)
	mu.Lock()
	calls = 0
	mu.Unlock()

	benefits, err = b.CampaignDetails(context.Background(), sess, "nonexistent")
	require.NoError(t, err)
	assert.Nil(t, benefits)

	// Age the cache past detailsTTL: the entry is stale and must miss.
	b.mu.Lock()
	b.tvDetailsAt = b.tvDetailsAt.Add(-detailsTTL - time.Minute)
	b.mu.Unlock()
	benefits, err = b.CampaignDetails(context.Background(), sess, "campA")
	require.NoError(t, err)
	assert.Nil(t, benefits, "stale cache entry must miss")

	mu.Lock()
	assert.Equal(t, 0, calls)
	mu.Unlock()
}

func TestListActiveCampaigns_LegacySessionUsesDashboard(t *testing.T) {
	called := map[string]bool{}
	srv := fakeGQL(t, map[string]func(map[string]any) string{
		"ViewerDropsDashboard": func(map[string]any) string {
			called["dash"] = true
			return `{"data":{"currentUser":{"dropCampaigns":[]}}}`
		},
		"DirectoryPage_Game": func(map[string]any) string { called["dir"] = true; return dirRust },
	})
	defer srv.Close()
	b := newForTest(srv.URL)
	_, err := b.ListActiveCampaigns(context.Background(), platform.Session{AccessToken: "a", Games: []string{"Rust"}})
	require.NoError(t, err)
	assert.True(t, called["dash"])
	assert.False(t, called["dir"], "Android sessions must not change discovery path")
}

// TestListByChannels_StatusFromWindow: channel-first campaigns derive
// Status from their startAt/endAt window like the dashboard path, instead
// of always claiming "active" — an expired in-progress Inventory campaign
// must not be offered to the watcher as mineable.
func TestListByChannels_StatusFromWindow(t *testing.T) {
	const availUpcoming = `{"data":{"channel":{"viewerDropCampaigns":[{"id":"campU","name":"Soon","game":{"id":"263490","name":"Rust"},
 "startAt":"2029-01-01T00:00:00Z","endAt":"2030-01-01T00:00:00Z",
 "timeBasedDrops":[{"id":"dU","name":"U","requiredMinutesWatched":60,"requiredSubs":0,"benefitEdges":[{"benefit":{"id":"bU","name":"U","imageAssetURL":""}}]}]}]}}}`
	const inventoryMixed = `{"data":{"currentUser":{"inventory":{"dropCampaignsInProgress":[
 {"id":"campX","name":"Over","game":{"id":"263490","name":"Rust"},"startAt":"2020-01-01T00:00:00Z","endAt":"2020-02-01T00:00:00Z",
  "timeBasedDrops":[{"id":"dX","name":"X","requiredMinutesWatched":60,"requiredSubs":0,
    "benefitEdges":[{"benefit":{"id":"bX","name":"X","imageAssetURL":""}}],
    "self":{"currentMinutesWatched":14,"isClaimed":false,"dropInstanceID":"iX"}}]},
 {"id":"campB","name":"Live","game":{"id":"263490","name":"Rust"},"startAt":"2020-01-01T00:00:00Z","endAt":"2030-01-01T00:00:00Z",
  "timeBasedDrops":[]}],
 "gameEventDrops":[]}}}}`
	srv := fakeGQL(t, map[string]func(map[string]any) string{
		"DirectoryPage_Game": func(v map[string]any) string { return dirRust },
		"DropsHighlightService_AvailableDrops": func(v map[string]any) string {
			if v["channelID"] == "c1" {
				return availUpcoming
			}
			return `{"data":{"channel":{"viewerDropCampaigns":[]}}}`
		},
		"Inventory": func(map[string]any) string { return inventoryMixed },
	})
	defer srv.Close()

	b := newForTest(srv.URL)
	sess := platform.Session{AccessToken: "tv", ClientID: ClientTV, Games: []string{"Rust"}}
	camps, err := b.ListActiveCampaigns(context.Background(), sess)
	require.NoError(t, err)
	byID := map[string]platform.Campaign{}
	for _, c := range camps {
		byID[c.ID] = c
	}
	require.Contains(t, byID, "campX")
	require.Contains(t, byID, "campU")
	require.Contains(t, byID, "campB")
	assert.Equal(t, "expired", byID["campX"].Status)
	assert.Equal(t, "upcoming", byID["campU"].Status)
	assert.Equal(t, "active", byID["campB"].Status)
	assert.False(t, byID["campU"].StartsAt.IsZero(), "startAt decoded")
}

// TestListByChannels_InventoryFailureKeepsDirectoryResults: a failed
// Inventory call is logged and the directory-sourced campaigns are still
// returned, rather than failing (and discarding) the whole pass.
func TestListByChannels_InventoryFailureKeepsDirectoryResults(t *testing.T) {
	srv := fakeGQL(t, map[string]func(map[string]any) string{
		"DirectoryPage_Game": func(v map[string]any) string { return dirRust },
		"DropsHighlightService_AvailableDrops": func(v map[string]any) string {
			if v["channelID"] == "c1" {
				return availAlpha
			}
			return `{"data":{"channel":{"viewerDropCampaigns":[]}}}`
		},
		"Inventory": func(map[string]any) string { return `{"errors":[{"message":"service error"}],"data":null}` },
	})
	defer srv.Close()

	b := newForTest(srv.URL)
	sess := platform.Session{AccessToken: "tv", ClientID: ClientTV, Games: []string{"Rust"}}
	camps, err := b.ListActiveCampaigns(context.Background(), sess)
	require.NoError(t, err)
	require.Len(t, camps, 1)
	assert.Equal(t, "campA", camps[0].ID)
	assert.Equal(t, 1, b.AllowedChannelCount("campA"))
}

// TestListByChannels_SkipsAvailableDropsForOtherGames: AvailableDrops also
// returns streamers' own channel campaigns, which carry no game (or a
// different game than the directory being walked). Those flooded /drops
// with hundreds of "no game" rows. Only campaigns for the queried game are
// kept; Inventory campaigns are unaffected.
func TestListByChannels_SkipsAvailableDropsForOtherGames(t *testing.T) {
	const availMixed = `{"data":{"channel":{"viewerDropCampaigns":[
 {"id":"campNoGame","name":"Streamer Channel Drop","game":null,"endAt":"2030-01-01T00:00:00Z",
  "timeBasedDrops":[{"id":"dN","name":"N","requiredMinutesWatched":30,"requiredSubs":0,"benefitEdges":[{"benefit":{"id":"bN","name":"N","imageAssetURL":""}}]}]},
 {"id":"campOther","name":"Other Game Drop","game":{"id":"1","name":"Apex Legends"},"endAt":"2030-01-01T00:00:00Z",
  "timeBasedDrops":[{"id":"dO","name":"O","requiredMinutesWatched":30,"requiredSubs":0,"benefitEdges":[{"benefit":{"id":"bO","name":"O","imageAssetURL":""}}]}]},
 {"id":"campA","name":"Rust Isles General","game":{"id":"263490","name":"Rust"},"endAt":"2030-01-01T00:00:00Z",
  "timeBasedDrops":[{"id":"dWatch","name":"Box","requiredMinutesWatched":60,"requiredSubs":0,"benefitEdges":[{"benefit":{"id":"bW","name":"Box","imageAssetURL":""}}]}]}]}}}`
	srv := fakeGQL(t, map[string]func(map[string]any) string{
		"DirectoryPage_Game": func(v map[string]any) string { return dirRust },
		"DropsHighlightService_AvailableDrops": func(v map[string]any) string {
			return availMixed
		},
		"Inventory": func(map[string]any) string { return inventoryTV },
	})
	defer srv.Close()

	b := newForTest(srv.URL)
	sess := platform.Session{AccessToken: "tv", ClientID: ClientTV, Games: []string{"Rust"}}
	camps, err := b.ListActiveCampaigns(context.Background(), sess)
	require.NoError(t, err)
	ids := map[string]bool{}
	for _, c := range camps {
		ids[c.ID] = true
	}
	assert.True(t, ids["campA"], "matching-game AvailableDrops campaign kept")
	assert.True(t, ids["campB"], "Inventory campaign unaffected")
	assert.False(t, ids["campNoGame"], "game-less channel campaign skipped")
	assert.False(t, ids["campOther"], "campaign for a different game skipped")
	assert.Len(t, camps, 2)
}

// TestListByChannels_GameIDMatchOverridesNameSlugMismatch: Twitch's
// AvailableDrops game display name can diverge from the directory's own
// display name for the same game (e.g. a differently punctuated/localized
// variant) even though both payloads carry the same game ID. The ID -- not
// the display-name slug -- is the authoritative match signal; a campaign
// whose name wouldn't slug-match the directory's game must still be kept
// when its game ID matches the directory stream's game ID.
func TestListByChannels_GameIDMatchOverridesNameSlugMismatch(t *testing.T) {
	const availIDMatchNameMismatch = `{"data":{"channel":{"viewerDropCampaigns":[
 {"id":"campIDMatch","name":"Renamed Drop","game":{"id":"263490","name":"RUST: Legacy Display"},"endAt":"2030-01-01T00:00:00Z",
  "timeBasedDrops":[{"id":"dI","name":"I","requiredMinutesWatched":30,"requiredSubs":0,"benefitEdges":[{"benefit":{"id":"bI","name":"I","imageAssetURL":""}}]}]}]}}}`
	srv := fakeGQL(t, map[string]func(map[string]any) string{
		"DirectoryPage_Game": func(v map[string]any) string { return dirRust },
		"DropsHighlightService_AvailableDrops": func(v map[string]any) string {
			return availIDMatchNameMismatch
		},
		"Inventory": func(map[string]any) string { return inventoryEmpty },
	})
	defer srv.Close()

	b := newForTest(srv.URL)
	sess := platform.Session{AccessToken: "tv", ClientID: ClientTV, Games: []string{"Rust"}}
	camps, err := b.ListActiveCampaigns(context.Background(), sess)
	require.NoError(t, err)
	ids := map[string]bool{}
	for _, c := range camps {
		ids[c.ID] = true
	}
	assert.True(t, ids["campIDMatch"], "game ID match must keep the campaign despite a display-name/slug mismatch")
}
