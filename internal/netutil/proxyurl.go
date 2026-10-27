package netutil

import "net/url"

// MaskedPassword stands in for a proxy password the UI and logs never show.
const MaskedPassword = "***"

// MaskProxyURL replaces the password in a proxy URL with MaskedPassword. A URL
// without a password, or one that does not parse, comes back unchanged.
func MaskProxyURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.User == nil {
		return raw
	}
	if _, ok := u.User.Password(); !ok {
		return raw
	}
	m := *u
	m.User = url.UserPassword(u.User.Username(), MaskedPassword)
	return m.String()
}

// RestoreProxyPassword puts saved's password back into submitted when
// submitted still carries MaskedPassword, so an edit to the host or port of a
// masked URL keeps the stored password. Any other value comes back unchanged.
func RestoreProxyPassword(submitted, saved string) string {
	u, err := url.Parse(submitted)
	if err != nil || u.User == nil {
		return submitted
	}
	if pw, ok := u.User.Password(); !ok || pw != MaskedPassword {
		return submitted
	}
	s, err := url.Parse(saved)
	if err != nil || s.User == nil {
		return submitted
	}
	pw, ok := s.User.Password()
	if !ok {
		return submitted
	}
	u.User = url.UserPassword(u.User.Username(), pw)
	return u.String()
}
