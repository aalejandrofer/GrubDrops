package sidecar

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/chromedp/chromedp"
)

// Browser wraps a chromedp allocator + tab manager. One Browser per
// sidecar process. Tabs are tracked by an opaque string handle so the
// gRPC layer can target them across requests.
type Browser struct {
	allocCtx    context.Context
	allocCancel context.CancelFunc

	// Stored to spawn isolated allocators for incognito-style work.
	baseCtx  context.Context
	baseOpts []chromedp.ExecAllocatorOption

	mu   sync.Mutex
	tabs map[string]tabState
	next int
}

type tabState struct {
	ctx    context.Context
	cancel context.CancelFunc
}

// stealthUA is a realistic, recent Chrome on Win10 desktop UA. Used
// across both the launch flags and the per-tab Network.SetUserAgent
// override below.
const stealthUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.0.0 Safari/537.36"

// New launches a headless Chrome via the system path. In the sidecar
// container the binary lives at /headless-shell/headless-shell.
//
// Flags chosen to defeat PerimeterX / kasada-style anti-bot fingerprint
// checks (the kind Twitch deploys via k.twitchcdn.net/p.js). The
// default chromedp headless mode leaks navigator.webdriver=true and
// other tells; --disable-blink-features=AutomationControlled removes
// the worst offender, and a per-tab JS override patches the rest.
func New(ctx context.Context) *Browser {
	// Build allocator options from scratch (NOT chromedp.DefaultExecAllocatorOptions)
	// because the default set includes --enable-automation which is the
	// master switch PerimeterX/Kasada look at: it sets navigator.webdriver
	// AND adds "AutomationControlled" to enabled blink features which
	// fingerprinting JS detects independently of the JS-level shim we
	// inject. Omit it entirely.
	opts := []chromedp.ExecAllocatorOption{
		chromedp.NoFirstRun,
		chromedp.NoDefaultBrowserCheck,
		// Use the new headless mode — closer to real Chrome's renderer.
		chromedp.Flag("headless", "new"),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-dev-shm-usage", true),

		// Anti-detection
		chromedp.Flag("disable-blink-features", "AutomationControlled"),

		// Puppeteer-style defaults minus enable-automation
		chromedp.Flag("disable-background-networking", true),
		chromedp.Flag("disable-background-timer-throttling", true),
		chromedp.Flag("disable-backgrounding-occluded-windows", true),
		chromedp.Flag("disable-breakpad", true),
		chromedp.Flag("disable-client-side-phishing-detection", true),
		chromedp.Flag("disable-default-apps", true),
		chromedp.Flag("disable-extensions", true),
		chromedp.Flag("disable-hang-monitor", true),
		chromedp.Flag("disable-ipc-flooding-protection", true),
		chromedp.Flag("disable-popup-blocking", true),
		chromedp.Flag("disable-prompt-on-repost", true),
		chromedp.Flag("disable-renderer-backgrounding", true),
		chromedp.Flag("disable-sync", true),
		chromedp.Flag("force-color-profile", "srgb"),
		chromedp.Flag("metrics-recording-only", true),
		chromedp.Flag("safebrowsing-disable-auto-update", true),
		chromedp.Flag("password-store", "basic"),
		chromedp.Flag("use-mock-keychain", true),
		chromedp.Flag("enable-features", "NetworkService,NetworkServiceInProcess"),

		// Narrower than the default which disabled site-per-process unilaterally.
		// Keep IsolateOrigins off so cross-origin frames don't get separate processes (cheaper),
		// but allow Translate/BlinkGenPropertyTrees (default behaviour) since disabling
		// those is detectable.
		chromedp.Flag("disable-features", "IsolateOrigins,site-per-process"),

		chromedp.Flag("lang", "en-US,en;q=0.9"),
		chromedp.UserAgent(stealthUA),

		// Video playback for the Kick IVS watch path. Without an explicit
		// autoplay grant, Chrome's autoplay policy blocks <video>.play() in a
		// headless tab that has no user gesture, so the player never advances
		// and Kick credits no watch-time. Muting the audio both satisfies the
		// "muted autoplay is always allowed" rule and keeps the tab cheap.
		chromedp.Flag("autoplay-policy", "no-user-gesture-required"),
		chromedp.Flag("mute-audio", true),
	}

	// In the container the runtime image sets CHROME_BIN to the
	// google-chrome-stable path (the codec-enabled build). Pass it explicitly
	// so chromedp launches that binary rather than probing PATH and possibly
	// finding a codec-less chromium. Outside the container CHROME_BIN is
	// unset and chromedp falls back to its normal PATH probe.
	if bin := os.Getenv("CHROME_BIN"); bin != "" {
		opts = append(opts, chromedp.ExecPath(bin))
	}

	// When the miner's global proxy is configured, GRUB_SIDECAR_PROXY is set
	// on this container's env so Chrome's own egress routes through it too
	// (see internal/platform/kick/sidecars.go sidecarEnv).
	opts = append(opts, proxyAllocOpts(os.Getenv("GRUB_SIDECAR_PROXY"))...)

	allocCtx, cancel := chromedp.NewExecAllocator(ctx, opts...)
	return &Browser{
		allocCtx:    allocCtx,
		allocCancel: cancel,
		baseCtx:     ctx,
		baseOpts:    opts,
		tabs:        map[string]tabState{},
	}
}

// proxyAllocOpts returns a chromedp option enabling Chrome's proxy when
// proxyURL is non-empty, else nothing. Chrome accepts http/https/socks5.
func proxyAllocOpts(proxyURL string) []chromedp.ExecAllocatorOption {
	if proxyURL == "" {
		return nil
	}
	return []chromedp.ExecAllocatorOption{chromedp.Flag("proxy-server", proxyURL)}
}

// StealthScript is the JS payload evaluated on every new document
// before page scripts run. Three goals:
//  1. Remove the standard "I'm a bot" tells PerimeterX / Kasada look
//     for: navigator.webdriver, missing window.chrome.runtime,
//     suspicious plugin/language counts.
//  2. Capture pristine references to fetch / XMLHttpRequest BEFORE
//     PerimeterX's p.js loads and wraps them. The wrapped versions
//     poison call sites that don't match an expected event trace
//     (returning "TypeError: Failed to fetch"). Using __origFetch
//     from our evalFetch bypasses the wrapper entirely.
//  3. Stash original Function / Promise prototypes so the page can
//     detect that *if* we later mess with them — currently we don't.
const StealthScript = `
(() => {
  // 1. Capture pristine network primitives before any page script can
  //    wrap them. PerimeterX wraps fetch + XHR to enforce its own
  //    bot heuristics; calling __origFetch instead of fetch sidesteps
  //    that wrapper.
  try {
    if (window.fetch && !window.__origFetch) {
      window.__origFetch = window.fetch.bind(window);
    }
    if (window.XMLHttpRequest && !window.__OrigXHR) {
      window.__OrigXHR = window.XMLHttpRequest;
    }
  } catch(e) {}

  // Hide navigator.webdriver
  try {
    Object.defineProperty(navigator, 'webdriver', {get: () => undefined});
  } catch(e) {}

  // Fake window.chrome.runtime (headless Chrome leaves chrome.runtime undefined)
  if (!window.chrome) { window.chrome = {}; }
  if (!window.chrome.runtime) { window.chrome.runtime = {}; }
  if (!window.chrome.app) {
    window.chrome.app = {
      isInstalled: false,
      InstallState: { DISABLED: 'disabled', INSTALLED: 'installed', NOT_INSTALLED: 'not_installed' },
      RunningState: { CANNOT_RUN: 'cannot_run', READY_TO_RUN: 'ready_to_run', RUNNING: 'running' }
    };
  }

  // NOTE: do NOT override navigator.plugins. Chrome's "new" headless mode
  // already exposes a genuine 5-entry PluginArray (the PDF-viewer set:
  // "PDF Viewer", "Chrome PDF Viewer", ...) that is indistinguishable from
  // headed Chrome — spoofing it is both unnecessary AND actively harmful.
  // A previous override returned a plain Array [1,2,3,4,5] (wrong prototype,
  // fake names): that made the browser MORE bot-detectable than the real
  // value, and — the load-bearing bug — it broke Kick's AWS IVS web player
  // environment probe, so the <video> never attached a MediaSource
  // (readyState stuck at 0, currentTime never advanced, zero watch-credit).
  // Leaving the real PluginArray in place fixes IVS playback in headless.

  // Patch navigator.languages
  try {
    Object.defineProperty(navigator, 'languages', {get: () => ['en-US', 'en']});
  } catch(e) {}

  // Patch permission query so it doesn't reveal headless quirks
  const originalQuery = navigator.permissions && navigator.permissions.query;
  if (originalQuery) {
    navigator.permissions.query = (parameters) => (
      parameters.name === 'notifications'
        ? Promise.resolve({state: Notification.permission})
        : originalQuery(parameters)
    );
  }
})();
`

// Close terminates the browser allocator and all open tabs.
func (b *Browser) Close() {
	b.mu.Lock()
	for _, t := range b.tabs {
		t.cancel()
	}
	b.tabs = map[string]tabState{}
	b.mu.Unlock()
	b.allocCancel()
}

// OpenIncognitoTab spawns a fresh, isolated chrome process whose
// cookie jar / storage is independent of the main browser. Used for
// scraping sites as a "logged-out" visitor while authenticated tabs
// for the same domain stay live.
//
// We spawn a SECOND ExecAllocator (i.e. a second Chrome OS process)
// instead of Target.createBrowserContext because chromedp/headless-shell
// — what this container ships — rejects that CDP command with
// "Not allowed (-32000)". A second process is heavier but isolated.
//
// The cleanup func MUST be called to terminate the chrome process and
// release temp storage. The returned context is bound to the new
// browser's first tab.
func (b *Browser) OpenIncognitoTab() (context.Context, func(), error) {
	tmpDir, err := os.MkdirTemp("", "grubdrops-incog-*")
	if err != nil {
		return nil, nil, fmt.Errorf("temp dir: %w", err)
	}
	opts := append([]chromedp.ExecAllocatorOption{}, b.baseOpts...)
	opts = append(opts, chromedp.UserDataDir(tmpDir))
	allocCtx, allocCancel := chromedp.NewExecAllocator(b.baseCtx, opts...)
	tabCtx, tabCancel := chromedp.NewContext(allocCtx)
	if err := chromedp.Run(tabCtx); err != nil {
		tabCancel()
		allocCancel()
		_ = os.RemoveAll(tmpDir)
		return nil, nil, fmt.Errorf("incognito browser: %w", err)
	}
	cleanup := func() {
		tabCancel()
		allocCancel()
		_ = os.RemoveAll(tmpDir)
	}
	return tabCtx, cleanup, nil
}

// OpenTab creates a new tab and returns an opaque handle.
func (b *Browser) OpenTab() (string, context.Context, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.next++
	handle := fmt.Sprintf("tab_%d", b.next)
	tabCtx, cancel := chromedp.NewContext(b.allocCtx)
	if err := chromedp.Run(tabCtx); err != nil {
		cancel()
		return "", nil, fmt.Errorf("create tab: %w", err)
	}
	b.tabs[handle] = tabState{ctx: tabCtx, cancel: cancel}
	return handle, tabCtx, nil
}

// Tab returns the context for an existing tab handle.
func (b *Browser) Tab(handle string) (context.Context, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	t, ok := b.tabs[handle]
	if !ok {
		return nil, false
	}
	return t.ctx, true
}

// CloseTab terminates a single tab.
func (b *Browser) CloseTab(handle string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if t, ok := b.tabs[handle]; ok {
		t.cancel()
		delete(b.tabs, handle)
	}
}
