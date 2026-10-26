package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"
	"unicode/utf8"

	"github.com/alexedwards/scs/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	mlog "github.com/aalejandrofer/grubdrops/internal/log"
	"github.com/aalejandrofer/grubdrops/internal/store"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

// newHealthEnv builds an authenticated env (auth bypass) with the health
// routes mounted and Deps adjusted by mut.
func newHealthEnv(t *testing.T, mut func(*Deps)) *env {
	t.Helper()
	t.Setenv("GRUB_AUTHBYPASS", "true")
	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "t.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	sm := scs.New()
	sm.Store = NewKVSessionStore(db)
	sm.Lifetime = time.Hour
	d := Deps{
		DB: db, Q: gen.New(db), Session: sm, Version: "v1.0.0", Hub: NewHub(),
		Scans: store.NewAccountScans(gen.New(db)), Settings: store.NewSettings(gen.New(db)),
		SPA: fstest.MapFS{"index.html": {Data: []byte("spa")}},
	}
	if mut != nil {
		mut(&d)
	}
	s := newServer(d)
	s.routesHealth(s.protected)
	srv := httptest.NewServer(s.handler())
	t.Cleanup(srv.Close)
	jar, _ := cookiejar.New(nil)
	return &env{t: t, srv: srv, cl: &http.Client{Jar: jar}, s: s}
}

func testRing() *mlog.Ring {
	r := mlog.NewRing(10)
	r.PushEvent(mlog.KindClaim, "INFO", "claimed it", map[string]any{"account": "a1"})
	r.PushEvent(mlog.KindError, "", "boom", map[string]any{"account": "a1"})
	r.PushEvent(mlog.KindState, "INFO", "mining", nil)
	return r
}

func TestHealth_FilterAndCounts(t *testing.T) {
	e := newHealthEnv(t, func(d *Deps) {
		d.Ring = testRing()
		d.StartTime = time.Unix(1700000000, 0).UTC()
		d.Commit = "abc123"
		d.KickSidecars = func() []string { return []string{"sc1:50051"} }
	})
	_, err := e.s.d.DB.Exec(`INSERT INTO accounts (id, platform, display_name, status, created_at, updated_at) VALUES ('a1','kick','Alice','active',1,1)`)
	require.NoError(t, err)

	_, tok := e.session()
	resp, b := e.do("GET", "/api/health?filter=problems", tok, nil)
	require.Equal(t, 200, resp.StatusCode, string(b))
	var got struct {
		Miner  map[string]any    `json:"miner"`
		Kick   map[string]any    `json:"kick_sidecars"`
		Events []healthEvent     `json:"events"`
		Counts map[string]int    `json:"counts"`
		Canary []json.RawMessage `json:"canary"`
	}
	require.NoError(t, json.Unmarshal(b, &got))
	require.Len(t, got.Events, 1)
	assert.Equal(t, "boom", got.Events[0].Msg)
	assert.Equal(t, "a1", got.Events[0].AccountID)
	assert.Equal(t, "Alice", got.Events[0].AccountName)
	assert.Equal(t, map[string]int{"all": 3, "problems": 1, "claims": 1}, got.Counts)
	assert.Equal(t, "abc123", got.Miner["commit"])
	assert.Equal(t, "v1.0.0", got.Miner["version"])
	assert.Equal(t, []any{"sc1:50051"}, got.Kick["running"])
	assert.Empty(t, got.Canary)

	// all: newest first, counts unchanged.
	_, b = e.do("GET", "/api/health", tok, nil)
	require.NoError(t, json.Unmarshal(b, &got))
	require.Len(t, got.Events, 3)
	assert.Equal(t, "mining", got.Events[0].Msg)
	assert.Equal(t, "claimed it", got.Events[2].Msg)

	_, b = e.do("GET", "/api/health?filter=claims", tok, nil)
	require.NoError(t, json.Unmarshal(b, &got))
	require.Len(t, got.Events, 1)
	assert.Equal(t, "claimed it", got.Events[0].Msg)

	resp, _ = e.do("GET", "/api/health?filter=nope", tok, nil)
	assert.Equal(t, 400, resp.StatusCode)
}

func TestHealth_EventsCappedAt50(t *testing.T) {
	ring := mlog.NewRing(100)
	for i := 0; i < 80; i++ {
		ring.PushEvent(mlog.KindState, "INFO", "x", nil)
	}
	e := newHealthEnv(t, func(d *Deps) { d.Ring = ring })
	_, b := e.do("GET", "/api/health", "", nil)
	var got struct {
		Events []healthEvent  `json:"events"`
		Counts map[string]int `json:"counts"`
	}
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Len(t, got.Events, 50)
	assert.Equal(t, 80, got.Counts["all"])
}

func TestHealth_DebugHiddenFromEventsKeptInLog(t *testing.T) {
	ring := mlog.NewRing(10)
	ring.PushEvent(mlog.KindClaim, "INFO", "claimed it", nil)
	ring.PushEvent(mlog.KindError, "DEBUG", "debug noise", nil)
	ring.PushEvent(mlog.KindState, "INFO", "mining", nil)
	e := newHealthEnv(t, func(d *Deps) { d.Ring = ring })

	for _, f := range []string{"all", "problems", "claims"} {
		_, b := e.do("GET", "/api/health?filter="+f, "", nil)
		var got struct {
			Events []healthEvent  `json:"events"`
			Counts map[string]int `json:"counts"`
		}
		require.NoError(t, json.Unmarshal(b, &got))
		for _, ev := range got.Events {
			assert.NotEqual(t, "debug noise", ev.Msg, f)
		}
		assert.Equal(t, map[string]int{"all": 2, "problems": 0, "claims": 1}, got.Counts, f)
	}

	_, b := e.do("GET", "/api/health/log", "", nil)
	assert.Contains(t, string(b), "DEBUG [error] debug noise")
}

func TestHealth_LogDownload(t *testing.T) {
	e := newHealthEnv(t, func(d *Deps) { d.Ring = testRing() })
	resp, b := e.do("GET", "/api/health/log", "", nil)
	require.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, `attachment; filename="grubdrops-log.txt"`, resp.Header.Get("Content-Disposition"))
	assert.True(t, strings.HasPrefix(resp.Header.Get("Content-Type"), "text/plain"))
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	require.Len(t, lines, 3)
	assert.Contains(t, lines[0], "claimed it")
	assert.Contains(t, lines[0], "account=a1")
	assert.Contains(t, lines[1], "ERROR [error] boom")
}

func TestHealth_CanaryRuns202(t *testing.T) {
	ran := make(chan struct{})
	e := newHealthEnv(t, func(d *Deps) {
		d.RunCanary = func(ctx context.Context) error {
			_, hasDeadline := ctx.Deadline()
			assert.True(t, hasDeadline)
			close(ran)
			return nil
		}
	})
	_, tok := e.session()
	resp, _ := e.do("POST", "/api/health/canary", tok, nil)
	assert.Equal(t, 202, resp.StatusCode)
	select {
	case <-ran:
	case <-time.After(2 * time.Second):
		t.Fatal("canary did not run")
	}
}

func TestHealth_CanaryNoOverlap(t *testing.T) {
	var starts atomic.Int32
	release := make(chan struct{})
	started := make(chan struct{}, 2)
	e := newHealthEnv(t, func(d *Deps) {
		d.RunCanary = func(context.Context) error {
			starts.Add(1)
			started <- struct{}{}
			<-release
			return nil
		}
	})
	_, tok := e.session()
	resp, _ := e.do("POST", "/api/health/canary", tok, nil)
	assert.Equal(t, 202, resp.StatusCode)
	<-started
	resp, _ = e.do("POST", "/api/health/canary", tok, nil)
	assert.Equal(t, 202, resp.StatusCode)
	time.Sleep(100 * time.Millisecond)
	assert.EqualValues(t, 1, starts.Load())

	close(release)
	require.Eventually(t, func() bool { return !e.s.canaryRunning.Load() }, 2*time.Second, 10*time.Millisecond)
	e.do("POST", "/api/health/canary", tok, nil)
	require.Eventually(t, func() bool { return starts.Load() == 2 }, 2*time.Second, 10*time.Millisecond)
}

func TestLogEventJSON_MatchesHealthEvent(t *testing.T) {
	at := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	l := mlog.LogLine{TS: at, Level: "INFO", Kind: mlog.KindClaim, Msg: "claimed", Fields: map[string]any{"account": "a1"}}
	b, err := json.Marshal(LogEventJSON(l, "Alice"))
	require.NoError(t, err)
	assert.JSONEq(t, `{"at":"2026-10-25T12:00:00Z","level":"INFO","kind":"claim","account_id":"a1","account_name":"Alice","msg":"claimed"}`, string(b))
	assert.Equal(t, "", LogAccountID(mlog.LogLine{}))
}

func TestHealth_TimesUTCOrNull(t *testing.T) {
	plus1 := time.FixedZone("plus1", 3600)
	ring := mlog.NewRing(10)
	ring.PushEvent(mlog.KindState, "INFO", "x", nil)
	e := newHealthEnv(t, func(d *Deps) {
		d.Ring = ring
		d.StartTime = time.Date(2026, 10, 25, 13, 0, 0, 0, plus1)
	})
	// A stored canary result with no check time renders checked_at as null.
	require.NoError(t, e.s.d.Q.UpsertSettingString(context.Background(), gen.UpsertSettingStringParams{
		Key: "canary:kick", Value: []byte(`{"ok":true,"detail":"d"}`),
	}))
	_, b := e.do("GET", "/api/health", "", nil)
	var got struct {
		Miner      map[string]any   `json:"miner"`
		LastScanAt *string          `json:"last_scan_at"`
		Canary     []map[string]any `json:"canary"`
		Events     []map[string]any `json:"events"`
	}
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, "2026-10-25T12:00:00Z", got.Miner["started_at"])
	assert.Nil(t, got.LastScanAt)
	require.Len(t, got.Canary, 1)
	v, ok := got.Canary[0]["checked_at"]
	assert.True(t, ok)
	assert.Nil(t, v)
	require.Len(t, got.Events, 1)
	assert.True(t, strings.HasSuffix(got.Events[0]["at"].(string), "Z"), got.Events[0]["at"])
}

func TestHealth_CanaryRunEndsWithRootCtx(t *testing.T) {
	root, cancel := context.WithCancel(context.Background())
	defer cancel()
	ended := make(chan struct{})
	started := make(chan struct{})
	e := newHealthEnv(t, func(d *Deps) {
		d.RootCtx = root
		d.RunCanary = func(ctx context.Context) error {
			close(started)
			<-ctx.Done()
			close(ended)
			return ctx.Err()
		}
	})
	_, tok := e.session()
	resp, _ := e.do("POST", "/api/health/canary", tok, nil)
	require.Equal(t, 202, resp.StatusCode)
	<-started
	cancel()
	select {
	case <-ended:
	case <-time.After(2 * time.Second):
		t.Fatal("canary run outlived the root context")
	}
}

func TestHealth_EventFieldsAllowlisted(t *testing.T) {
	ring := mlog.NewRing(10)
	ring.PushEvent(mlog.KindClaim, "INFO", "runner claim recorded", map[string]any{
		"game": "Rust", "benefit_name": "Hazmat", "err": strings.Repeat("x", 300),
		"cookie": "secret", "account": "a1",
	})
	e := newHealthEnv(t, func(d *Deps) { d.Ring = ring })
	var body struct {
		Events []struct {
			Fields map[string]string `json:"fields"`
		} `json:"events"`
	}
	_, tok := e.session()
	resp, b := e.do("GET", "/api/health", tok, nil)
	require.Equal(t, 200, resp.StatusCode, string(b))
	require.NoError(t, json.Unmarshal(b, &body))
	require.Len(t, body.Events, 1)
	f := body.Events[0].Fields
	assert.Len(t, f, 3)
	assert.Equal(t, "Rust", f["game"])
	assert.Equal(t, "Hazmat", f["benefit_name"])
	assert.Len(t, []rune(f["err"]), 200)
	assert.NotContains(t, f, "cookie")
	assert.NotContains(t, f, "account")
}

func TestLogEventJSON_AppliesAllowlist(t *testing.T) {
	l := mlog.LogLine{
		TS: time.Now(), Level: "INFO", Kind: mlog.KindClaim, Msg: "m",
		Fields: map[string]any{"game": "Rust", "cookie": "secret", "account": "a1", "err": strings.Repeat("é", 300)},
	}
	b, err := json.Marshal(LogEventJSON(l, "Alice"))
	require.NoError(t, err)
	var got struct {
		Fields map[string]string `json:"fields"`
	}
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Len(t, got.Fields, 2)
	assert.Equal(t, "Rust", got.Fields["game"])
	assert.NotContains(t, string(b), "secret")
	assert.True(t, utf8.ValidString(got.Fields["err"]))
	assert.Equal(t, 200, utf8.RuneCountInString(got.Fields["err"]))
	assert.Equal(t, strings.Repeat("é", 200), got.Fields["err"])
}

func TestHealth_CanarySkipped(t *testing.T) {
	e := newHealthEnv(t, nil)
	require.NoError(t, e.s.d.Q.UpsertSettingString(context.Background(), gen.UpsertSettingStringParams{
		Key: "canary:twitch", Value: []byte(`{"ok":false,"skipped":true,"detail":"","checked_at":"2026-10-25T12:00:00Z"}`),
	}))
	_, b := e.do("GET", "/api/health", "", nil)
	var got struct {
		Canary []map[string]any `json:"canary"`
	}
	require.NoError(t, json.Unmarshal(b, &got))
	require.Len(t, got.Canary, 1)
	assert.Equal(t, true, got.Canary[0]["skipped"])
	assert.Equal(t, "twitch", got.Canary[0]["platform"])
}
