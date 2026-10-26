package web

import (
	"io/fs"
	"testing"
)

// TestDistEmbeds guards the embed: dist/ always carries at least .gitkeep, so
// go:embed compiles before the SPA is built.
func TestDistEmbeds(t *testing.T) {
	if _, err := fs.Stat(Dist(), ".gitkeep"); err != nil {
		t.Fatalf("dist/.gitkeep missing: %v", err)
	}
}
