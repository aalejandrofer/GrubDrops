package store

import (
	"strings"
	"testing"
)

func TestNewClaimID_PrefixedAndUnique(t *testing.T) {
	a := NewClaimID()
	b := NewClaimID()
	if !strings.HasPrefix(a, "clm_") {
		t.Fatalf("id %q missing clm_ prefix", a)
	}
	if a == b {
		t.Fatalf("two ids collided: %q", a)
	}
}
