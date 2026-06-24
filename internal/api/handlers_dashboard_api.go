package api

import (
	"io"
	"net/http"

	"github.com/aalejandrofer/grubdrops/internal/web"
)

// spaSecureCookies mirrors Deps.SecureCookies for the SPA's readable CSRF
// cookie (spaIndex is a free function with no access to Deps). Set once in
// NewRouter.
var spaSecureCookies bool

// apiPage serves the dashboard snapshot as JSON for the SPA. It reuses
// the same collectPage projection the html/template dashboard renders,
// so the SPA and the legacy page show identical data. JSON keys are the
// exported Go field names of dashPage (PascalCase).
func (d dashboardDeps) apiPage(w http.ResponseWriter, r *http.Request) {
	page := d.collectPage(r)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, page)
}

// spaFileServer serves the embedded SPA build output (JS/CSS under
// /assets, plus index.html). Mounted at /assets/* in the router. CSS/JS
// filenames are content-hashed by Vite, so they cache aggressively.
// The canonical index.html redirect (301 → /) is suppressed so the file
// is served directly when requested by path.
func spaFileServer() http.Handler {
	fs := http.FileServer(http.FS(web.SPA()))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// http.FileServer redirects /index.html → / by default.
		// Suppress that so /index.html is always served as a file.
		if r.URL.Path == "/index.html" {
			spaIndex(w, r)
			return
		}
		fs.ServeHTTP(w, r)
	})
}

// spaIndex writes the SPA shell (index.html). The client-side router
// then renders the requested view. Used for routes opted into the SPA.
func spaIndex(w http.ResponseWriter, r *http.Request) {
	// Hand the SPA a readable CSRF token so its fetch writes can echo it in
	// the X-CSRF-Token header. Not HttpOnly (JS must read it); nosurf still
	// verifies the masked token against the session, so readability here does
	// not weaken CSRF.
	http.SetCookie(w, &http.Cookie{
		Name:     "csrftoken",
		Value:    csrfToken(r),
		Path:     "/",
		HttpOnly: false,
		Secure:   spaSecureCookies,
		SameSite: http.SameSiteLaxMode,
	})
	f, err := web.SPA().Open("index.html")
	if err != nil {
		http.Error(w, "spa index missing", http.StatusInternalServerError)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = io.Copy(w, f)
}
