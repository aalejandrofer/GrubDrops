package preflight

import (
	"strings"
	"testing"

	"filippo.io/age"
)

// validKey returns a real age X25519 identity string.
func validKey(t *testing.T) string {
	t.Helper()
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("generate identity: %v", err)
	}
	return id.String()
}

func TestCheckMasterKey_ValidIdentityPasses(t *testing.T) {
	if err := CheckMasterKey(validKey(t)); err != nil {
		t.Fatalf("valid age identity rejected: %v", err)
	}
}

// The exact shape of issue #33: the old docs told users to make a key with
// `head -c32 /dev/urandom | base64`, which is not an age identity.
func TestCheckMasterKey_RandomStringIsRejected(t *testing.T) {
	err := CheckMasterKey("Zm9vYmFyYmF6cXV4MTIzNDU2Nzg5MGFiY2RlZg==")
	if err == nil {
		t.Fatal("a random base64 string must be rejected")
	}
	msg := err.Error()
	// The message has to carry the fix, not just the diagnosis. A user who
	// only reads this one line must know what to type next.
	if !strings.Contains(msg, "AGE-SECRET-KEY-1") {
		t.Fatalf("message must show the expected key shape, got: %s", msg)
	}
	if !strings.Contains(msg, "keygen") {
		t.Fatalf("message must name the keygen command, got: %s", msg)
	}
}

func TestCheckMasterKey_EmptyIsRejected(t *testing.T) {
	if err := CheckMasterKey(""); err == nil {
		t.Fatal("an empty key must be rejected")
	}
}

// Surrounding whitespace is easy to introduce in a .env file or a Portainer
// environment-variables box, and is not the user's mistake in spirit.
func TestCheckMasterKey_TrimsSurroundingWhitespace(t *testing.T) {
	if err := CheckMasterKey("  " + validKey(t) + "\n"); err != nil {
		t.Fatalf("a key with surrounding whitespace should be accepted: %v", err)
	}
}
