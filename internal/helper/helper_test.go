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
// enabled — matches the production wiring closely enough for postForm().
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

func TestPostForm200IsSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("want POST, got %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	if err := c.postForm(context.Background(), "/helper/accounts/acc_x/kick", url.Values{}); err != nil {
		t.Fatalf("postForm returned error: %v", err)
	}
}

func TestPostForm404IsFriendlyAccountError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	err := c.postForm(context.Background(), "/helper/accounts/acc_bad/kick", url.Values{})
	if err == nil {
		t.Fatal("expected an error on 404, got nil")
	}
	if !strings.Contains(err.Error(), "Account ID") {
		t.Fatalf("404 error should mention the Account ID, got %v", err)
	}
}

func TestPostFormNon2xxReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	err := c.postForm(context.Background(), "/helper/accounts/acc_x/kick", url.Values{})
	if err == nil {
		t.Fatal("expected error on 500, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Fatalf("expected error to mention status, got %v", err)
	}
}
