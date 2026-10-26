package main

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/api"
	mlog "github.com/aalejandrofer/grubdrops/internal/log"
	"github.com/aalejandrofer/grubdrops/internal/store"
)

// TestRun_BootsAndServesAPI boots the whole miner against a temp DB, checks
// the JSON API and the SPA or its not-built page answer, then cancels and expects a
// clean shutdown inside 10 s.
func TestRun_BootsAndServesAPI(t *testing.T) {
	prevZone := setProcessZone
	setProcessZone = func(*time.Location) {}
	t.Cleanup(func() { setProcessZone = prevZone })

	key, err := generateMasterKey()
	require.NoError(t, err)

	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := l.Addr().String()
	require.NoError(t, l.Close())

	t.Setenv("GRUB_MASTER_KEY", key)
	t.Setenv("GRUB_DB_PATH", filepath.Join(t.TempDir(), "miner.db"))
	t.Setenv("GRUB_HTTP_ADDR", addr)
	t.Setenv("GRUB_UPDATE_CHECK", "0")
	t.Setenv("GRUB_SECURE_COOKIES", "0")
	t.Setenv("GRUB_BROWSER_URL", "")
	t.Setenv("GRUB_BROWSER_URLS", "")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- run(ctx) }()

	base := "http://" + addr
	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	client := &http.Client{Timeout: 5 * time.Second, Jar: jar}

	var resp *http.Response
	require.Eventually(t, func() bool {
		r, err := client.Get(base + "/api/session")
		if err != nil {
			return false
		}
		resp = r
		return true
	}, 10*time.Second, 50*time.Millisecond, "miner did not start listening")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Content-Type"), "application/json")
	var sess map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&sess))
	assert.Equal(t, true, sess["setup_needed"])
	token, _ := sess["csrf_token"].(string)
	require.NotEmpty(t, token)

	// Create the admin so the SSE stream below is a real, held-open stream.
	req, err := http.NewRequest(http.MethodPost, base+"/api/setup", strings.NewReader(`{"password":"hunter2hunter2","confirm":"hunter2hunter2"}`))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", token)
	setup, err := client.Do(req)
	require.NoError(t, err)
	_ = setup.Body.Close()
	require.Equal(t, http.StatusNoContent, setup.StatusCode)

	// An open SSE stream must not hold shutdown up.
	sseClient := &http.Client{Jar: jar}
	sse, err := sseClient.Get(base + "/api/events")
	require.NoError(t, err)
	defer sse.Body.Close()
	require.Equal(t, http.StatusOK, sse.StatusCode)
	require.Contains(t, sse.Header.Get("Content-Type"), "text/event-stream")

	page, err := client.Get(base + "/")
	require.NoError(t, err)
	body, err := io.ReadAll(page.Body)
	_ = page.Body.Close()
	require.NoError(t, err)
	// 200 with a built SPA, 503 with the "UI not built" page.
	assert.Contains(t, []int{http.StatusOK, http.StatusServiceUnavailable}, page.StatusCode)
	assert.True(t, strings.Contains(string(body), "GrubDrops"), "SPA or not-built page not served")

	start := time.Now()
	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("run did not shut down within 10 s")
	}
	// srv.Shutdown waits up to 5 s for open requests; the SSE stream must
	// end with the root ctx well before that.
	assert.Less(t, time.Since(start), 4*time.Second, "SSE stream held shutdown")
}

// TestGenerateMasterKey_IsAcceptedByCryptor proves the `keygen` subcommand
// emits a key the store actually accepts — guarding against the old README
// guidance (head -c32 /dev/urandom | base64), which produced a blob that
// failed age.ParseX25519Identity and crashed the miner at startup.
func TestGenerateMasterKey_IsAcceptedByCryptor(t *testing.T) {
	key, err := generateMasterKey()
	require.NoError(t, err)
	require.NotEmpty(t, key)
	assert.Contains(t, key, "AGE-SECRET-KEY-1", "must be an age X25519 identity")

	// The real gate: store.NewCryptor parses it without error.
	_, err = store.NewCryptor(key)
	require.NoError(t, err, "generated key must be accepted by the session store")

	// Two calls yield distinct keys.
	key2, err := generateMasterKey()
	require.NoError(t, err)
	assert.NotEqual(t, key, key2, "each keygen must be unique")
}

func TestLogHook_NamesFromCacheOnly(t *testing.T) {
	hub := api.NewHub()
	sub, unsub := hub.Subscribe()
	defer unsub()
	var asked []string
	hook := logHook(hub, func(id string) string {
		asked = append(asked, id)
		return "Alice"
	})
	hook(mlog.LogLine{Msg: "x", Fields: map[string]any{"account": "a1"}})
	hook(mlog.LogLine{Msg: "y"})

	for _, want := range []string{"Alice", ""} {
		ev := <-sub
		assert.Equal(t, api.EventLog, ev.Name)
		b, err := json.Marshal(ev.Data)
		require.NoError(t, err)
		var got struct {
			AccountName string `json:"account_name"`
		}
		require.NoError(t, json.Unmarshal(b, &got))
		assert.Equal(t, want, got.AccountName)
	}
	assert.Equal(t, []string{"a1"}, asked, "lookup only for account lines")
}
