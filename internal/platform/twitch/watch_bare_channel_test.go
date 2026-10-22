package twitch

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A force-watch starts from a bare channel name. Twitch discards beacons that
// lack broadcast/game ids, so start must resolve them, and must refuse an
// offline channel so the watcher moves on to the next one.
func TestWatchStartResolvesBareChannel(t *testing.T) {
	live := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			OperationName string `json:"operationName"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		switch req.OperationName {
		case "VideoPlayerStreamInfoOverlayChannel":
			if live {
				_, _ = fmt.Fprint(w, `{"data":{"user":{"id":"77","login":"xqc","stream":{"id":"bc-9","viewersCount":5,"game":{"id":"g1","displayName":"Just Chatting"}}}}}`)
			} else {
				_, _ = fmt.Fprint(w, `{"data":{"user":{"id":"77","login":"xqc","stream":null}}}`)
			}
		case "ChannelPointsContext":
			_, _ = fmt.Fprint(w, `{"data":{"community":{"channel":{"id":"77","self":{"communityPoints":{"availableClaim":null}}}}}}`)
		default:
			t.Errorf("unexpected operation %q", req.OperationName)
		}
	}))
	defer srv.Close()

	wt := &watch{c: newTestClient(srv.URL), cachedUserID: 42, spadeURLs: map[string]string{}}
	sess := platform.Session{AccessToken: "tok"}

	h, err := wt.start(context.Background(), sess, platform.Stream{Channel: "xqc"})
	require.NoError(t, err)
	in := h.Internal.(watchInternal)
	assert.Equal(t, "bc-9", in.BroadcastID)
	assert.Equal(t, "g1", in.GameID)
	assert.Equal(t, "Just Chatting", in.Game)
	assert.Equal(t, "77", in.ChannelID)

	live = false
	_, err = wt.start(context.Background(), sess, platform.Stream{Channel: "xqc"})
	require.ErrorContains(t, err, "offline")
}
