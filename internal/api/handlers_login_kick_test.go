package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// parseKickChannels must accept the various separator styles operators
// paste into the form. The web template advertises "comma/space-
// separated"; the helper CLI joins with commas. Both must round-trip.
func TestParseKickChannels(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"empty", "", nil},
		{"single", "alice", []string{"alice"}},
		{"csv", "alice,bob,carol", []string{"alice", "bob", "carol"}},
		{"spaces", "alice bob carol", []string{"alice", "bob", "carol"}},
		{"mixed", "alice, bob; carol\tdave", []string{"alice", "bob", "carol", "dave"}},
		{"dedupe", "Alice,alice,ALICE,bob", []string{"Alice", "bob"}},
		{"trim", "  alice  ,  bob  ", []string{"alice", "bob"}},
		{"empty parts", ",,alice,,,bob,,", []string{"alice", "bob"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, parseKickChannels(tc.in))
		})
	}
}
