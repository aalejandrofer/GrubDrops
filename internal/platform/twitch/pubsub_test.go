package twitch

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
)

func TestPubSub_UsesProxyDial(t *testing.T) {
	var used int32
	dial := func(ctx context.Context, network, addr string) (net.Conn, error) {
		atomic.AddInt32(&used, 1)
		return nil, context.Canceled
	}
	p := newPubSubClientWithDial(dial)
	_ = p.dialAndPump(context.Background())
	if atomic.LoadInt32(&used) == 0 {
		t.Fatal("PubSub did not use the injected dialer")
	}
}

func TestPubSub_RaidDispatchRequiresMatchingSourceAndEvent(t *testing.T) {
	var gotChannel, gotRaid string
	p := &PubSubClient{handlers: PubSubHandlers{OnRaid: func(channelID, raidID string) {
		gotChannel, gotRaid = channelID, raidID
	}}}
	p.dispatch(TopicRaid("source-17"), []byte(`{"type":"raid_update_v2","raid":{"id":"raid-42","source_id":"source-17"}}`))
	if gotChannel != "source-17" || gotRaid != "raid-42" {
		t.Fatalf("raid callback = (%q, %q), want source-17 and raid-42", gotChannel, gotRaid)
	}
	for _, payload := range []string{
		`{"type":"raid_update_v2","raid":{"id":"raid-42","source_id":"another-source"}}`,
		`{"type":"raid","raid":{"id":"raid-42","source_id":"source-17"}}`,
	} {
		gotChannel, gotRaid = "", ""
		p.dispatch(TopicRaid("source-17"), []byte(payload))
		if gotChannel != "" || gotRaid != "" {
			t.Fatalf("unexpected callback for %s: (%q, %q)", payload, gotChannel, gotRaid)
		}
	}
	if TopicRaid("source-17") != "raid.source-17" {
		t.Fatalf("unexpected raid topic %q", TopicRaid("source-17"))
	}
}

func TestBackend_SubscribeChannelManagesRaidTopic(t *testing.T) {
	p := &PubSubClient{topics: map[string]struct{}{}}
	b := &Backend{pubsub: p}
	b.SubscribeChannel("account", "channel-1")
	p.mu.Lock()
	_, present := p.topics[TopicRaid("channel-1")]
	p.mu.Unlock()
	if !present {
		t.Fatal("raid topic was not subscribed")
	}
	b.UnsubscribeChannel("account", "channel-1")
	p.mu.Lock()
	_, present = p.topics[TopicRaid("channel-1")]
	p.mu.Unlock()
	if present {
		t.Fatal("raid topic was not unsubscribed")
	}
}
