package sidecar

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseInventoryNextData_Empty(t *testing.T) {
	out, err := parseInventoryNextData(`{"props":{"pageProps":{"drops":[]}}}`)
	require.NoError(t, err)
	assert.Empty(t, out)
}

func TestParseInventoryNextData_OneDrop(t *testing.T) {
	raw := `{"props":{"pageProps":{"drops":[
		{"id":"d1","minutesWatched":30,"claimed":false},
		{"id":"d2","minutesWatched":60,"claimed":true}
	]}}}`
	out, err := parseInventoryNextData(raw)
	require.NoError(t, err)
	require.Len(t, out, 2)
	assert.Equal(t, "d1", out[0].BenefitId)
	assert.Equal(t, int32(30), out[0].MinutesWatched)
	assert.False(t, out[0].Claimed)
	assert.True(t, out[1].Claimed)
}

func TestParseInventoryNextData_Malformed(t *testing.T) {
	_, err := parseInventoryNextData(`not json`)
	require.Error(t, err)
}

func TestScanForCampaigns_NuxtShape(t *testing.T) {
	// Nuxt commonly nests state under data[0].pageProps or
	// state.drops.activeCampaigns. We don't pin a path — scanForCampaigns
	// walks the whole tree.
	raw := `{
	  "state": {
	    "drops": {
	      "activeCampaigns": [
	        {
	          "id": "camp-1",
	          "name": "Rust Drops Weekend",
	          "game": "Rust",
	          "startsAt": "2026-06-01T00:00:00Z",
	          "endsAt": "2026-06-10T00:00:00Z",
	          "benefits": [
	            {"id": "ben-a", "name": "Crate", "requiredMinutes": 90, "imageUrl": "https://x/y.png"}
	          ]
	        },
	        {
	          "id": "camp-2",
	          "name": "CS Drops",
	          "game": {"name": "Counter-Strike", "slug": "cs"},
	          "rewards": [
	            {"id": "ben-b", "name": "Sticker", "minutes": 30}
	          ]
	        }
	      ]
	    }
	  }
	}`
	got := scanForCampaigns(raw)
	require.Len(t, got, 2)
	byID := map[string]int{}
	for i, c := range got {
		byID[c.Id] = i
	}
	c1 := got[byID["camp-1"]]
	assert.Equal(t, "Rust", c1.Game)
	assert.Equal(t, "Rust Drops Weekend", c1.Name)
	assert.Greater(t, c1.StartsAt, int64(0))
	assert.Greater(t, c1.EndsAt, c1.StartsAt)
	require.Len(t, c1.Benefits, 1)
	assert.Equal(t, "ben-a", c1.Benefits[0].Id)
	assert.Equal(t, int32(90), c1.Benefits[0].RequiredMinutes)
	assert.Equal(t, "https://x/y.png", c1.Benefits[0].ImageUrl)

	c2 := got[byID["camp-2"]]
	assert.Equal(t, "Counter-Strike", c2.Game, "nested game object resolved")
	require.Len(t, c2.Benefits, 1)
	assert.Equal(t, int32(30), c2.Benefits[0].RequiredMinutes)
}

func TestScanForCampaigns_DedupesById(t *testing.T) {
	// Same campaign object appearing under two different state paths
	// (common in Nuxt SSR + client hydration) should only surface once.
	raw := `{
	  "ssr": {"drops": [{"id": "dup", "name": "X", "game": "Rust"}]},
	  "client": {"drops": [{"id": "dup", "name": "X", "game": "Rust"}]}
	}`
	got := scanForCampaigns(raw)
	require.Len(t, got, 1)
	assert.Equal(t, "dup", got[0].Id)
}

func TestScanForCampaigns_SkipsObjectsMissingRequiredFields(t *testing.T) {
	// Objects with no "game" are skipped (they aren't campaigns). The
	// project goal is game-aware mining; objects missing game can't be
	// whitelisted.
	raw := `{
	  "drops": [
	    {"id": "a", "name": "no game"},
	    {"id": "b", "game": "Rust"} ,
	    {"id": "c", "name": "complete", "game": "Rust"}
	  ]
	}`
	got := scanForCampaigns(raw)
	require.Len(t, got, 1)
	assert.Equal(t, "c", got[0].Id)
}

func TestParseActiveDropsState_FallbackChain(t *testing.T) {
	// Empty nuxt -> try next.
	nuxt := `{}`
	next := `{"props":{"pageProps":{"drops":[{"id":"x","name":"X","game":"Rust"}]}}}`
	camps, src := parseActiveDropsState(nuxt, next)
	require.Len(t, camps, 1)
	assert.Equal(t, "next", src)

	// Both empty -> nothing.
	camps, src = parseActiveDropsState(`{}`, `{}`)
	assert.Empty(t, camps)
	assert.Equal(t, "", src)
}

func TestParseActiveDropsHTML_NoGameMeansNoCampaign(t *testing.T) {
	// HTML fallback can't reliably recover a game name — entries are
	// dropped to avoid polluting the dashboard with unwhitelistable
	// "(unknown game)" cards.
	html := `<html><body>
	  <div data-drop-card="campaign-1">card 1</div>
	  <div data-drop-card="campaign-2">card 2</div>
	</body></html>`
	got := parseActiveDropsHTML(html)
	assert.Empty(t, got)
}

func TestParseKickUsername(t *testing.T) {
	cases := []struct {
		name   string
		body   string
		want   string
		wantOK bool
	}{
		{"flat", `{"username":"alice","id":1}`, "alice", true},
		{"data wrapper", `{"data":{"username":"bob","id":2}}`, "bob", true},
		{"user wrapper", `{"user":{"username":"carol","id":3}}`, "carol", true},
		{"empty flat", `{"username":""}`, "", false},
		{"missing", `{"id":1}`, "", false},
		{"not json", `<html>cf</html>`, "", false},
		{"empty wrapped", `{"data":{"username":""}}`, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseKickUsername([]byte(tc.body))
			assert.Equal(t, tc.wantOK, ok)
			assert.Equal(t, tc.want, got)
		})
	}
}
