package api

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
)

// downloadDeps serves the pre-built cookie helper binaries baked into the
// image (see deploy/Dockerfile.miner). Self-contained — no GitHub/CI needed.
type downloadDeps struct {
	dir string // where the cross-compiled helpers live (default /helpers)
}

// helperFiles maps the ?os= query value to the baked binary filename.
var helperFiles = map[string]string{
	"macos":       "grubdrops-helper-macos-arm64",
	"macos-intel": "grubdrops-helper-macos-intel",
	"windows":     "grubdrops-helper-windows.exe",
	"linux":       "grubdrops-helper-linux",
}

// helper streams a pre-built helper binary as an attachment.
func (d *downloadDeps) helper(w http.ResponseWriter, r *http.Request) {
	fn, ok := helperFiles[r.URL.Query().Get("os")]
	if !ok {
		http.Error(w, "unknown os — use one of: macos, macos-intel, windows, linux", http.StatusBadRequest)
		return
	}
	dir := d.dir
	if dir == "" {
		dir = "/helpers"
	}
	f, err := os.Open(filepath.Join(dir, fn))
	if err != nil {
		http.Error(w, "helper binary not available in this build", http.StatusNotFound)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename="+fn)
	_, _ = io.Copy(w, f)
}
