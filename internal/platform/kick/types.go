package kick

import (
	"encoding/json"

	pb "github.com/aalejandrofer/grubdrops/internal/auth/browser/gen/browser/v1"
	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// kickSession is the JSON we serialize into platform.Session.Cookies +
// CSRF as a single encoded blob, so the rest of the daemon's
// session-store machinery (encrypted via age) reuses unchanged.
type kickSession struct {
	Cookies   []cookie `json:"cookies"`
	XSRFToken string   `json:"xsrf_token"`
	UserAgent string   `json:"user_agent"`
}

type cookie struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Domain string `json:"domain"`
	Path   string `json:"path"`
}

// encodeSession packs a Kick browser session into a platform.Session.
// AccessToken stays empty (Kick has no bearer); we stash everything in
// the Cookies map under a single key so the existing JSON marshaller
// rounds-trips correctly.
func encodeSession(ks kickSession) (platform.Session, error) {
	raw, err := json.Marshal(ks)
	if err != nil {
		return platform.Session{}, err
	}
	return platform.Session{
		Cookies: map[string]string{"kick": string(raw)},
		CSRF:    ks.XSRFToken,
	}, nil
}

func decodeSession(p platform.Session) (kickSession, error) {
	raw, ok := p.Cookies["kick"]
	if !ok {
		return kickSession{}, nil
	}
	var ks kickSession
	if err := json.Unmarshal([]byte(raw), &ks); err != nil {
		return kickSession{}, err
	}
	return ks, nil
}

// patchKickSession returns a copy of sess with ONLY the cookie fields
// (cookies + xsrf_token) inside the stored "kick" JSON blob updated to
// merged, and CSRF mirrored to match. Every other key already in that blob
// — notably "channel"/"channels"/"username", which the login handler writes
// (internal/api/handlers_login_kick.go, kickSessionForStorage) but
// kickSession does not model — is preserved untouched, and every other
// platform.Session field (ExpiresAt, RefreshToken, AccessToken, ...) is
// carried over unchanged.
//
// This exists because kickSession only models cookies/xsrf_token/user_agent:
// a decode-into-kickSession-then-encodeSession round trip silently drops
// everything else. In particular it zeroes ExpiresAt, which makes the
// boot-time scheduler reload treat a perfectly live session as expired
// (cmd/miner/main.go's s.ExpiresAt.Before(time.Now()) gate) and replace the
// account with an idling nopRunner — and it drops "channels", leaving the
// account with nothing to watch. Patching the existing JSON object instead
// of rebuilding it avoids both.
func patchKickSession(sess platform.Session, merged kickSession) (platform.Session, error) {
	var obj map[string]json.RawMessage
	if raw, ok := sess.Cookies["kick"]; ok {
		// Best-effort: a malformed existing blob just means obj stays nil
		// and we start from an empty object rather than fail the rotation.
		_ = json.Unmarshal([]byte(raw), &obj)
	}
	if obj == nil {
		obj = map[string]json.RawMessage{}
	}

	cookiesRaw, err := json.Marshal(merged.Cookies)
	if err != nil {
		return platform.Session{}, err
	}
	xsrfRaw, err := json.Marshal(merged.XSRFToken)
	if err != nil {
		return platform.Session{}, err
	}
	obj["cookies"] = cookiesRaw
	obj["xsrf_token"] = xsrfRaw

	patched, err := json.Marshal(obj)
	if err != nil {
		return platform.Session{}, err
	}

	updated := sess // preserves ExpiresAt, AccessToken, RefreshToken, ...
	// sess.Cookies is a live map the caller (and other goroutines inside
	// do()) may still read; copy it rather than mutating in place so this
	// patch never races a concurrent reader.
	newCookies := make(map[string]string, len(sess.Cookies)+1)
	for k, v := range sess.Cookies {
		newCookies[k] = v
	}
	newCookies["kick"] = string(patched)
	updated.Cookies = newCookies
	updated.CSRF = merged.XSRFToken
	return updated, nil
}

// toProto converts the internal session form into the gRPC type used
// by the sidecar.
func toProto(ks kickSession) *pb.KickSession {
	cookies := make([]*pb.Cookie, 0, len(ks.Cookies))
	for _, c := range ks.Cookies {
		cookies = append(cookies, &pb.Cookie{
			Name: c.Name, Value: c.Value, Domain: c.Domain, Path: c.Path,
		})
	}
	return &pb.KickSession{
		Cookies:   cookies,
		XsrfToken: ks.XSRFToken,
		UserAgent: ks.UserAgent,
	}
}
