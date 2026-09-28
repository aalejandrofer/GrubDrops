package twitch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

func TestProfileFor(t *testing.T) {
	assert.Equal(t, clientID, profileFor("").ID, "legacy sessions stay Android")
	assert.Equal(t, "ue6666qo983tsx6so1t0vnawi233wa", profileFor(ClientTV).ID)
	assert.Equal(t, clientID, profileFor("bogus").ID, "unknown falls back to Android")
}

// Each token must be sent under the Client-Id of the client that minted it.
func TestClientIDFollowsSession(t *testing.T) {
	var mu sync.Mutex
	seen := map[string]string{} // Authorization -> Client-Id
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen[r.Header.Get("Authorization")] = r.Header.Get("Client-Id")
		mu.Unlock()
		_, _ = w.Write([]byte(`{"data":{"currentUser":{"inventory":{"dropCampaignsInProgress":[]}}}}`))
	}))
	defer srv.Close()

	b := newForTest(srv.URL)
	ctx := context.Background()
	_, err := b.InventoryProgress(ctx, platform.Session{AccessToken: "tok_android"})
	require.NoError(t, err)
	_, err = b.InventoryProgress(ctx, platform.Session{AccessToken: "tok_tv", ClientID: ClientTV})
	require.NoError(t, err)

	assert.Equal(t, clientID, seen["OAuth tok_android"])
	assert.Equal(t, profileTV.ID, seen["OAuth tok_tv"])
}

// Review Focus #2: the heartbeat carries only the token; StartWatch must
// bind the session so the beacon uses the TV Client-Id.
func TestHeartbeatUsesBoundClient(t *testing.T) {
	var beaconClient string
	var channelPagePath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/beacon":
			beaconClient = r.Header.Get("Client-Id")
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet:
			// channel page scraped for spade_url
			channelPagePath = r.URL.Path
			_, _ = w.Write([]byte(`"spade_url":"` + "http://" + r.Host + `/beacon"`))
		default:
			_, _ = w.Write([]byte(`{"data":{"currentUser":{"id":"42"}}}`))
		}
	}))
	defer srv.Close()

	b := newForTest(srv.URL)
	b.c.beaconHostAllow = func(string) bool { return true } // test-only: see Step 4
	s := platform.Session{AccessToken: "tok_tv", ClientID: ClientTV}
	h, err := b.StartWatch(context.Background(), s, platform.Stream{Channel: "chan", ChannelID: "1", BroadcastID: "2", GameID: "3", Game: "G"})
	require.NoError(t, err)
	require.NoError(t, b.Heartbeat(context.Background(), h))
	assert.Equal(t, profileTV.ID, beaconClient)
	// Guard per LCBRST/TwitchDropsMiner-CLI 8a7f516: pages served to
	// app-client profiles don't link the spade URL, so the channel page
	// must always be fetched from the web home URL — never derived from
	// the (TV) client profile.
	assert.Equal(t, "/chan", channelPagePath)
}
