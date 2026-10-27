package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/store"
)

func TestResolveMasterKey_EnvWins(t *testing.T) {
	dir := t.TempDir()
	key, src, err := resolveMasterKey("AGE-SECRET-KEY-1ENV", filepath.Join(dir, "miner.db"))
	require.NoError(t, err)
	assert.Equal(t, "AGE-SECRET-KEY-1ENV", key)
	assert.Equal(t, keyFromEnv, src)
	_, statErr := os.Stat(filepath.Join(dir, masterKeyFile))
	assert.True(t, os.IsNotExist(statErr), "env key must not write a key file")
}

func TestResolveMasterKey_GeneratesOnceThenReuses(t *testing.T) {
	dir := t.TempDir()
	key, src, err := resolveMasterKey("", filepath.Join(dir, "miner.db"))
	require.NoError(t, err)
	assert.Equal(t, keyGenerated, src)
	_, err = store.NewCryptor(key)
	require.NoError(t, err, "generated key must be a valid age identity")

	info, err := os.Stat(filepath.Join(dir, masterKeyFile))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	again, src, err := resolveMasterKey("  ", filepath.Join(dir, "miner.db"))
	require.NoError(t, err)
	assert.Equal(t, keyFromFile, src)
	assert.Equal(t, key, again, "restart must reuse the saved key")
}

func TestResolveMasterKey_UnwritableDir(t *testing.T) {
	_, _, err := resolveMasterKey("", filepath.Join(t.TempDir(), "missing", "dir", "miner.db"))
	require.Error(t, err)
}

func TestResolveMasterKey_RefusesToGenerateOverExistingDB(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "miner.db")
	require.NoError(t, os.WriteFile(dbPath, []byte("SQLite format 3\x00"), 0o600))

	_, _, err := resolveMasterKey("", dbPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "GRUB_MASTER_KEY is unset and no master.key exists")
	assert.Contains(t, err.Error(), dbPath)
	_, statErr := os.Stat(filepath.Join(dir, masterKeyFile))
	assert.True(t, os.IsNotExist(statErr), "must not write a key over an existing DB")
}

func TestResolveMasterKey_EmptyDBFileStillGenerates(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "miner.db")
	require.NoError(t, os.WriteFile(dbPath, nil, 0o600))

	_, src, err := resolveMasterKey("", dbPath)
	require.NoError(t, err)
	assert.Equal(t, keyGenerated, src)
}

func TestResolveMasterKey_ExistingDBWithKeyFileOrEnv(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "miner.db")
	require.NoError(t, os.WriteFile(dbPath, []byte("SQLite format 3\x00"), 0o600))

	_, src, err := resolveMasterKey("AGE-SECRET-KEY-1ENV", dbPath)
	require.NoError(t, err)
	assert.Equal(t, keyFromEnv, src)

	require.NoError(t, os.WriteFile(filepath.Join(dir, masterKeyFile), []byte("AGE-SECRET-KEY-1FILE\n"), 0o600))
	key, src, err := resolveMasterKey("", dbPath)
	require.NoError(t, err)
	assert.Equal(t, keyFromFile, src)
	assert.Equal(t, "AGE-SECRET-KEY-1FILE", key)
}
