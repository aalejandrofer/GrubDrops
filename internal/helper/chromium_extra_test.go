package helper

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestExtraChromiumCookieDBs_IncludesOperaGX(t *testing.T) {
	dbs := extraChromiumCookieDBs()
	if len(dbs) == 0 {
		t.Fatal("no candidate cookie DBs returned for this OS")
	}
	foundGX := false
	for _, p := range dbs {
		if strings.Contains(strings.ToLower(p), "operagx") || strings.Contains(strings.ToLower(p), "opera gx") {
			foundGX = true
		}
		if filepath.Base(p) != "Cookies" {
			t.Errorf("candidate should point at a Cookies DB, got %s", p)
		}
	}
	if !foundGX {
		t.Errorf("Opera GX cookie DB missing from candidates: %v", dbs)
	}
}
