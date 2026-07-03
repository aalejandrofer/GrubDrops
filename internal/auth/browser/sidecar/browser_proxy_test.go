package sidecar

import "testing"

func TestProxyAllocOpts_EmptyIsNil(t *testing.T) {
	if got := proxyAllocOpts(""); got != nil {
		t.Fatalf("expected nil for empty proxy, got %d opts", len(got))
	}
}

func TestProxyAllocOpts_SetReturnsOne(t *testing.T) {
	if got := proxyAllocOpts("socks5://127.0.0.1:1080"); len(got) != 1 {
		t.Fatalf("expected 1 opt for a configured proxy, got %d", len(got))
	}
}
