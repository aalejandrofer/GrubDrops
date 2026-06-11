package helper

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/browserutils/kooky"
	"github.com/browserutils/kooky/browser/chrome"
)

// readExtraChromiumCookies scans Chromium-based browsers that kooky's auto
// finder misses — chiefly Opera GX, which kooky only knows as "Opera Stable".
// The on-disk cookie format is Chrome's, so chrome.ReadCookies decodes them
// (auto-locating each profile's Local State for DPAPI/keyring decryption).
// Best-effort: absent profiles and unreadable stores are skipped silently.
func readExtraChromiumCookies(ctx context.Context, domain, browser string) []*kooky.Cookie {
	if browser != "" {
		// A pinned browser only makes sense to scan here if it's one of the
		// Chromium variants we add (opera/operagx/vivaldi/chrome-ish).
		b := strings.ToLower(browser)
		if !strings.Contains(b, "opera") && !strings.Contains(b, "gx") &&
			!strings.Contains(b, "vivaldi") && !strings.Contains(b, "chrom") {
			return nil
		}
	}
	var out []*kooky.Cookie
	for _, db := range extraChromiumCookieDBs() {
		if _, err := os.Stat(db); err != nil {
			continue // profile not installed
		}
		cs, err := chrome.ReadCookies(ctx, db, kooky.Valid, kooky.DomainHasSuffix(domain))
		if err != nil {
			dlog("extra chromium store %s: %v", db, err)
			continue
		}
		dlog("extra chromium store %s yielded %d cookies for %s", db, len(cs), domain)
		out = append(out, cs...)
	}
	return out
}

// extraChromiumProfileDirs returns the profile directories of Chromium
// browsers kooky doesn't natively locate, for the current OS.
func extraChromiumProfileDirs() []string {
	switch runtime.GOOS {
	case "windows":
		appData := os.Getenv("APPDATA")    // %AppData% (Roaming) — Opera lives here
		local := os.Getenv("LOCALAPPDATA") // Vivaldi lives here
		var d []string
		if appData != "" {
			d = append(d, filepath.Join(appData, "Opera Software", "Opera GX Stable"))
		}
		if local != "" {
			d = append(d, filepath.Join(local, "Vivaldi", "User Data", "Default"))
		}
		return d
	case "darwin":
		home, _ := os.UserHomeDir()
		as := filepath.Join(home, "Library", "Application Support")
		return []string{
			filepath.Join(as, "com.operasoftware.OperaGX"),
			filepath.Join(as, "Vivaldi", "Default"),
		}
	default: // linux + others
		home, _ := os.UserHomeDir()
		cfg := os.Getenv("XDG_CONFIG_HOME")
		if cfg == "" {
			cfg = filepath.Join(home, ".config")
		}
		return []string{
			filepath.Join(cfg, "opera-gx"),
			filepath.Join(cfg, "vivaldi", "Default"),
		}
	}
}

// extraChromiumCookieDBs expands each profile dir to the candidate cookie DB
// locations. Modern Chromium keeps the DB under Network/Cookies; older builds
// put it directly in the profile root.
func extraChromiumCookieDBs() []string {
	var out []string
	for _, d := range extraChromiumProfileDirs() {
		out = append(out,
			filepath.Join(d, "Network", "Cookies"),
			filepath.Join(d, "Cookies"),
		)
	}
	return out
}
