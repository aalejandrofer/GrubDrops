package store

import "github.com/aalejandrofer/grubdrops/internal/store/gen"

// AccountNameMax is the longest custom account name, in characters.
const AccountNameMax = 40

// AccountName is the name shown for an account: the user-set custom name, or
// the platform display name when none is set.
func AccountName(a gen.Account) string {
	if a.CustomName != "" {
		return a.CustomName
	}
	return a.DisplayName
}
