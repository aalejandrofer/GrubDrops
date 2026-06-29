package update

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

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
