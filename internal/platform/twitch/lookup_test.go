package twitch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

const liveStreamJSON = `{"data":{"user":{"id":"491062114","login":"keshaeuw","stream":{"id":"b777","viewersCount":321,"game":{"id":"g9","displayName":"Rust"}}}}}`

func TestLookupChannel_LiveAndOffline(t *testing.T) {
	body := liveStreamJSON
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req gqlRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		assert.Equal(t, OpGetStreamInfo.Name, req.OperationName)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	b := newForTest(srv.URL)
	sess := platform.Session{AccessToken: "tok"}

	st, live, err := b.LookupChannel(context.Background(), sess, "keshaeuw")
	require.NoError(t, err)
	require.True(t, live)
	assert.Equal(t, "keshaeuw", st.Channel)
	assert.Equal(t, "491062114", st.ChannelID)
	assert.Equal(t, "b777", st.BroadcastID)
	assert.Equal(t, "g9", st.GameID)
	assert.Equal(t, "Rust", st.Game)
	assert.Equal(t, 321, st.ViewerCount)

	body = `{"data":{"user":{"login":"keshaeuw","stream":null}}}`
	_, live, err = b.LookupChannel(context.Background(), sess, "keshaeuw")
	require.NoError(t, err)
	assert.False(t, live)
}

func TestLookupChannel_NoIntegrityHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Empty(t, r.Header.Get("Client-Integrity"))
		_, _ = w.Write([]byte(liveStreamJSON))
	}))
	defer srv.Close()

	b := newForTest(srv.URL)
	_, _, err := b.LookupChannel(context.Background(), platform.Session{AccessToken: "tok"}, "keshaeuw")
	require.NoError(t, err)
}

func TestReadyDrops(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req gqlRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		assert.Equal(t, OpInventory.Name, req.OperationName)
		_, _ = w.Write([]byte(`{"data":{"currentUser":{"inventory":{"dropCampaignsInProgress":[
		  {"id":"campA","name":"Spring Drops","game":{"name":"Rust"},"accountLinkURL":"https://rust.example/link","self":{"isAccountConnected":true},"timeBasedDrops":[
		    {"id":"ready","name":"Ready Drop","requiredMinutesWatched":60,"self":{"currentMinutesWatched":60,"isClaimed":false,"dropInstanceID":"inst-ready"}},
		    {"id":"partial","name":"Partial","requiredMinutesWatched":60,"self":{"currentMinutesWatched":30,"isClaimed":false,"dropInstanceID":"inst-partial"}},
		    {"id":"claimed","name":"Claimed","requiredMinutesWatched":60,"self":{"currentMinutesWatched":60,"isClaimed":true,"dropInstanceID":"inst-claimed"}},
		    {"id":"noinst","name":"No Instance","requiredMinutesWatched":10,"self":{"currentMinutesWatched":10,"isClaimed":false,"dropInstanceID":""}}
		  ]}]}}}}`))
	}))
	defer srv.Close()

	b := newForTest(srv.URL)
	out, err := b.ReadyDrops(context.Background(), platform.Session{AccessToken: "tok"})
	require.NoError(t, err)
	require.Len(t, out, 1)
	assert.Equal(t, "ready", out[0].Benefit.ID)
	assert.Equal(t, "campA", out[0].Benefit.CampaignID)
	assert.Equal(t, "inst-ready", out[0].Benefit.InstanceID)
	assert.Equal(t, 60, out[0].Benefit.RequiredMinutes)
	assert.Equal(t, "Ready Drop", out[0].Benefit.Name)
	assert.Equal(t, platform.Campaign{
		ID: "campA", Platform: "twitch", Name: "Spring Drops", Game: "Rust",
		AccountLinked: true, AccountLinkChecked: true, AccountLinkURL: "https://rust.example/link",
	}, out[0].Campaign)
}
