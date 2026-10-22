package twitch

import (
	"context"
	"net"
	"sync/atomic"
	"testing"

	"github.com/aalejandrofer/grubdrops/internal/platform"
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

func TestPubSubDispatchRaidUpdateV2(t *testing.T) {
	var sourceID, raidID string
	p := NewPubSubClient("", PubSubHandlers{
		OnRaid: func(channelID, id string) {
			sourceID, raidID = channelID, id
		},
	})
	p.dispatch("raid.123", []byte(`{"type":"raid_update_v2","raid":{"id":"raid-1","source_id":"123"}}`))
	if sourceID != "123" || raidID != "raid-1" {
		t.Fatalf("raid callback = (%q, %q), want (%q, %q)", sourceID, raidID, "123", "raid-1")
	}

	p.dispatch("raid.123", []byte(`{"type":"raid_go_v2","raid":{"id":"raid-2","source_id":"123"}}`))
	p.dispatch("raid.123", []byte(`{"type":"raid_update_v2","raid":{"id":"raid-3","source_id":"wrong"}}`))
	if sourceID != "123" || raidID != "raid-1" {
		t.Fatalf("ignored raid events changed callback to (%q, %q)", sourceID, raidID)
	}
	if got := TopicRaid("123"); got != "raid.123" {
		t.Fatalf("TopicRaid() = %q, want raid.123", got)
	}
}

func TestSetAccountPubSubHooksUpdatesExistingClient(t *testing.T) {
	for _, tc := range []struct {
		name    string
		install func(*PubSubClient, platform.PubSubHooks)
	}{
		{
			name: "direct backend",
			install: func(client *PubSubClient, hooks platform.PubSubHooks) {
				(&Backend{pubsub: client}).SetAccountPubSubHooks("acct", hooks)
			},
		},
		{
			name: "browser backend",
			install: func(client *PubSubClient, hooks platform.PubSubHooks) {
				b := &BrowserBackend{
					pubsubs:     map[string]*PubSubClient{"acct": client},
					pubsubHooks: map[string]platform.PubSubHooks{},
				}
				b.SetAccountPubSubHooks("acct", hooks)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got string
			client := NewPubSubClient("", PubSubHandlers{})
			tc.install(client, platform.PubSubHooks{
				OnRaid: func(_, raidID string) { got = raidID },
			})
			client.dispatch("raid.123", []byte(`{"type":"raid_update_v2","raid":{"id":"raid-1","source_id":"123"}}`))
			if got != "raid-1" {
				t.Fatalf("updated PubSub callback received raid %q, want raid-1", got)
			}
		})
	}
}
