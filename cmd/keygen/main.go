// Command keygen prints a fresh age X25519 identity for use as GRUB_MASTER_KEY.
// Run: go run ./cmd/keygen
package main

import (
	"fmt"

	"filippo.io/age"
)

func main() {
	id, err := age.GenerateX25519Identity()
	if err != nil {
		panic(err)
	}
	// Just the secret identity line, so it's trivial to capture into an env var.
	fmt.Println(id.String())
}
