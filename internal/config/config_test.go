package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad_RequiresMasterKey(t *testing.T) {
	t.Setenv("MINER_MASTER_KEY", "")
	_, err := Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "MINER_MASTER_KEY")
}

func TestLoad_DefaultsApplied(t *testing.T) {
	t.Setenv("MINER_MASTER_KEY", "AGE-SECRET-KEY-1XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX0")

	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, "0.0.0.0:8080", cfg.HTTPAddr)
	assert.Equal(t, "/data/miner.db", cfg.DBPath)
	assert.Equal(t, false, cfg.SeedFakeAccount)
}

func TestLoad_Overrides(t *testing.T) {
	t.Setenv("MINER_MASTER_KEY", "AGE-SECRET-KEY-1XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX0")
	t.Setenv("MINER_HTTP_ADDR", "127.0.0.1:9000")
	t.Setenv("MINER_DB_PATH", "/tmp/m.db")
	t.Setenv("MINER_SEED_FAKE_ACCOUNT", "true")

	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, "127.0.0.1:9000", cfg.HTTPAddr)
	assert.Equal(t, "/tmp/m.db", cfg.DBPath)
	assert.True(t, cfg.SeedFakeAccount)
}
