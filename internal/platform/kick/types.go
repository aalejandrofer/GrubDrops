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
