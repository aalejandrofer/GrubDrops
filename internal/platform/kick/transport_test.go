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

func TestNewUTLSConn_UsesProxyDial(t *testing.T) {
	var used int32
	old := wsProxyDial
	t.Cleanup(func() { wsProxyDial = old })
	wsProxyDial = func(ctx context.Context, network, addr string) (net.Conn, error) {
		atomic.AddInt32(&used, 1)
		return nil, context.Canceled
	}
	_, _ = newUTLSConn(context.Background(), "tcp", "web.kick.com:443")
	if atomic.LoadInt32(&used) == 0 {
		t.Fatal("newUTLSConn did not use wsProxyDial")
	}
}
