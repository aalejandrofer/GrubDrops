package twitch

import "testing"

func TestBoxArtURL(t *testing.T) {
	cases := []struct{ name, raw, id, want string }{
		{"gql template filled", "https://static-cdn.jtvnw.net/ttv-boxart/1448964995_IGDB-{width}x{height}.jpg", "1448964995",
			"https://static-cdn.jtvnw.net/ttv-boxart/1448964995_IGDB-144x192.jpg"},
		{"gql sized url kept", "https://static-cdn.jtvnw.net/ttv-boxart/856072301_IGDB-285x380.jpg", "856072301",
			"https://static-cdn.jtvnw.net/ttv-boxart/856072301_IGDB-285x380.jpg"},
		{"no gql url: igdb id form", "", "1736572793",
			"https://static-cdn.jtvnw.net/ttv-boxart/1736572793_IGDB-144x192.jpg"},
		{"off-cdn url ignored", "https://evil.example/x.jpg", "458443",
			"https://static-cdn.jtvnw.net/ttv-boxart/458443_IGDB-144x192.jpg"},
		{"nothing known", "", "", ""},
	}
	for _, tc := range cases {
		if got := BoxArtURL(tc.raw, tc.id); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
	if got := BoxArtByName("Dead by Daylight"); got != "https://static-cdn.jtvnw.net/ttv-boxart/Dead%20by%20Daylight-144x192.jpg" {
		t.Errorf("BoxArtByName = %q", got)
	}
}
