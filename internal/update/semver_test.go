package update

import "testing"

func TestNewer(t *testing.T) {
	cases := []struct {
		current, latest string
		want            bool
	}{
		{"v1.3.4", "v1.3.5", true},          // newer patch
		{"1.3.4", "1.4.0", true},            // newer minor, no v prefix
		{"v1.3.5", "v1.3.5", false},         // equal
		{"v1.3.5", "v1.3.4", false},         // older latest
		{"v2.0.0", "v1.9.9", false},         // older major
		{"v1.3.4-itempull", "v1.3.5", true}, // build suffix on current
		{"v1.3.4", "v1.3.5+build", true},    // build suffix on latest
		{"v1.3", "v1.3.1", true},            // missing patch defaults to 0
		{"", "v1.3.5", false},               // empty current -> no false positive
		{"dev", "v1.3.5", false},            // unparseable current
		{"v1.3.4", "", false},               // empty latest
		{"v1.3.4", "garbage", false},        // unparseable latest
	}
	for _, c := range cases {
		if got := Newer(c.current, c.latest); got != c.want {
			t.Errorf("Newer(%q,%q) = %v, want %v", c.current, c.latest, got, c.want)
		}
	}
}
