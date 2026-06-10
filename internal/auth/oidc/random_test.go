package oidc

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRandomString_DistinctAndLong(t *testing.T) {
	a := randomString(32)
	b := randomString(32)
	require.NotEqual(t, a, b)
	require.GreaterOrEqual(t, len(a), 32)
}

func TestPKCE_ChallengeMatchesVerifier(t *testing.T) {
	verifier := randomString(32)
	challenge := pkceChallenge(verifier)
	require.NotEmpty(t, challenge)
	require.NotEqual(t, verifier, challenge)
	// deterministic: same verifier -> same challenge
	require.Equal(t, challenge, pkceChallenge(verifier))
}
