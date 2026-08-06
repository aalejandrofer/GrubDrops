// Package preflight validates the environment before the miner opens its
// database, so that a misconfigured container reports the fix rather than a
// SQLite error code.
//
// Every user-reported problem with this project has been a deployment failure
// rather than a mining bug, so these messages are part of the product: they
// are read by a human in a log pane, often in Portainer, and they are the only
// thing standing between a new self-hoster and giving up.
//
// This package deliberately imports only the standard library and
// filippo.io/age. It must not be able to fail for the reasons it exists to
// diagnose.
package preflight

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"filippo.io/age"
)

// CheckMasterKey verifies GRUB_MASTER_KEY is an age X25519 identity. It
// touches no disk, so it is cheap enough to run before anything else.
//
// Ordering matters: the miner used to validate this only after opening and
// migrating the database, so a bad key cost eighteen log lines and left a
// half-created database behind. With the quickstart compose restarting on
// failure, that repeated forever and buried the one useful line.
func CheckMasterKey(secret string) error {
	if _, err := age.ParseX25519Identity(strings.TrimSpace(secret)); err != nil {
		return fmt.Errorf(`GRUB_MASTER_KEY is not a valid age identity

  It must look like: AGE-SECRET-KEY-1...
  Generate one with:  docker run --rm ghcr.io/aalejandrofer/grubdrops:latest keygen

  Reuse the SAME key on every restart, or stored logins cannot be decrypted.

  (underlying error: %v)`, err)
	}
	return nil
}

// CheckDataDir verifies the miner can actually write into the directory that
// will hold the database.
//
// This is a real write probe rather than an inspection of the mode bits.
// Mode bits do not account for a read-only filesystem, for the effective UID
// and its supplementary groups, or for ACLs, and reimplementing that logic in
// Go duplicates what the kernel answers in a single syscall. The probe also
// collapses two different SQLite failures into one message: "unable to open
// database file (14)" when the directory is unwritable and no database exists
// yet, and "attempt to write a readonly database (1544)" when the directory is
// read-only but the database file is already there. Neither names permissions,
// the UID, or the mount, and the fix for both is the same chown.
func CheckDataDir(dbPath string) error {
	dir := filepath.Dir(dbPath)

	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf(`the data directory does not exist

  path: %s

  Nothing is mounted there. Check the volume mapping in your compose file,
  and that GRUB_DB_PATH points inside it.`, dir)
		}
		return fmt.Errorf(`the data directory cannot be read

  path: %s

  (underlying error: %v)`, dir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf(`the data directory is not a directory

  path: %s

  GRUB_DB_PATH should be a file inside a mounted directory, for example
  /data/miner.db.`, dir)
	}

	f, err := os.CreateTemp(dir, ".grubdrops-preflight-*")
	if err != nil {
		return fmt.Errorf(`the data directory is not writable

  path: %s
  uid:  %d

  The directory mounted at %s must be writable by UID %d.
  If you are using the default compose file, which mounts ./data, run this
  on the host:
      sudo chown -R %d:%d ./data
  Otherwise, chown the host directory you have mounted at %s.
  A named Docker volume avoids this entirely.

  (underlying error: %v)`, dir, os.Getuid(), dir, os.Getuid(), os.Getuid(), os.Getuid(), dir, err)
	}
	name := f.Name()
	// Close before removing so the handle is never left open on an error path.
	_ = f.Close()
	_ = os.Remove(name)
	return nil
}

// Run performs every startup check in cost order and returns the first
// failure. The master key is checked first because it touches no disk: a bad
// key should never cost a database open, let alone a full migration.
func Run(dbPath, masterKey string) error {
	if err := CheckMasterKey(masterKey); err != nil {
		return err
	}
	return CheckDataDir(dbPath)
}
