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
