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
