package api

import (
	"fmt"
	"strings"
)

// kickCookiesFromNetscape parses a Netscape cookies.txt export (the format
// browser extensions like "Get cookies.txt LOCALLY" produce) and extracts the
// kick.com cookies the miner needs. Lines are 7 tab-separated fields:
// domain, includeSubdomains, path, secure, expiry, name, value. '#' lines are
// comments, except the '#HttpOnly_' domain prefix some exporters emit.
func kickCookiesFromNetscape(raw string) (kickCookieForm, error) {
	var f kickCookieForm
	sawKick := false
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || (strings.HasPrefix(line, "#") && !strings.HasPrefix(line, "#HttpOnly_")) {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 7 {
			continue
		}
		domain := strings.TrimPrefix(fields[0], "#HttpOnly_")
		if !isKickDomain(domain) {
			continue
		}
		sawKick = true
		switch fields[5] {
		case "kick_session":
			f.KickSession = fields[6]
		case "XSRF-TOKEN":
			f.XSRF = fields[6]
		case "cf_clearance":
			f.CFClearance = fields[6]
		case "session_token":
			f.SessionToken = fields[6]
		}
	}
	if !sawKick {
		return kickCookieForm{}, fmt.Errorf("no kick.com cookies found — export cookies.txt while on kick.com")
	}
	var missing []string
	if f.KickSession == "" {
		missing = append(missing, "kick_session")
	}
	if f.SessionToken == "" {
		missing = append(missing, "session_token")
	}
	if len(missing) > 0 {
		return kickCookieForm{}, fmt.Errorf("missing required cookie(s): %s — make sure you're signed in to kick.com before exporting", strings.Join(missing, ", "))
	}
	return f, nil
}

func isKickDomain(d string) bool {
	d = strings.TrimPrefix(strings.ToLower(d), ".")
	return d == "kick.com" || strings.HasSuffix(d, ".kick.com")
}

// twitchAuthTokenFromNetscape extracts the Twitch auth-token cookie from a
// Netscape cookies.txt export (the format "Get cookies.txt LOCALLY" produces),
// mirroring the Kick paste flow. Note: a web-issued auth-token may not
// authenticate against the Android client_id the direct backend uses — Twitch
// login is device-code only — but this lets a user try a cookies.txt import
// the same way Kick works.
func twitchAuthTokenFromNetscape(raw string) (string, error) {
	sawTwitch := false
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || (strings.HasPrefix(line, "#") && !strings.HasPrefix(line, "#HttpOnly_")) {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 7 {
			continue
		}
		domain := strings.TrimPrefix(fields[0], "#HttpOnly_")
		if !isTwitchDomain(domain) {
			continue
		}
		sawTwitch = true
		if fields[5] == "auth-token" {
			return fields[6], nil
		}
	}
	if !sawTwitch {
		return "", fmt.Errorf("no twitch.tv cookies found — export cookies.txt while on twitch.tv")
	}
	return "", fmt.Errorf("missing auth-token cookie — make sure you're signed in to twitch.tv before exporting")
}

func isTwitchDomain(d string) bool {
	d = strings.TrimPrefix(strings.ToLower(d), ".")
	return d == "twitch.tv" || strings.HasSuffix(d, ".twitch.tv")
}
