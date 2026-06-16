package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	HTTPAddr          string
	DBPath            string
	MasterKey         string
	DiscordWebhookURL string
	SecureCookies     bool
	BrowserURL        string
	// Location is the timezone used for all displayed times. Loaded from the
	// TZ environment variable (e.g. "Asia/Shanghai"). Defaults to UTC.
	Location *time.Location
	// BrowserURLs is the full sidecar pool (GRUB_BROWSER_URLS, comma-sep).
	// Kick shards accounts across these so two logged-in Kick accounts get
	// their own Chrome (one shared Chrome collides on the kick.com cookie).
	// Falls back to [BrowserURL] when unset. BrowserURL stays the login /
	// Twitch / display client.
	BrowserURLs []string
	LogLevel    string

	// KickSidecarTemplate names each Kick account's sidecar container from its
	// username slug ("{slug}" placeholder). Default "grubdrops-browser-{slug}".
	KickSidecarTemplate string
	KickSidecarPort     int
	// KickSidecarImage is the browser image the miner auto-creates per-account
	// sidecars from. Default ghcr.io/aalejandrofer/grubdrops-browser:latest.
	KickSidecarImage string
	// KickSidecarNetwork overrides network self-detection. When empty the miner
	// inspects its own container to find the network it shares with sidecars.
	KickSidecarNetwork string

	// OIDC single-sign-on (all optional; feature enabled only when issuer,
	// client id, client secret, and redirect URL are all set).
	OIDCIssuer        string
	OIDCClientID      string
	OIDCClientSecret  string
	OIDCRedirectURL   string
	OIDCProviderName  string
	OIDCAllowedEmails []string
	OIDCAllowedGroups []string
}

func Load() (Config, error) {
	cfg := Config{
		HTTPAddr:          getenv("GRUB_HTTP_ADDR", "0.0.0.0:8080"),
		DBPath:            getenv("GRUB_DB_PATH", "/data/miner.db"),
		MasterKey:         os.Getenv("GRUB_MASTER_KEY"),
		DiscordWebhookURL: os.Getenv("GRUB_DISCORD_WEBHOOK"),
		SecureCookies:     parseBool(os.Getenv("GRUB_SECURE_COOKIES")),
		BrowserURL:        os.Getenv("GRUB_BROWSER_URL"),
		LogLevel:          strings.ToLower(getenv("GRUB_LOG_LEVEL", "info")),
	}
	cfg.BrowserURLs = splitList(os.Getenv("GRUB_BROWSER_URLS"))
	if len(cfg.BrowserURLs) == 0 && cfg.BrowserURL != "" {
		cfg.BrowserURLs = []string{cfg.BrowserURL}
	}
	if cfg.BrowserURL == "" && len(cfg.BrowserURLs) > 0 {
		cfg.BrowserURL = cfg.BrowserURLs[0]
	}
	cfg.KickSidecarTemplate = getenv("GRUB_KICK_SIDECAR_TEMPLATE", "grubdrops-browser-{slug}")
	cfg.KickSidecarPort = 9090
	if v := os.Getenv("GRUB_KICK_SIDECAR_PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.KickSidecarPort = n
		}
	}
	cfg.KickSidecarImage = getenv("GRUB_KICK_SIDECAR_IMAGE", "ghcr.io/aalejandrofer/grubdrops-browser:latest")
	cfg.KickSidecarNetwork = os.Getenv("GRUB_KICK_SIDECAR_NETWORK")
	cfg.OIDCIssuer = os.Getenv("GRUB_OIDC_ISSUER")
	cfg.OIDCClientID = os.Getenv("GRUB_OIDC_CLIENT_ID")
	cfg.OIDCClientSecret = os.Getenv("GRUB_OIDC_CLIENT_SECRET")
	cfg.OIDCRedirectURL = os.Getenv("GRUB_OIDC_REDIRECT_URL")
	cfg.OIDCProviderName = getenv("GRUB_OIDC_PROVIDER_NAME", "SSO")
	cfg.OIDCAllowedEmails = splitList(os.Getenv("GRUB_OIDC_ALLOWED_EMAILS"))
	cfg.OIDCAllowedGroups = splitList(os.Getenv("GRUB_OIDC_ALLOWED_GROUPS"))
	if strings.TrimSpace(cfg.MasterKey) == "" {
		return Config{}, fmt.Errorf("GRUB_MASTER_KEY is required")
	}
	// Timezone: TZ env var → time.Local (Docker-style).
	cfg.Location = time.UTC
	if tz := os.Getenv("TZ"); tz != "" {
		if loc, err := time.LoadLocation(tz); err == nil {
			cfg.Location = loc
		}
	}
	return cfg, nil
}

func parseBool(s string) bool {
	if s == "" {
		return false
	}
	b, _ := strconv.ParseBool(s)
	return b
}

func getenv(k, d string) string {
	if v, ok := os.LookupEnv(k); ok && v != "" {
		return v
	}
	return d
}

// OIDCEnabled reports whether all mandatory OIDC settings are present.
func (c Config) OIDCEnabled() bool {
	return c.OIDCIssuer != "" && c.OIDCClientID != "" &&
		c.OIDCClientSecret != "" && c.OIDCRedirectURL != ""
}

// splitList parses a comma-separated env value into a trimmed, non-empty slice.
func splitList(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}
