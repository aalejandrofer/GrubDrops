package twitch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// androidCamp is a catalog campaign carrying the Android account's state.
func androidCamp(id, game string) platform.Campaign {
	return platform.Campaign{
		ID: id, Platform: "twitch", Game: game, GameID: "263490", Name: "Shared " + id,
		BoxArtURL: "http://art", EndsAt: time.Now().Add(24 * time.Hour), Status: "active", Kind: "drop",
		AccountLinked: true, AccountLinkChecked: true, ClaimNeedsLink: true,
		AccountLinkURL:  "https://www.twitch.tv/drops/campaigns?id=" + id,
		AllowedChannels: []string{"gamma", "delta"}, AllowedChannelCount: 2,
		Benefits: []platform.DropBenefit{{
			ID: "d" + id, CampaignID: id, Name: "Hat", RequiredMinutes: 30, ImageURL: "http://img",
			RewardID: "r" + id, InstanceID: "android#" + id + "#d" + id, Preconditions: []string{"p1"},
		}},
	}
}

func TestMergeShared_AddsOnlyMissing(t *testing.T) {
	own := []platform.Campaign{{ID: "campA", Name: "mine"}}
	add := mergeShared(own, []platform.Campaign{androidCamp("campA", "Rust"), androidCamp("campX", "Rust"), androidCamp("campX", "Rust")}, nil, time.Now())
	require.Len(t, add, 1, "own campaign wins and a duplicate shared row adds once")
	assert.Equal(t, "campX", add[0].ID)
	assert.Equal(t, "mine", own[0].Name, "own campaign untouched")
}

func TestMergeShared_StripsAndroidState(t *testing.T) {
	src := androidCamp("campX", "Rust")
	shared := []platform.Campaign{src}
	add := mergeShared(nil, shared, nil, time.Now())
	require.Len(t, add, 1)
	c := add[0]
	assert.False(t, c.AccountLinkChecked, "Android link state is not the TV account's")
	assert.True(t, c.AccountLinked, "a twitch.tv link page needs no link")
	assert.False(t, c.ClaimNeedsLink)
	assert.Equal(t, []string{"gamma", "delta"}, c.AllowedChannels)
	assert.Equal(t, 2, c.AllowedChannelCount)
	require.Len(t, c.Benefits, 1)
	b := c.Benefits[0]
	assert.Empty(t, b.InstanceID, "never an Android drop instance id")
	assert.Equal(t, "dcampX", b.ID)
	assert.Equal(t, "campX", b.CampaignID)
	assert.Equal(t, "rcampX", b.RewardID)
	assert.Equal(t, 30, b.RequiredMinutes)
	assert.Equal(t, []string{"p1"}, b.Preconditions)

	// The source, shared with the Discover page, is never changed.
	assert.Equal(t, "android#campX#dcampX", shared[0].Benefits[0].InstanceID)
	assert.True(t, shared[0].AccountLinkChecked)
	c.AllowedChannels[0] = "x"
	c.Benefits[0].Preconditions[0] = "x"
	assert.Equal(t, "gamma", shared[0].AllowedChannels[0])
	assert.Equal(t, "p1", shared[0].Benefits[0].Preconditions[0])
}

// A publisher link page with unknown TV link state is reported unlinked, so
// the plan flags it and a refused claim blocks it.
func TestMergeShared_PublisherLinkIsUnlinked(t *testing.T) {
	src := androidCamp("campX", "Rust")
	src.AccountLinkURL = "https://account.riotgames.com/link"
	add := mergeShared(nil, []platform.Campaign{src}, nil, time.Now())
	require.Len(t, add, 1)
	assert.False(t, add[0].AccountLinked)
	assert.False(t, add[0].AccountLinkChecked)
	assert.True(t, platform.TwitchNeedsLink(add[0]))
}

func TestMergeShared_Skips(t *testing.T) {
	now := time.Now()
	mk := func(f func(*platform.Campaign)) platform.Campaign {
		c := androidCamp("c", "Rust")
		f(&c)
		return c
	}
	cases := map[string]platform.Campaign{
		"no id":       mk(func(c *platform.Campaign) { c.ID = "" }),
		"kick":        mk(func(c *platform.Campaign) { c.Platform = "kick" }),
		"upcoming":    mk(func(c *platform.Campaign) { c.Status = "upcoming" }),
		"ended":       mk(func(c *platform.Campaign) { c.EndsAt = now.Add(-time.Minute) }),
		"not started": mk(func(c *platform.Campaign) { c.StartsAt = now.Add(time.Hour) }),
		"reward":      mk(func(c *platform.Campaign) { c.Kind = "reward" }),
		"no drops":    mk(func(c *platform.Campaign) { c.Benefits = nil }),
		"other game":  mk(func(c *platform.Campaign) { c.Game = "Dota 2" }),
	}
	filter := func(g string) bool { return g == "Rust" }
	for name, c := range cases {
		assert.Empty(t, mergeShared(nil, []platform.Campaign{c}, filter, now), name)
	}
	assert.Len(t, mergeShared(nil, []platform.Campaign{androidCamp("c", "Dota 2")}, nil, now), 1, "no filter, the plan filters")
}

// tvServer serves the TV discovery ops from chandisc_test.go.
func tvServer(t *testing.T) *httptest.Server {
	return fakeGQL(t, map[string]func(map[string]any) string{
		"DirectoryPage_Game": func(map[string]any) string { return dirRust },
		"DropsHighlightService_AvailableDrops": func(v map[string]any) string {
			if v["channelID"] == "c1" {
				return availAlpha
			}
			return `{"data":{"channel":{"viewerDropCampaigns":[]}}}`
		},
		"Inventory": func(map[string]any) string { return inventoryTV },
	})
}

func tvSession() platform.Session {
	return platform.Session{AccessToken: "tv", ClientID: ClientTV, Games: []string{"Rust"},
		GameFilter: func(g string) bool { return g == "Rust" }}
}

// Off, empty, or failing, the shared source leaves the TV scan unchanged.
func TestListActiveCampaigns_TVSharedOffIsUnchanged(t *testing.T) {
	srv := tvServer(t)
	defer srv.Close()
	ctx := context.Background()

	base := newForTest(srv.URL)
	want, err := base.ListActiveCampaigns(ctx, tvSession())
	require.NoError(t, err)

	sources := map[string]SharedCatalog{
		"nil": nil,
		"off": func(context.Context) ([]platform.Campaign, error) { return nil, nil },
		"error": func(context.Context) ([]platform.Campaign, error) {
			return []platform.Campaign{androidCamp("campX", "Rust")}, errors.New("boom")
		},
		"own": func(context.Context) ([]platform.Campaign, error) {
			return []platform.Campaign{androidCamp("campA", "Rust")}, nil
		},
	}
	for name, src := range sources {
		b := newForTest(srv.URL)
		b.SetSharedCatalog(src)
		got, err := b.ListActiveCampaigns(ctx, tvSession())
		require.NoError(t, err, name)
		assert.Equal(t, want, got, name)
		assert.Equal(t, base.allowedLoginsByCampaign, b.allowedLoginsByCampaign, name)
		assert.Equal(t, base.tvDetails, b.tvDetails, name)
	}
}

func TestListActiveCampaigns_TVSharedAddsMissing(t *testing.T) {
	srv := tvServer(t)
	defer srv.Close()
	ctx := context.Background()

	inv := androidCamp("campB", "Rust") // in the TV Inventory: own wins
	inv.Name = "android name"
	b := newForTest(srv.URL)
	calls := 0
	b.SetSharedCatalog(func(context.Context) ([]platform.Campaign, error) {
		calls++
		return []platform.Campaign{inv, androidCamp("campX", "Rust"), androidCamp("campY", "Dota 2")}, nil
	})
	camps, err := b.ListActiveCampaigns(ctx, tvSession())
	require.NoError(t, err)
	assert.Equal(t, 1, calls)

	byID := map[string]platform.Campaign{}
	for _, c := range camps {
		byID[c.ID] = c
	}
	require.Len(t, byID, 3, "campA and campB own, campX shared, campY off the whitelist")
	assert.Equal(t, "Rust Isles Tac Gloves", byID["campB"].Name, "own campaign is never overwritten")
	assert.True(t, byID["campB"].AccountLinkChecked)

	x := byID["campX"]
	assert.False(t, x.AccountLinkChecked)
	assert.Empty(t, x.Benefits[0].InstanceID)
	assert.Equal(t, 2, b.AllowedChannelCount("campX"), "Twitch's allow-list picks the channels")
	assert.Equal(t, 1, b.AllowedChannelCount("campB"), "own allow-list untouched")

	got, err := b.CampaignDetails(ctx, tvSession(), "campX")
	require.NoError(t, err)
	assert.Equal(t, x.Benefits, got, "TV details serve the shared drops")
}

// An open shared campaign clears a stale allow-list so the directory serves
// any live channel of the game.
func TestListActiveCampaigns_TVSharedOpenCampaignUsesDirectory(t *testing.T) {
	srv := tvServer(t)
	defer srv.Close()
	b := newForTest(srv.URL)
	b.setAllowedLogins("campX", []string{"stale"})
	open := androidCamp("campX", "Rust")
	open.AllowedChannels, open.AllowedChannelCount = nil, 0
	b.SetSharedCatalog(func(context.Context) ([]platform.Campaign, error) { return []platform.Campaign{open}, nil })
	_, err := b.ListActiveCampaigns(context.Background(), tvSession())
	require.NoError(t, err)
	assert.Equal(t, 0, b.AllowedChannelCount("campX"))
}

func TestListActiveCampaigns_AndroidIgnoresShared(t *testing.T) {
	srv := fakeGQL(t, map[string]func(map[string]any) string{
		"ViewerDropsDashboard": func(map[string]any) string { return `{"data":{"currentUser":{"dropCampaigns":[]}}}` },
	})
	defer srv.Close()
	b := newForTest(srv.URL)
	b.SetSharedCatalog(func(context.Context) ([]platform.Campaign, error) {
		t.Fatal("an Android session must never read the shared catalog")
		return nil, nil
	})
	camps, err := b.ListActiveCampaigns(context.Background(), platform.Session{AccessToken: "a", Games: []string{"Rust"}})
	require.NoError(t, err)
	assert.Empty(t, camps)
}

// A shared drop is claimed under the TV account's own synthetic instance id.
func TestClaim_SharedDropUsesOwnInstanceID(t *testing.T) {
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if bytes.Contains(b, []byte(OpClaimDrop.Name)) {
			body = b // the post-claim inventory read is not the claim
		}
		_, _ = w.Write(loadFixture(t, "claim_ok.json"))
	}))
	defer srv.Close()

	add := mergeShared(nil, []platform.Campaign{androidCamp("campX", "Rust")}, nil, time.Now())
	require.Len(t, add, 1)
	cl := &claimer{c: newTestClient(srv.URL)}
	require.NoError(t, cl.claim(context.Background(), platform.Session{AccessToken: "tv"}, add[0].Benefits[0], 77))

	var req struct {
		Variables struct {
			Input struct {
				DropInstanceID string `json:"dropInstanceID"`
			} `json:"input"`
		} `json:"variables"`
	}
	require.NoError(t, json.Unmarshal(body, &req))
	assert.Equal(t, "77#campX#dcampX", req.Variables.Input.DropInstanceID)
}
