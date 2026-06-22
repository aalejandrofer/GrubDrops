package web

import (
	"embed"
	"io/fs"
)

//go:embed spa/dist
var spaFS embed.FS

// SPA returns an fs.FS rooted at the embedded SPA build output
// (internal/web/spa/dist). Mirrors Static(): callers serve it with
// http.FileServer(http.FS(web.SPA())). The dist directory always
// contains at least a placeholder index.html (committed) so this
// compiles even before `vite build` has run in a fresh checkout.
func SPA() fs.FS {
	sub, err := fs.Sub(spaFS, "spa/dist")
	if err != nil {
		panic(err)
	}
	return sub
}
