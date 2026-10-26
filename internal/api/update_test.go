package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/update"
)

// rewriteHost sends every request to a local http server.
type rewriteHost struct{ to string }

func (r rewriteHost) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.URL.Scheme, req.URL.Host = "http", r.to
	return http.DefaultTransport.RoundTrip(req)
}

func TestUpdate_ShapeAndCheck(t *testing.T) {
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"tag_name": "v1.1.0", "body": "- fast\n- fixed", "html_url": "https://example.test/rel",
		})
	}))
	t.Cleanup(gh.Close)

	e := newHealthEnv(t, func(d *Deps) {
		// Route the checker's api.github.com calls to the fake server.
		client := &http.Client{Transport: rewriteHost{to: gh.Listener.Addr().String()}}
		d.Update = update.NewChecker(client, "o/r", d.Settings)
	})
	_, tok := e.session()

	var got map[string]any
	resp, b := e.do("GET", "/api/update", tok, nil)
	require.Equal(t, 200, resp.StatusCode)
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, "v1.0.0", got["current"])
	assert.Equal(t, "", got["latest"])
	assert.Equal(t, false, got["update_available"])
	assert.Nil(t, got["checked_at"])
	assert.Equal(t, []any{}, got["notes"])
	assert.Equal(t, "docker compose pull && docker compose up -d", got["command"])
	assert.Equal(t, true, got["auto"])

	resp, b = e.do("POST", "/api/update/check", tok, nil)
	require.Equal(t, 200, resp.StatusCode)
	got = nil
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, "v1.1.0", got["latest"])
	assert.Equal(t, true, got["update_available"])
	assert.Equal(t, []any{"fast", "fixed"}, got["notes"])
	assert.Equal(t, "https://example.test/rel", got["notes_url"])
	require.IsType(t, "", got["checked_at"])
	assert.True(t, strings.HasSuffix(got["checked_at"].(string), "Z"), got["checked_at"])
}
