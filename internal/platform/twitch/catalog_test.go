package twitch

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// dashboardAndroid is the ViewerDropsDashboard an Android-client session
// sees: the watch campaign campR6 (hidden from the TV client behind a
// badge campaign), a non-whitelisted game, an expired campaign, and an
// ACTIVE-labelled campaign whose end time already passed. The Android
// account is NOT linked for campR6 — that flag must never leak to other
// accounts through the catalog.
const dashboardAndroid = `{"data":{"currentUser":{"dropCampaigns":[
 {"id":"campR6","name":"Rust S2 Watch","status":"ACTIVE","startAt":"2020-01-01T00:00:00Z","endAt":"2030-01-01T00:00:00Z",
  "accountLinkURL":"https://link/r6","self":{"isAccountConnected":false},"game":{"id":"263490","displayName":"Rust"}},
 {"id":"campApex","name":"Apex Watch","status":"ACTIVE","startAt":"2020-01-01T00:00:00Z","endAt":"2030-01-01T00:00:00Z",
  "self":{"isAccountConnected":true},"game":{"id":"1","displayName":"Apex Legends"}},
 {"id":"campOld","name":"Rust S1","status":"EXPIRED","startAt":"2020-01-01T00:00:00Z","endAt":"2020-02-01T00:00:00Z",
  "self":{"isAccountConnected":true},"game":{"id":"263490","displayName":"Rust"}},
 {"id":"campPast","name":"Rust Ended","status":"ACTIVE","startAt":"2020-01-01T00:00:00Z","endAt":"2020-02-01T00:00:00Z",
  "self":{"isAccountConnected":true},"game":{"id":"263490","displayName":"Rust"}}]}}}`

const detailsR6 = `{"data":{"user":{"dropCampaign":{"id":"campR6","name":"Rust S2 Watch",
 "allow":{"isEnabled":true,"channels":[{"id":"c7","name":"r6chan"}]},
 "timeBasedDrops":[{"id":"dR1","name":"Charm","requiredMinutesWatched":120,"requiredSubs":0,
   "benefitEdges":[{"benefit":{"id":"bR1","name":"Charm","imageAssetURL":""}}]}]}}}}`

const detailsEmpty = `{"data":{"user":{"dropCampaign":{"id":"x","timeBasedDrops":[]}}}}`

// availBadge mimics the live masking: AvailableDrops returns ONE campaign
// per channel, a global 0-minute chat badge, hiding campR6.
const availBadge = `{"data":{"channel":{"viewerDropCampaigns":[{"id":"campBadge","name":"Chat Badge","game":{"id":"263490","name":"Rust"},"endAt":"2030-01-01T00:00:00Z",
 "timeBasedDrops":[{"id":"dB","name":"Badge","requiredMinutesWatched":0,"requiredSubs":0,"benefitEdges":[{"benefit":{"id":"bB","name":"Badge","imageAssetURL":""}}]}]}]}}}`

const inventoryEmpty = `{"data":{"currentUser":{"inventory":{"dropCampaignsInProgress":[],"gameEventDrops":[]}}}}`

func androidBackend(t *testing.T) *Backend {
	t.Helper()
	srv := fakeGQL(t, map[string]func(map[string]any) string{
		"ViewerDropsDashboard": func(map[string]any) string { return dashboardAndroid },
		"DropCampaignDetails": func(v map[string]any) string {
			if v["dropID"] == "campR6" {
				return detailsR6
			}
			return detailsEmpty
		},
	})
	t.Cleanup(srv.Close)
	return newForTest(srv.URL)
}

func tvBackend(t *testing.T, inventory string) *Backend {
	t.Helper()
	srv := fakeGQL(t, map[string]func(map[string]any) string{
		"DirectoryPage_Game": func(map[string]any) string { return dirRust },
		"DropsHighlightService_AvailableDrops": func(map[string]any) string {
			return availBadge
		},
		"Inventory": func(map[string]any) string { return inventory },
	})
	t.Cleanup(srv.Close)
	return newForTest(srv.URL)
}

func tvSess() platform.Session {
	return platform.Session{AccessToken: "tv", ClientID: ClientTV, Games: []string{"Rust"}}
}

func campsByID(camps []platform.Campaign) map[string]platform.Campaign {
	out := map[string]platform.Campaign{}
	for _, c := range camps {
		out[c.ID] = c
	}
	return out
}

// publishFromAndroid runs a non-TV ListActiveCampaigns against cat.
func publishFromAndroid(t *testing.T, cat *Catalog) {
	t.Helper()
	ab := androidBackend(t)
	ab.SetCatalog(cat)
	_, err := ab.ListActiveCampaigns(context.Background(), platform.Session{AccessToken: "android"})
	require.NoError(t, err)
}

// (a) A campaign the TV client can't see (masked by a badge campaign) is
// found through the catalog an Android account published: optimistic,
// non-account-specific link state, its allow-list feeds the TV backend's
// eligible-channel cache, and its benefits serve TV CampaignDetails.
func TestCatalog_TVMergesAndroidCampaigns(t *testing.T) {
	cat := NewCatalog()
	publishFromAndroid(t, cat)

	tb := tvBackend(t, inventoryEmpty)
	tb.SetCatalog(cat)
	camps, err := tb.ListActiveCampaigns(context.Background(), tvSess())
	require.NoError(t, err)
	byID := campsByID(camps)
	require.Contains(t, byID, "campBadge", "the TV path's own campaign is kept")
	require.Contains(t, byID, "campR6", "catalog campaign hidden from TV must be merged")

	r6 := byID["campR6"]
	assert.True(t, r6.AccountLinked, "catalog copy must not carry the Android account's link flags")
	assert.False(t, r6.AccountLinkChecked, "link state is unknown for another account: optimistic")
	assert.Equal(t, "https://link/r6", r6.AccountLinkURL, "campaign-level link URL kept")
	assert.Equal(t, "active", r6.Status)
	require.Len(t, r6.Benefits, 1)
	assert.Equal(t, "dR1", r6.Benefits[0].ID)
	assert.Equal(t, 120, r6.Benefits[0].RequiredMinutes)

	assert.Equal(t, 1, tb.AllowedChannelCount("campR6"), "catalog allow-list feeds ListEligibleChannels")
	tb.mu.Lock()
	assert.Equal(t, []string{"r6chan"}, tb.allowedLoginsByCampaign["campR6"])
	tb.mu.Unlock()

	benefits, err := tb.CampaignDetails(context.Background(), tvSess(), "campR6")
	require.NoError(t, err)
	require.Len(t, benefits, 1, "merged campaigns serve /drops CampaignDetails")
}

// (b) The TV account's own Inventory is authoritative for link state and
// status of a campaign the catalog also carries.
func TestCatalog_InventoryWinsOverCatalog(t *testing.T) {
	const invR6 = `{"data":{"currentUser":{"inventory":{"dropCampaignsInProgress":[
 {"id":"campR6","name":"Rust S2 Watch","game":{"id":"263490","name":"Rust"},"endAt":"2030-01-01T00:00:00Z",
  "status":"ACTIVE","self":{"isAccountConnected":false},"accountLinkURL":"https://link/r6",
  "timeBasedDrops":[{"id":"dR1","name":"Charm","requiredMinutesWatched":120,"requiredSubs":0,
    "benefitEdges":[{"benefit":{"id":"bR1","name":"Charm","imageAssetURL":""}}],
    "self":{"currentMinutesWatched":5,"isClaimed":false,"dropInstanceID":""}}]}],
 "gameEventDrops":[]}}}}`
	cat := NewCatalog()
	publishFromAndroid(t, cat)

	tb := tvBackend(t, invR6)
	tb.SetCatalog(cat)
	camps, err := tb.ListActiveCampaigns(context.Background(), tvSess())
	require.NoError(t, err)
	n := 0
	for _, c := range camps {
		if c.ID == "campR6" {
			n++
		}
	}
	assert.Equal(t, 1, n, "catalog must not duplicate an Inventory campaign")
	r6 := campsByID(camps)["campR6"]
	assert.False(t, r6.AccountLinked, "Inventory's link state wins over the catalog's optimistic one")
	assert.True(t, r6.AccountLinkChecked)
	require.Len(t, r6.Benefits, 1)
}

// (c) Non-whitelisted, expired, and past-end catalog campaigns are not
// merged; a session GameFilter takes precedence over Session.Games.
func TestCatalog_FiltersGameAndWindow(t *testing.T) {
	cat := NewCatalog()
	publishFromAndroid(t, cat)

	tb := tvBackend(t, inventoryEmpty)
	tb.SetCatalog(cat)
	camps, err := tb.ListActiveCampaigns(context.Background(), tvSess())
	require.NoError(t, err)
	byID := campsByID(camps)
	assert.Contains(t, byID, "campR6")
	assert.NotContains(t, byID, "campApex", "non-whitelisted game not merged")
	assert.NotContains(t, byID, "campOld", "expired campaign not merged")
	assert.NotContains(t, byID, "campPast", "campaign past its end time not merged")

	sess := tvSess()
	sess.GameFilter = func(string) bool { return false }
	tb2 := tvBackend(t, inventoryEmpty)
	tb2.SetCatalog(cat)
	camps, err = tb2.ListActiveCampaigns(context.Background(), sess)
	require.NoError(t, err)
	assert.NotContains(t, campsByID(camps), "campR6", "GameFilter, when set, decides membership")
}

// (d) A catalog older than catalogTTL is ignored.
func TestCatalog_StaleIgnored(t *testing.T) {
	cat := NewCatalog()
	publishFromAndroid(t, cat)
	cat.mu.Lock()
	for k, e := range cat.sources {
		e.at = e.at.Add(-catalogTTL - time.Minute)
		cat.sources[k] = e
	}
	cat.mu.Unlock()

	tb := tvBackend(t, inventoryEmpty)
	tb.SetCatalog(cat)
	camps, err := tb.ListActiveCampaigns(context.Background(), tvSess())
	require.NoError(t, err)
	assert.NotContains(t, campsByID(camps), "campR6", "stale catalog must be ignored")
	assert.Equal(t, 0, tb.AllowedChannelCount("campR6"))
}

// (e) No catalog set: TV discovery is unchanged, and a non-TV backend
// without a catalog still lists normally.
func TestCatalog_NoneSetUnchanged(t *testing.T) {
	ab := androidBackend(t)
	_, err := ab.ListActiveCampaigns(context.Background(), platform.Session{AccessToken: "android"})
	require.NoError(t, err)

	tb := tvBackend(t, inventoryEmpty)
	camps, err := tb.ListActiveCampaigns(context.Background(), tvSess())
	require.NoError(t, err)
	require.Len(t, camps, 1)
	assert.Equal(t, "campBadge", camps[0].ID)
}

// A publisher whose whitelist lacks a game emits that campaign as a
// benefit-less shell; it must not wipe another publisher's full entry.
func TestCatalog_PrefersEntryWithBenefits(t *testing.T) {
	cat := NewCatalog()
	full := platform.Campaign{ID: "c1", Game: "Rust", Status: "active", EndsAt: time.Now().Add(time.Hour),
		Benefits: []platform.DropBenefit{{ID: "d1", CampaignID: "c1", RequiredMinutes: 60}}}
	shell := full
	shell.Benefits = nil
	cat.Publish("acc-a", []platform.Campaign{full}, map[string][]string{"c1": {"x"}})
	cat.Publish("acc-b", []platform.Campaign{shell}, nil)

	camps, allowed, _, ok := cat.snapshot()
	require.True(t, ok)
	require.Len(t, camps, 1)
	assert.Len(t, camps[0].Benefits, 1)
	assert.Equal(t, []string{"x"}, allowed["c1"])
}
