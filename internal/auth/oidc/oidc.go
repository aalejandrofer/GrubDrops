package oidc

import (
	"context"
	"fmt"
	"strings"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// Config holds the resolved OIDC settings (sourced from GRUB_OIDC_* env).
type Config struct {
	Issuer        string
	ClientID      string
	ClientSecret  string
	RedirectURL   string
	ProviderName  string
	AllowedEmails []string
	AllowedGroups []string
}

func (c Config) enabled() bool {
	return c.Issuer != "" && c.ClientID != "" && c.ClientSecret != "" && c.RedirectURL != ""
}

// Provider is grubdrops' OIDC relying-party client.
type Provider struct {
	name          string
	oauth         oauth2.Config
	verifier      *gooidc.IDTokenVerifier
	allowedEmails []string
	allowedGroups []string
}

// New builds a Provider from cfg. When cfg is incomplete, it returns a
// disabled Provider (Enabled() == false) and no error, so the caller can wire
// it unconditionally. A non-nil error means the issuer was set but discovery
// failed.
func New(ctx context.Context, cfg Config) (*Provider, error) {
	p := &Provider{
		name:          cfg.ProviderName,
		allowedEmails: trimAll(cfg.AllowedEmails),
		allowedGroups: trimAll(cfg.AllowedGroups),
	}
	if p.name == "" {
		p.name = "SSO"
	}
	if !cfg.enabled() {
		return p, nil
	}

	provider, err := gooidc.NewProvider(ctx, cfg.Issuer)
	if err != nil {
		return nil, fmt.Errorf("oidc discovery: %w", err)
	}
	scopes := []string{gooidc.ScopeOpenID, "profile", "email"}
	if len(cfg.AllowedGroups) > 0 {
		scopes = append(scopes, "groups")
	}
	p.oauth = oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  cfg.RedirectURL,
		Endpoint:     provider.Endpoint(),
		Scopes:       scopes,
	}
	p.verifier = provider.Verifier(&gooidc.Config{ClientID: cfg.ClientID})
	return p, nil
}

// Enabled reports whether OIDC is configured and discovery succeeded.
func (p *Provider) Enabled() bool { return p.verifier != nil }

// Name is the display label for the login button.
func (p *Provider) Name() string { return p.name }

// AuthCodeURL builds the IdP authorize URL with state, nonce, and PKCE.
func (p *Provider) AuthCodeURL(state, nonce, challenge string) string {
	if p.verifier == nil {
		return ""
	}
	return p.oauth.AuthCodeURL(state,
		gooidc.Nonce(nonce),
		oauth2.SetAuthURLParam("code_challenge", challenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
	)
}

// ExchangeAndVerify swaps the authorization code for tokens, verifies the ID
// token signature/issuer/audience/expiry, checks the nonce, and returns the
// parsed claims.
func (p *Provider) ExchangeAndVerify(ctx context.Context, code, codeVerifier, wantNonce string) (Claims, error) {
	if p.verifier == nil {
		return Claims{}, fmt.Errorf("oidc: provider not configured")
	}
	tok, err := p.oauth.Exchange(ctx, code, oauth2.SetAuthURLParam("code_verifier", codeVerifier))
	if err != nil {
		return Claims{}, fmt.Errorf("token exchange: %w", err)
	}
	rawID, ok := tok.Extra("id_token").(string)
	if !ok || rawID == "" {
		return Claims{}, fmt.Errorf("no id_token in response")
	}
	idTok, err := p.verifier.Verify(ctx, rawID)
	if err != nil {
		return Claims{}, fmt.Errorf("verify id_token: %w", err)
	}
	if idTok.Nonce != wantNonce {
		return Claims{}, fmt.Errorf("nonce mismatch: got %q, want %q", idTok.Nonce, wantNonce)
	}
	var c Claims
	if err := idTok.Claims(&c); err != nil {
		return Claims{}, fmt.Errorf("parse claims: %w", err)
	}
	return c, nil
}

// trimAll returns a copy of list with each entry whitespace-trimmed and empty
// entries dropped. Normalizes allowlists at construction so comparisons stay
// clean.
func trimAll(list []string) []string {
	if len(list) == 0 {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, s := range list {
		if t := strings.TrimSpace(s); t != "" {
			out = append(out, t)
		}
	}
	return out
}
