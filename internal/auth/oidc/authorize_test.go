package oidc

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAuthorize(t *testing.T) {
	cases := []struct {
		name          string
		allowedEmails []string
		allowedGroups []string
		email         string
		groups        []string
		wantErr       bool
	}{
		{name: "no restrictions allows anyone", email: "x@y.com", wantErr: false},
		{name: "email match", allowedEmails: []string{"a@b.com"}, email: "a@b.com", wantErr: false},
		{name: "email match case-insensitive", allowedEmails: []string{"A@B.com"}, email: "a@b.COM", wantErr: false},
		{name: "email miss", allowedEmails: []string{"a@b.com"}, email: "z@b.com", wantErr: true},
		{name: "group match", allowedGroups: []string{"admins"}, groups: []string{"users", "admins"}, wantErr: false},
		{name: "group miss", allowedGroups: []string{"admins"}, groups: []string{"users"}, wantErr: true},
		{name: "email ok but group miss fails", allowedEmails: []string{"a@b.com"}, allowedGroups: []string{"admins"}, email: "a@b.com", groups: []string{"users"}, wantErr: true},
		{name: "both satisfied", allowedEmails: []string{"a@b.com"}, allowedGroups: []string{"admins"}, email: "a@b.com", groups: []string{"admins"}, wantErr: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := &Provider{allowedEmails: tc.allowedEmails, allowedGroups: tc.allowedGroups}
			err := p.Authorize(Claims{Email: tc.email, Groups: tc.groups})
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
