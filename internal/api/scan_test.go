package api

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newScanEnv is a gamesEnv with POST /api/scan and a clock it returns.
func newScanEnv(t *testing.T) (*gamesEnv, *time.Time) {
	t.Helper()
	g := newGamesEnv(t)
	now := time.Unix(1_000_000, 0)
	g.s.d.Now = func() time.Time { return now }
	g.s.routesScan(g.s.protected)
	return g, &now
}

func TestPostScan_AllAndPlatform(t *testing.T) {
	g, now := newScanEnv(t)

	resp, _ := g.do("POST", "/api/scan", g.csrf, nil)
	assert.Equal(t, 202, resp.StatusCode)
	assert.Equal(t, 1, g.rescans)
	assert.Empty(t, g.plats)

	*now = now.Add(scanCooldown)
	resp, _ = g.do("POST", "/api/scan", g.csrf, map[string]any{"platform": "kick"})
	assert.Equal(t, 202, resp.StatusCode)
	assert.Equal(t, 1, g.rescans, "a platform scan leaves the other platform alone")
	assert.Equal(t, []string{"kick"}, g.plats)

	*now = now.Add(scanCooldown)
	resp, _ = g.do("POST", "/api/scan", g.csrf, map[string]any{"platform": ""})
	assert.Equal(t, 202, resp.StatusCode)
	assert.Equal(t, 2, g.rescans, "an empty platform scans every account")

	*now = now.Add(scanCooldown)
	resp, _ = g.do("POST", "/api/scan", g.csrf, map[string]any{"platform": "twitch"})
	assert.Equal(t, 202, resp.StatusCode)
	assert.Equal(t, []string{"kick", "twitch"}, g.plats)
}

func TestPostScan_BadPlatform(t *testing.T) {
	g, _ := newScanEnv(t)
	for _, body := range []any{
		map[string]any{"platform": "youtube"},
		map[string]any{"platform": "Twitch"},
		map[string]any{"platform": 1},
		map[string]any{"other": "x"},
	} {
		resp, _ := g.do("POST", "/api/scan", g.csrf, body)
		assert.Equal(t, 400, resp.StatusCode, "%v", body)
	}
	assert.Zero(t, g.rescans)
	assert.Empty(t, g.plats)

	// A refused request does not start the cooldown.
	resp, _ := g.do("POST", "/api/scan", g.csrf, map[string]any{"platform": "twitch"})
	assert.Equal(t, 202, resp.StatusCode)
}

func TestPostScan_Cooldown(t *testing.T) {
	g, now := newScanEnv(t)
	resp, _ := g.do("POST", "/api/scan", g.csrf, map[string]any{"platform": "twitch"})
	require.Equal(t, 202, resp.StatusCode)

	// One cooldown for every platform.
	*now = now.Add(20*time.Second + 500*time.Millisecond)
	resp, b := g.do("POST", "/api/scan", g.csrf, map[string]any{"platform": "kick"})
	assert.Equal(t, 429, resp.StatusCode)
	assert.Equal(t, "40", resp.Header.Get("Retry-After"))
	assert.JSONEq(t, `{"error":"cooldown","retry_after_sec":40}`, string(b))

	resp, _ = g.do("POST", "/api/scan", g.csrf, nil)
	assert.Equal(t, 429, resp.StatusCode)
	assert.Equal(t, []string{"twitch"}, g.plats)
	assert.Zero(t, g.rescans)

	*now = now.Add(40 * time.Second)
	resp, _ = g.do("POST", "/api/scan", g.csrf, nil)
	assert.Equal(t, 202, resp.StatusCode)
	assert.Equal(t, 1, g.rescans)
}

func TestPostScan_CSRF(t *testing.T) {
	g, _ := newScanEnv(t)
	resp, _ := g.do("POST", "/api/scan", "", nil)
	assert.Equal(t, 403, resp.StatusCode)
	resp, _ = g.do("POST", "/api/scan", "bogus", map[string]any{"platform": "kick"})
	assert.Equal(t, 403, resp.StatusCode)
	assert.Zero(t, g.rescans)
	assert.Empty(t, g.plats)

	// The refused calls left the cooldown untouched.
	resp, _ = g.do("POST", "/api/scan", g.csrf, nil)
	assert.Equal(t, 202, resp.StatusCode)
}

func TestPostScan_NoPool(t *testing.T) {
	g, _ := newScanEnv(t)
	g.s.d.Pool = nil
	resp, _ := g.do("POST", "/api/scan", g.csrf, nil)
	assert.Equal(t, 503, resp.StatusCode)
}
