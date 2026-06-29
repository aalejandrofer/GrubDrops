package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestUpdateBadgeMiddleware_InjectsContext proves the middleware stores the
// status from the closure into request context for render() to read.
func TestUpdateBadgeMiddleware_InjectsContext(t *testing.T) {
	status := func(current string) (bool, string) {
		if current != "v1.3.4" {
			t.Fatalf("got current %q", current)
		}
		return true, "v1.3.5"
	}
	var gotAvail bool
	var gotLatest string
	h := updateBadge(status, "v1.3.4")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAvail, gotLatest = updateInfoFromContext(r.Context())
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	if !gotAvail || gotLatest != "v1.3.5" {
		t.Fatalf("ctx not injected: avail=%v latest=%q", gotAvail, gotLatest)
	}
}

// TestUpdateBadgeMiddleware_NilStatusNoPanic proves a nil status (checker
// disabled) is a safe passthrough.
func TestUpdateBadgeMiddleware_NilStatusNoPanic(t *testing.T) {
	called := false
	h := updateBadge(nil, "v1.3.4")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if avail, _ := updateInfoFromContext(r.Context()); avail {
			t.Fatal("nil status must not report an update")
		}
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil).WithContext(context.Background()))
	if !called {
		t.Fatal("next handler not called")
	}
}
