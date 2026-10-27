package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad_OIDCDisabledWhenUnset(t *testing.T) {
	t.Setenv("GRUB_MASTER_KEY", "k")
	cfg, err := Load()
	require.NoError(t, err)
	require.False(t, cfg.OIDCEnabled())
}

func TestLoad_OIDCEnabledWhenComplete(t *testing.T) {
	t.Setenv("GRUB_MASTER_KEY", "k")
	t.Setenv("GRUB_OIDC_ISSUER", "https://idp.example.com/")
	t.Setenv("GRUB_OIDC_CLIENT_ID", "cid")
	t.Setenv("GRUB_OIDC_CLIENT_SECRET", "secret")
	t.Setenv("GRUB_OIDC_REDIRECT_URL", "https://app.example.com/auth/oidc/callback")
	cfg, err := Load()
	require.NoError(t, err)
	require.True(t, cfg.OIDCEnabled())
	require.Equal(t, "https://idp.example.com/", cfg.OIDCIssuer)
	require.Equal(t, []string{"a@x.com", "b@y.com"}, splitList("a@x.com, b@y.com"))
}

func TestLoad_OIDCDisabledWhenPartial(t *testing.T) {
	t.Setenv("GRUB_MASTER_KEY", "k")
	t.Setenv("GRUB_OIDC_ISSUER", "https://idp.example.com/")
	// missing client id/secret/redirect
	cfg, err := Load()
	require.NoError(t, err)
	require.False(t, cfg.OIDCEnabled())
}

func TestSplitList(t *testing.T) {
	require.Nil(t, splitList(""))
	require.Equal(t, []string{"a", "b"}, splitList(" a , b "))
}

func TestLoad_MasterKeyOptional(t *testing.T) {
	t.Setenv("GRUB_MASTER_KEY", "")
	cfg, err := Load()
	require.NoError(t, err, "an unset key is generated at startup")
	assert.Empty(t, cfg.MasterKey)
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
