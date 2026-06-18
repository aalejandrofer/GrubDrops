package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHealthz(t *testing.T) {
	h := NewRouter(Deps{})
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "ok\n", rec.Body.String())
}

func TestApplyRedirectTarget(t *testing.T) {
	cases := []struct {
		name, referer, want string
	}{
		{"empty referer", "", "/"},
		{"dashboard referer", "https://miner.example.com/", "/"},
		{"accounts list referer", "https://miner.example.com/accounts", "/accounts"},
		{"accounts detail referer", "https://miner.example.com/accounts/abc", "/accounts/abc"},
		{"with query string", "https://miner.example.com/accounts?filter=on", "/accounts?filter=on"},
		{"settings referer", "https://miner.example.com/settings", "/settings"},
		{"unparseable referer", "::not a url::", "/"},
		{"protocol-relative open redirect", "https://miner.example.com//evil.com/path", "/"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/accounts/apply", nil)
			if tc.referer != "" {
				req.Header.Set("Referer", tc.referer)
			}
			assert.Equal(t, tc.want, applyRedirectTarget(req))
		})
	}
}
