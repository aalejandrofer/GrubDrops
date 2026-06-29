package api

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aalejandrofer/grubdrops/internal/web"
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

func renderNav(t *testing.T, data templateData) string {
	t.Helper()
	tmpl, err := web.Templates()
	if err != nil {
		t.Fatalf("load templates: %v", err)
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "nav", data); err != nil {
		t.Fatalf("render nav: %v", err)
	}
	return buf.String()
}

func TestNav_UpdateBadgeShownWhenAvailable(t *testing.T) {
	out := renderNav(t, templateData{AuthedAdmin: true, UpdateAvailable: true, LatestRelease: "v1.3.5"})
	if !strings.Contains(out, "update-pill") {
		t.Errorf("update pill missing when UpdateAvailable")
	}
	if !strings.Contains(out, "/releases/latest") {
		t.Errorf("pill must link to releases/latest")
	}
	if !strings.Contains(out, "v1.3.5") {
		t.Errorf("pill/title must show the latest version")
	}
	if !strings.Contains(out, "pulse update") {
		t.Errorf("pulse dot must get the .update class (orange)")
	}
}

func TestNav_NoBadgeWhenUpToDate(t *testing.T) {
	out := renderNav(t, templateData{AuthedAdmin: true, UpdateAvailable: false})
	if strings.Contains(out, "update-pill") {
		t.Errorf("no pill when up to date")
	}
	if strings.Contains(out, "pulse update") {
		t.Errorf("pulse must stay plain (green) when up to date")
	}
}
