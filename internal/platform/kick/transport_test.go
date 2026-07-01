package kick

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
)

func TestHTTPDoer_UsesInjectedDialer(t *testing.T) {
	var used int32
	dial := func(ctx context.Context, network, addr string) (net.Conn, error) {
		atomic.AddInt32(&used, 1)
		return nil, context.Canceled // short-circuit; we only assert it was called
	}
	d := newHTTPDoer(dial)
	_, _ = d.connFor(context.Background(), "web.kick.com")
	if atomic.LoadInt32(&used) == 0 {
		t.Fatal("httpDoer did not use the injected dialer")
	}
}
