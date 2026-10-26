package web

import (
	"io/fs"
	"testing"
)

// TestDistHasIndex guards the SPA fallback: the router serves index.html for
// every unknown path, so the embed must always carry one.
func TestDistHasIndex(t *testing.T) {
	if _, err := fs.Stat(Dist(), "index.html"); err != nil {
		t.Fatalf("dist/index.html missing: %v", err)
	}
}
