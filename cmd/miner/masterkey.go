package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// masterKeyFile is where an auto-generated master key is kept, next to the DB.
const masterKeyFile = "master.key"

type keySource string

const (
	keyFromEnv   keySource = "env"
	keyFromFile  keySource = "file"
	keyGenerated keySource = "generated"
)

// resolveMasterKey returns the key that encrypts stored sessions.
// GRUB_MASTER_KEY wins when set. Otherwise the key is read from
// master.key next to the DB at dbPath, which is created with a fresh key on
// first boot, so a plain `docker compose up` works without a keygen step.
// A key is never generated over an existing non-empty DB: its sessions are
// encrypted with a key that has gone missing, and a new key would make them
// unreadable until re-login overwrites them.
func resolveMasterKey(envKey, dbPath string) (string, keySource, error) {
	if k := strings.TrimSpace(envKey); k != "" {
		return k, keyFromEnv, nil
	}
	path := filepath.Join(filepath.Dir(dbPath), masterKeyFile)
	if b, err := os.ReadFile(path); err == nil {
		k := strings.TrimSpace(string(b))
		if k == "" {
			return "", "", fmt.Errorf("master key file %s is empty", path)
		}
		return k, keyFromFile, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", "", fmt.Errorf("read master key: %w", err)
	}

	if info, err := os.Stat(dbPath); err == nil && info.Size() > 0 {
		return "", "", fmt.Errorf("GRUB_MASTER_KEY is unset and no %s exists, but the database at %s already exists; restore the key or set GRUB_MASTER_KEY", masterKeyFile, dbPath)
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", "", fmt.Errorf("check database %s: %w", dbPath, err)
	}

	key, err := generateMasterKey()
	if err != nil {
		return "", "", err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", "", fmt.Errorf("save master key to %s: %w", path, err)
	}
	if _, err := f.WriteString(key + "\n"); err != nil {
		f.Close()
		return "", "", fmt.Errorf("save master key to %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return "", "", fmt.Errorf("save master key to %s: %w", path, err)
	}
	return key, keyGenerated, nil
}
