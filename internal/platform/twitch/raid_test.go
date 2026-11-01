package twitch

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// raid_update_v2 on raid.<id> reaches OnRaid with the topic's channel and
// the raid ID. Other raid messages, and raids from another source, do not.
func TestPubSub_DispatchRaidUpdate(t *testing.T) {
	type call struct{ channel, raid string }
	var got []call
	p := NewPubSubClient("", PubSubHandlers{
		OnRaid: func(channelID, raidID string) { got = append(got, call{channelID, raidID}) },
	})
	p.dispatch("raid.123", []byte(`{"type":"raid_update_v2","raid":{"id":"raid-1","source_id":"123","target_id":"456","target_login":"other"}}`))
	p.dispatch("raid.123", []byte(`{"type":"raid_go_v2","raid":{"id":"raid-1","source_id":"123"}}`))
	p.dispatch("raid.123", []byte(`{"type":"raid_cancel_v2","raid":{"id":"raid-1","source_id":"123"}}`))
	p.dispatch("raid.123", []byte(`{"type":"raid_update_v2","raid":{"id":"raid-2","source_id":"999"}}`))
	p.dispatch("raid.123", []byte(`{"type":"raid_update_v2","raid":{"id":"","source_id":"123"}}`))
	p.dispatch("raid.123", []byte(`not json`))
	assert.Equal(t, []call{{"123", "raid-1"}}, got)
	assert.Equal(t, "raid.123", TopicRaid("123"))
}

// The direct backend passes OnRaid into its PubSub handlers.
func TestBackend_RaidHookWired(t *testing.T) {
	b := &Backend{}
	var fired string
	b.SetAccountPubSubHooks("acct", platform.PubSubHooks{OnRaid: func(_, raidID string) { fired = raidID }})
	require.NotNil(t, b.pubsubHandlers.OnRaid)
	b.pubsubHandlers.OnRaid("1", "raid-1")
	assert.Equal(t, "raid-1", fired)
}

// SubscribeRaids adds the raid topic to the account's socket and
// UnsubscribeRaids removes it; the stream topics are untouched.
func TestBackend_SubscribeRaidsTopic(t *testing.T) {
	client := NewPubSubClient("", PubSubHandlers{})
	b := &Backend{pubsub: client}
	b.SubscribeRaids("acct", "123")
	client.mu.Lock()
	_, has := client.topics["raid.123"]
	n := len(client.topics)
	client.mu.Unlock()
	assert.True(t, has, "raid topic not added")
	assert.Equal(t, 1, n)

	b.UnsubscribeRaids("acct", "123")
	client.mu.Lock()
	_, has = client.topics["raid.123"]
	client.mu.Unlock()
	assert.False(t, has, "raid topic kept")
}

// JoinRaid sends one JoinRaid mutation carrying the raid ID.
func TestBackend_JoinRaidRequest(t *testing.T) {
	srv, seen := pointsServer(t, map[string]string{
		"JoinRaid": `{"data":{"joinRaid":{"raidID":"raid-1"}}}`,
	})
	b := newForTest(srv.URL)
	require.NoError(t, b.JoinRaid(context.Background(), platform.Session{AccessToken: "tok"}, "raid-1"))
	reqs := seen()
	require.Len(t, reqs, 1)
	assert.Equal(t, "JoinRaid", reqs[0].Op)
	assert.Equal(t, OpJoinRaid.Hash, reqs[0].Hash)
	assert.Equal(t, map[string]any{"input": map[string]any{"raidID": "raid-1"}}, reqs[0].Variables)
	assert.Empty(t, reqs[0].Header.Get("Client-Integrity"))

	require.Error(t, b.JoinRaid(context.Background(), platform.Session{AccessToken: "tok"}, ""))
	assert.Len(t, seen(), 1, "empty raid ID must not send a request")
}
