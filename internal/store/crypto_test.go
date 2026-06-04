package store

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Generated once via `go run filippo.io/age/cmd/age-keygen` — throwaway test key.
const testKey = "AGE-SECRET-KEY-1DZCAXYWJM6M42NSX5GR4QWZZ2JXEYKJ9ZKWYFYSNU997775JJ6XSY85FK9"

func TestCrypto_RoundTrip(t *testing.T) {
	c, err := NewCryptor(testKey)
	require.NoError(t, err)

	ct, err := c.Encrypt([]byte("hello"))
	require.NoError(t, err)
	assert.NotEmpty(t, ct)

	pt, err := c.Decrypt(ct)
	require.NoError(t, err)
	assert.Equal(t, []byte("hello"), pt)
}

func TestCrypto_RejectsBadKey(t *testing.T) {
	_, err := NewCryptor("not-a-key")
	require.Error(t, err)
}
