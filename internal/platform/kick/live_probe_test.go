//go:build live

package kick

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// TestLive_ProgressRaw dumps raw /drops/progress for a Kick account that has
// at least one CLAIMED reward, to learn whether claimed rewards stay listed.
//
//	GRUB_LIVE_KICK_SESSION_JSON='{"cookies":{...}}' \
//	  go test -tags live -run TestLive_ProgressRaw -v ./internal/platform/kick/
func TestLive_ProgressRaw(t *testing.T) {
	js := os.Getenv("GRUB_LIVE_KICK_SESSION_JSON")
	if js == "" {
		t.Skip("set GRUB_LIVE_KICK_SESSION_JSON")
	}
	var s platform.Session
	require.NoError(t, json.Unmarshal([]byte(js), &s))
	b := New(nil, nil, "grubdrops-browser-{slug}", 9090, 10*time.Minute)
	body, status, err := b.api.d.do(context.Background(), s, http.MethodGet, dropsBase+"/api/v1/drops/progress", nil)
	require.NoError(t, err)
	t.Logf("status=%d body=%s", status, body)
}
