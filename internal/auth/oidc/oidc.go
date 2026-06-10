package oidc

// Provider is grubdrops' OIDC relying-party client. Fully wired in oidc.go.
type Provider struct {
	allowedEmails []string
	allowedGroups []string
}
