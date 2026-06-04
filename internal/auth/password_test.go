package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHashVerifyRoundTrip(t *testing.T) {
	h, err := HashPassword("hunter2!")
	require.NoError(t, err)
	assert.NotEmpty(t, h)

	require.NoError(t, VerifyPassword(h, "hunter2!"))
}

func TestVerifyRejectsWrongPassword(t *testing.T) {
	h, err := HashPassword("hunter2!")
	require.NoError(t, err)
	require.Error(t, VerifyPassword(h, "wrong"))
}

func TestHashEmptyRejected(t *testing.T) {
	_, err := HashPassword("")
	require.Error(t, err)
}

func TestHashTooShortRejected(t *testing.T) {
	_, err := HashPassword("short")
	require.ErrorIs(t, err, ErrPasswordTooShort)
}
