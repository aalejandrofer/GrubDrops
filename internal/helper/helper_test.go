package helper

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// newTestClient builds a minerClient pointed at srv with cookie jar
// enabled — matches the production wiring closely enough for submit().
func newTestClient(t *testing.T, srv *httptest.Server) *minerClient {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	base, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	return &minerClient{
		http: &http.Client{Jar: jar},
		base: base,
	}
}

// csrfPageBody is the minimum body shape submit() needs to extract a
// CSRF token via extractCSRF.
const csrfPageBody = `<form><input name="csrf_token" value="tok-123"></form>`

func TestSubmitTreats303AsSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(csrfPageBody))
			return
		}
		// Real success: 303 with Location header.
		w.Header().Set("Location", "/done")
		w.WriteHeader(http.StatusSeeOther)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	if err := c.submit(context.Background(), "/path", url.Values{}); err != nil {
		t.Fatalf("submit returned error: %v", err)
	}
}

func TestSubmitTreats200WithoutMarkerAsSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(csrfPageBody))
			return
		}
		_, _ = w.Write([]byte("<html><body>ok page</body></html>"))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	if err := c.submit(context.Background(), "/path", url.Values{}); err != nil {
		t.Fatalf("submit returned error: %v", err)
	}
}

func TestSubmit200WithErrorMarkerReturnsError(t *testing.T) {
	for _, marker := range []string{
		"sidecar rejected",
		"failed to persist",
		"wrong password",
		"CSRF token invalid",
	} {
		marker := marker
		t.Run(marker, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					_, _ = w.Write([]byte(csrfPageBody))
					return
				}
				// Server re-rendered the form with an error flash.
				_, _ = w.Write([]byte("<div class='err'>" + marker + "</div>" + csrfPageBody))
			}))
			defer srv.Close()

			c := newTestClient(t, srv)
			err := c.submit(context.Background(), "/path", url.Values{})
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", marker)
			}
			if !strings.Contains(err.Error(), marker) {
				t.Fatalf("expected error to mention marker %q, got %v", marker, err)
			}
		})
	}
}

func TestSubmitNon200Non303ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(csrfPageBody))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	err := c.submit(context.Background(), "/path", url.Values{})
	if err == nil {
		t.Fatalf("expected error on 500 response, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Fatalf("expected error to mention status, got %v", err)
	}
}
