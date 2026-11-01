package platform

import "testing"

func TestEffectivePointsMode(t *testing.T) {
	for _, tc := range []struct {
		plat   string
		stored PointsMode
		want   PointsMode
	}{
		{"twitch", PointsOff, PointsOff},
		{"twitch", PointsDropsPoints, PointsDropsPoints},
		{"twitch", PointsOnly, PointsOnly},
		{"kick", PointsOff, PointsDropsPoints},
		{"kick", PointsOnly, PointsDropsPoints},
		{"kick", "", PointsDropsPoints},
	} {
		if got := EffectivePointsMode(tc.plat, tc.stored); got != tc.want {
			t.Errorf("EffectivePointsMode(%q, %q) = %q, want %q", tc.plat, tc.stored, got, tc.want)
		}
	}
	if !PointsSupported("twitch") || PointsSupported("kick") {
		t.Error("PointsSupported: want Twitch only")
	}
}
