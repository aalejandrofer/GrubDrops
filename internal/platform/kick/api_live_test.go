package kick

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"
)

// TestLiveDiscovery hits the REAL Kick public livestreams endpoint through the
// production utls transport to prove the Chrome-fingerprint client bypasses
// Cloudflare and the discovery parsing works. Gated by KICK_COOKIES (a JSON
// array of {name,value}); skipped otherwise so CI stays offline.
//
//	KICK_COOKIES=/tmp/dropsminer-kick-cookies.json go test ./internal/platform/kick -run TestLiveDiscovery -v
func TestLiveDiscovery(t *testing.T) {
	path := os.Getenv("KICK_COOKIES")
	if path == "" {
		t.Skip("set KICK_COOKIES to a cookies json to run the live discovery test")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read cookies: %v", err)
	}
	var flat []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	}
	if err := json.Unmarshal(raw, &flat); err != nil {
		t.Fatalf("parse cookies: %v", err)
	}
	ks := kickSession{UserAgent: chromeUA}
	for _, c := range flat {
		ks.Cookies = append(ks.Cookies, cookie{Name: c.Name, Value: c.Value, Domain: ".kick.com", Path: "/"})
		if c.Name == "XSRF-TOKEN" {
			ks.XSRFToken = c.Value
		}
	}
	sess, err := encodeSession(ks)
	if err != nil {
		t.Fatalf("encode session: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	chans, err := newAPI().DiscoverChannelsForCategory(ctx, sess, "rust")
	if err != nil {
		t.Fatalf("discover channels: %v", err)
	}
	t.Logf("discovered %d live rust channels via utls (CF bypassed)", len(chans))
	if len(chans) == 0 {
		t.Fatal("expected at least one live channel")
	}
	for i, c := range chans {
		if i >= 5 {
			break
		}
		t.Logf("  %s  viewers=%d livestreamID=%s", c.Channel, c.ViewerCount, c.ChannelID)
	}
}
