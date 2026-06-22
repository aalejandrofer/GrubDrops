package web

import (
	"io/fs"
	"testing"
)

func TestSPAEmbedsIndex(t *testing.T) {
	f, err := SPA().Open("index.html")
	if err != nil {
		t.Fatalf("SPA() must embed index.html: %v", err)
	}
	defer f.Close()
}

func TestSPAIsFS(t *testing.T) {
	var _ fs.FS = SPA()
}
