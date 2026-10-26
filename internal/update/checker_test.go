package update

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/store"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

func newTestSettings(t *testing.T) *store.Settings {
	t.Helper()
	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "u.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return store.NewSettings(gen.New(db))
}

func TestRunOnce_CachesTagAndStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Contains(t, r.URL.Path, "/releases/latest")
		_, _ = w.Write([]byte(`{"tag_name":"v1.3.5","name":"v1.3.5"}`))
	}))
	t.Cleanup(srv.Close)

	c := NewChecker(srv.Client(), "owner/repo", newTestSettings(t))
	c.apiBase = srv.URL // test seam: override the GitHub base

	require.NoError(t, c.RunOnce(context.Background()))

	avail, latest := c.Status("v1.3.4")
	require.True(t, avail)
	require.Equal(t, "v1.3.5", latest)

	avail, _ = c.Status("v1.3.5")
	require.False(t, avail, "equal version is not an update")
}

func TestRunOnce_BadResponseKeepsPriorCache(t *testing.T) {
	c := NewChecker(http.DefaultClient, "owner/repo", newTestSettings(t))
	// Seed a known-good cached value.
	c.setLatest("v1.3.5")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	c.httpClient = srv.Client()
	c.apiBase = srv.URL

	require.Error(t, c.RunOnce(context.Background()))
	_, latest := c.Status("v1.3.4")
	require.Equal(t, "v1.3.5", latest, "prior cache retained on error")
}

func TestChecker_StoresNotes(t *testing.T) {
	body := "Highlights\n\n- **One** first\n* Two second\n- Three third\n\nA closing paragraph."
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"tag_name": "v2.0.0", "body": body, "html_url": "https://github.com/o/r/releases/tag/v2.0.0",
		})
	}))
	t.Cleanup(srv.Close)

	s := newTestSettings(t)
	c := NewChecker(srv.Client(), "owner/repo", s)
	c.apiBase = srv.URL
	require.NoError(t, c.RunOnce(context.Background()))

	latest, url, notes, at := c.Release()
	require.Equal(t, "v2.0.0", latest)
	require.Equal(t, "https://github.com/o/r/releases/tag/v2.0.0", url)
	require.Equal(t, []string{"**One** first", "Two second", "Three third"}, notes)
	require.False(t, at.IsZero())

	// A restart reloads the same answer from the store.
	c2 := NewChecker(srv.Client(), "owner/repo", s)
	latest, url, notes, at = c2.Release()
	require.Equal(t, "v2.0.0", latest)
	require.NotEmpty(t, url)
	require.Len(t, notes, 3)
	require.False(t, at.IsZero())
}

func TestChecker_AutoOffSkips(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte(`{"tag_name":"v2.0.0"}`))
	}))
	t.Cleanup(srv.Close)

	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "u.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	q := gen.New(db)
	require.NoError(t, q.UpsertSettingString(context.Background(), gen.UpsertSettingStringParams{
		Key: "settings:update_auto", Value: []byte("0"),
	}))
	c := NewChecker(srv.Client(), "owner/repo", store.NewSettings(q))
	c.apiBase = srv.URL

	// Run checks immediately, then blocks until the context ends.
	run := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		defer cancel()
		c.Run(ctx, time.Hour)
	}
	run()
	require.Zero(t, hits.Load(), "auto off: no network call")

	require.NoError(t, q.UpsertSettingString(context.Background(), gen.UpsertSettingStringParams{
		Key: "settings:update_auto", Value: []byte("1"),
	}))
	run()
	require.EqualValues(t, 1, hits.Load())
}
