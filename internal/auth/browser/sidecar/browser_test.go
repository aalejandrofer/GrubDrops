//go:build chromedp_smoke

package sidecar

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestBrowser_OpenTab requires a real Chrome binary on PATH. It's
// guarded behind a build tag so unit-test runs skip it. Run manually
// with: go test -tags=chromedp_smoke ./internal/auth/browser/sidecar/...
func TestBrowser_OpenTab(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	b := New(ctx)
	defer b.Close()

	h, _, err := b.OpenTab()
	require.NoError(t, err)
	require.NotEmpty(t, h)
	b.CloseTab(h)
}
