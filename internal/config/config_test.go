package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad_RequiresMasterKey(t *testing.T) {
	t.Setenv("GRUB_MASTER_KEY", "")
	_, err := Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "GRUB_MASTER_KEY")
}

func TestLoad_DefaultsApplied(t *testing.T) {
	t.Setenv("GRUB_MASTER_KEY", "AGE-SECRET-KEY-1XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX0")
	t.Setenv("GRUB_DISCORD_WEBHOOK", "")
	t.Setenv("GRUB_BROWSER_URL", "")

	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, "0.0.0.0:8080", cfg.HTTPAddr)
	assert.Equal(t, "/data/miner.db", cfg.DBPath)
	assert.Equal(t, "", cfg.DiscordWebhookURL)
	assert.Equal(t, "", cfg.BrowserURL)
}

func TestLoad_Overrides(t *testing.T) {
	t.Setenv("GRUB_MASTER_KEY", "AGE-SECRET-KEY-1XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX0")
	t.Setenv("GRUB_HTTP_ADDR", "127.0.0.1:9000")
	t.Setenv("GRUB_DB_PATH", "/tmp/m.db")
	t.Setenv("GRUB_DISCORD_WEBHOOK", "https://discord.example/wh/x")
	t.Setenv("GRUB_BROWSER_URL", "browser:9090")

	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, "127.0.0.1:9000", cfg.HTTPAddr)
	assert.Equal(t, "/tmp/m.db", cfg.DBPath)
	assert.Equal(t, "https://discord.example/wh/x", cfg.DiscordWebhookURL)
	assert.Equal(t, "browser:9090", cfg.BrowserURL)
}
