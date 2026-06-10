package oidc

import (
	"fmt"
	"strings"
)

// Claims is the subset of ID-token claims grubdrops cares about.
type Claims struct {
	Subject string   `json:"sub"`
	Email   string   `json:"email"`
	Groups  []string `json:"groups"`
}

// Authorize applies the optional email and group allowlists. Empty lists mean
// "no restriction". Both lists, when set, must be satisfied.
func (p *Provider) Authorize(c Claims) error {
	if len(p.allowedEmails) > 0 {
		if !containsFold(p.allowedEmails, c.Email) {
			return fmt.Errorf("email %q not in allowlist", c.Email)
		}
	}
	if len(p.allowedGroups) > 0 {
		if !anyFold(p.allowedGroups, c.Groups) {
			return fmt.Errorf("no group in allowlist for %q", c.Email)
		}
	}
	return nil
}

func containsFold(list []string, v string) bool {
	for _, item := range list {
		if strings.EqualFold(strings.TrimSpace(item), strings.TrimSpace(v)) {
			return true
		}
	}
	return false
}

func anyFold(allowed, have []string) bool {
	for _, h := range have {
		if containsFold(allowed, h) {
			return true
		}
	}
	return false
}
