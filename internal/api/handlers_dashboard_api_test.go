package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAPIDashboardReturnsJSON verifies that apiPage serialises the
// dashPage snapshot to JSON with the correct status and content-type.
// collectPage calls d.q.ListAllAccounts unconditionally, so a zero-value
// dashboardDeps{} would panic on nil d.q. We construct the minimal
// non-nil deps (migrated in-memory DB via newTestSettings) rather than
// papering over the nil-pointer with a panic recovery.
func TestAPIDashboardReturnsJSON(t *testing.T) {
	_, q := newTestSettings(t) // migrated sqlite-backed *gen.Queries
	d := dashboardDeps{q: q}
	req := httptest.NewRequest(http.MethodGet, "/api/dashboard", nil)
	rec := httptest.NewRecorder()
	d.apiPage(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "application/json")
	// Body is a JSON object with the dashPage top-level keys.
	body := rec.Body.String()
	assert.True(t, strings.HasPrefix(strings.TrimSpace(body), "{"))
	assert.Contains(t, body, `"Tele"`)
	assert.Contains(t, body, `"Mining"`)
}

func TestSPAIndexServesHTML(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	spaIndex(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "text/html")
	assert.Contains(t, rec.Body.String(), `id="app"`)
}

func TestSPAFileServerServesIndex(t *testing.T) {
	h := spaFileServer()
	req := httptest.NewRequest(http.MethodGet, "/index.html", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.True(t, strings.Contains(rec.Body.String(), "<html"))
}
