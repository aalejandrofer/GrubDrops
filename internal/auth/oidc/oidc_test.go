package oidc

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"net/url"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/stretchr/testify/require"
)

func newTestProvider(t *testing.T, idp *fakeIDP) *Provider {
	t.Helper()
	p, err := New(context.Background(), Config{
		Issuer:       idp.srv.URL,
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		RedirectURL:  "https://app.example.com/auth/oidc/callback",
		ProviderName: "Test",
	})
	require.NoError(t, err)
	return p
}

func TestNew_DisabledConfig(t *testing.T) {
	p, err := New(context.Background(), Config{})
	require.NoError(t, err)
	require.False(t, p.Enabled())
}

func TestAuthCodeURL_ContainsParams(t *testing.T) {
	idp := newFakeIDP(t)
	p := newTestProvider(t, idp)
	require.True(t, p.Enabled())
	raw := p.AuthCodeURL("state123", "nonce456", "challengeXYZ")
	u, err := url.Parse(raw)
	require.NoError(t, err)
	q := u.Query()
	require.Equal(t, "state123", q.Get("state"))
	require.Equal(t, "nonce456", q.Get("nonce"))
	require.Equal(t, "challengeXYZ", q.Get("code_challenge"))
	require.Equal(t, "S256", q.Get("code_challenge_method"))
	require.Contains(t, q.Get("scope"), "openid")
}

func TestExchangeAndVerify_Success(t *testing.T) {
	idp := newFakeIDP(t)
	idp.email = "admin@example.com"
	idp.nonce = "nonce456"
	p := newTestProvider(t, idp)

	claims, err := p.ExchangeAndVerify(context.Background(), "any-code", "verifier", "nonce456")
	require.NoError(t, err)
	require.Equal(t, "admin@example.com", claims.Email)
	require.Equal(t, "subject-123", claims.Subject)
}

func TestExchangeAndVerify_NonceMismatch(t *testing.T) {
	idp := newFakeIDP(t)
	idp.nonce = "wrong-nonce"
	p := newTestProvider(t, idp)

	_, err := p.ExchangeAndVerify(context.Background(), "any-code", "verifier", "expected-nonce")
	require.Error(t, err)
}

func TestExchangeAndVerify_RejectsWrongKey(t *testing.T) {
	idp := newFakeIDP(t)
	idp.nonce = "n"
	p := newTestProvider(t, idp)

	// Mint a token signed by a key the JWKS does NOT contain.
	wrongKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: wrongKey},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", idp.keyID),
	)
	require.NoError(t, err)
	raw, err := jwt.Signed(signer).Claims(map[string]any{
		"iss": idp.srv.URL, "sub": "s", "aud": "test-client",
		"exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(),
		"email": "x@y.com", "nonce": "n",
	}).Serialize()
	require.NoError(t, err)

	// Verify directly against the provider's verifier (bypassing the token
	// endpoint, which always signs with the correct key).
	_, err = p.verifier.Verify(context.Background(), raw)
	require.Error(t, err)
}
