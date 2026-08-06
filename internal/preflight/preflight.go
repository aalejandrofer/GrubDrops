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
//
// This validates the EXACT bytes store.NewCryptor will receive: cfg.MasterKey
// is never trimmed before it reaches age.ParseX25519Identity, so trimming
// here would let a key through that then fails at NewCryptor, after the
// database has already been opened and migrated. Surrounding whitespace is
// detected explicitly instead, so the message names the real cause rather
// than reporting a generic parse failure for a key that is otherwise valid.
func CheckMasterKey(secret string) error {
	if trimmed := strings.TrimSpace(secret); trimmed != secret && trimmed != "" {
		return fmt.Errorf(`GRUB_MASTER_KEY has leading or trailing whitespace

  The key itself may be valid, but age will not parse it with extra spaces,
  tabs, or newlines around it. This commonly happens when pasting into a
  .env file or a Portainer environment-variables box, either of which can
  silently add a trailing newline.

  Remove the leading/trailing whitespace and use the key as-is.`)
	}
	if _, err := age.ParseX25519Identity(secret); err != nil {
		return fmt.Errorf(`GRUB_MASTER_KEY is not a valid age identity

  It must look like: AGE-SECRET-KEY-1...
  Generate one with:  docker run --rm ghcr.io/aalejandrofer/grubdrops:latest keygen

  Reuse the SAME key on every restart, or stored logins cannot be decrypted.

  (underlying error: %v)`, err)
	}
	return nil
}

// CheckDataDir verifies the miner can actually write into the directory that
// will hold the database, and, if the database file already exists, that the
// file itself is writable too.
//
// This is a real write probe rather than an inspection of the mode bits.
// Mode bits do not account for a read-only filesystem, for the effective UID
// and its supplementary groups, or for ACLs, and reimplementing that logic in
// Go duplicates what the kernel answers in a single syscall. A writable
// directory is not sufficient: SQLite's "attempt to write a readonly database
// (1544)" happens when the directory is fine but the database FILE is not,
// which is exactly what README.md's non-recursive `chown data` leaves behind
// for a restored backup or a directory left over from an older, root-run
// container. This probes the file directly, in addition to the directory,
// so that case is caught before store.Open ever touches disk.
func CheckDataDir(dbPath string) error {
	// The most common misconfiguration of GRUB_DB_PATH is pointing it at the
	// mounted directory itself (e.g. "/data") rather than a file inside it.
	// filepath.Dir("/data") is "/", so without this check the operator would
	// be told to chown "/" — advice that fixes nothing. Catch it first.
	if fi, err := os.Stat(dbPath); err == nil && fi.IsDir() {
		return fmt.Errorf(`GRUB_DB_PATH names a directory, not a file

  path: %s

  GRUB_DB_PATH must name a file inside the mounted directory, for example
  /data/miner.db, not the directory itself.`, dbPath)
	}

	dir := filepath.Dir(dbPath)

	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf(`the data directory does not exist

  path: %s

  Either create it:
      mkdir -p %s
  or, if you are running under Docker, check the volume mapping in your
  compose file so GRUB_DB_PATH points inside the mounted directory.`, dir, dir)
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

	// The directory being writable does not mean the database FILE is: a
	// restored backup or a directory left over from an older, root-run
	// container can leave miner.db owned by someone else while ./data itself
	// is wide open. Skipped when the file does not exist yet — that is the
	// normal first-run case and the probe above already covers it.
	if _, err := os.Stat(dbPath); err == nil {
		wf, err := os.OpenFile(dbPath, os.O_WRONLY, 0)
		if err != nil {
			return fmt.Errorf(`the database file is not writable

  path: %s
  uid:  %d

  The directory is writable, but this file is not: it is likely owned by a
  different user, from a restored backup or an older container that ran as
  root. Fix it with a RECURSIVE chown so it reaches files already inside the
  directory, not just the directory entry itself:
      sudo chown -R %d:%d %s

  (underlying error: %v)`, dbPath, os.Getuid(), os.Getuid(), os.Getuid(), dir, err)
		}
		_ = wf.Close()
	}
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
