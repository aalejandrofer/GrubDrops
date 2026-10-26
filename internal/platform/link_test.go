package platform

import "testing"

func TestExternalLinkURL(t *testing.T) {
	cases := map[string]bool{
		"":                                      false,
		"https://www.twitch.tv/drops/campaigns": false,
		"https://www.twitch.tv/drops/campaigns?dropID=8d6f1b2e": false,
		"https://twitch.tv/":                                false,
		"https://dashboard.twitch.tv/":                      false,
		"HTTPS://WWW.TWITCH.TV./drops":                      false,
		"javascript:alert(1)":                               false,
		"/drops/campaigns":                                  false,
		"https://eviltwitch.tv/link":                        true,
		"https://account.riotgames.com/#connected-accounts": true,
		"https://www.ubisoft.com/en-us/help/connectivity-and-performance/article/linking-your-ubisoft-account-to-twitch/000062476": true,
		"https://account.mojang.com/twitch": true,
	}
	for raw, want := range cases {
		if got := ExternalLinkURL(raw); got != want {
			t.Errorf("ExternalLinkURL(%q) = %v, want %v", raw, got, want)
		}
	}
}

func TestTwitchNeedsLink(t *testing.T) {
	riot := "https://account.riotgames.com/#connected-accounts"
	cases := []struct {
		name string
		c    Campaign
		want bool
	}{
		{"badge campaign, twitch drops page", Campaign{AccountLinkURL: "https://www.twitch.tv/drops/campaigns"}, false},
		{"no link url", Campaign{}, false},
		{"publisher, not connected", Campaign{AccountLinkURL: riot}, true},
		{"publisher, connected", Campaign{AccountLinked: true, AccountLinkURL: riot}, false},
	}
	for _, tc := range cases {
		if got := TwitchNeedsLink(tc.c); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}
