package sidecar

import "testing"

func TestIsCampaignsQuery(t *testing.T) {
	cases := []struct {
		name string
		body string
		want bool
	}{
		{"viewer drops dashboard", `{"operationName":"ViewerDropsDashboard","variables":{}}`, true},
		{"inventory", `{"operationName":"Inventory","variables":{}}`, false},
		{"empty", ``, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isCampaignsQuery([]byte(c.body)); got != c.want {
				t.Errorf("isCampaignsQuery(%q) = %v, want %v", c.body, got, c.want)
			}
		})
	}
}

func TestIsInventoryQuery(t *testing.T) {
	cases := []struct {
		name string
		body string
		want bool
	}{
		{"inventory exact", `{"operationName":"Inventory","variables":{}}`, true},
		{"campaigns", `{"operationName":"ViewerDropsDashboard"}`, false},
		{"empty", ``, false},
		// Must match the quoted form so we don't false-positive on
		// fields that happen to mention "Inventory" inside variables.
		{"unquoted substring should not match", `{"some_field":"Inventory data"}`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isInventoryQuery([]byte(c.body)); got != c.want {
				t.Errorf("isInventoryQuery(%q) = %v, want %v", c.body, got, c.want)
			}
		})
	}
}

func TestGqlCampaignsEmpty(t *testing.T) {
	cases := []struct {
		name string
		body string
		want bool
	}{
		{"empty array", `{"data":{"currentUser":{"dropCampaigns":[]}}}`, true},
		{"one campaign", `{"data":{"currentUser":{"dropCampaigns":[{"id":"x"}]}}}`, false},
		{"missing field", `{"data":{"currentUser":{}}}`, true},
		{"garbage", `not json`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := gqlCampaignsEmpty([]byte(c.body)); got != c.want {
				t.Errorf("gqlCampaignsEmpty(%q) = %v, want %v", c.body, got, c.want)
			}
		})
	}
}

func TestCleanCampaignName(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"Builder CapeSat, May 30, 3:30 PM UTC - Mon, Jun 15, 6:59 AM UTC", "Builder Cape"},
		{"Builder Cape", "Builder Cape"},
		{"FooThu, Jan 1", "Foo"},
		{"", ""},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			if got := cleanCampaignName(c.in); got != c.want {
				t.Errorf("cleanCampaignName(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}
