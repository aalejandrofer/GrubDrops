package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	HTTPAddr          string
	DBPath            string
	MasterKey         string
	DiscordWebhookURL string
	SecureCookies     bool
	BrowserURL        string
	LogLevel          string
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
	if strings.TrimSpace(cfg.MasterKey) == "" {
		return Config{}, fmt.Errorf("GRUB_MASTER_KEY is required")
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
