package kick

import "net/http"

// rotatableCookies are the ONLY cookie names a Set-Cookie response may
// update in a stored session. These are the three the session actually
// authenticates with, per cookieHeaderFor: session_token is the Sanctum
// bearer, XSRF-TOKEN is the CSRF header, kick_session is the web session.
// Restricting the set keeps analytics and Cloudflare cookies from churning
// the session store on every single request.
var rotatableCookies = map[string]bool{
	"session_token": true,
	"XSRF-TOKEN":    true,
	"kick_session":  true,
}

// defaultCookieDomain/Path match every cookie the login handler writes
// (internal/api/handlers_login_kick.go persistKickSession). Used as the
// last-resort fallback when a newly added rotated cookie carries neither,
// which Set-Cookie commonly omits for host-only cookies.
const (
	defaultCookieDomain = "kick.com"
	defaultCookiePath   = "/"
)

// mergeCookies folds a response's Set-Cookie headers into a stored session,
// returning the merged session and whether any authenticating cookie actually
// changed. Cookies not named in the response are preserved untouched.
//
// Kick reissues these cookies mid-session; before this they were discarded,
// so an account rode its original login until it expired.
func mergeCookies(ks kickSession, set []*http.Cookie) (kickSession, bool) {
	if len(set) == 0 {
		return ks, false
	}
	out := kickSession{
		Cookies:   make([]cookie, len(ks.Cookies)),
		XSRFToken: ks.XSRFToken,
		UserAgent: ks.UserAgent,
	}
	copy(out.Cookies, ks.Cookies)

	changed := false
	for _, sc := range set {
		if sc == nil || !rotatableCookies[sc.Name] {
			continue
		}
		// An empty value is the server DELETING a cookie. Honouring it would
		// discard a live token and log the account out on the next request,
		// so treat it as "no information" instead.
		if sc.Value == "" {
			continue
		}
		idx := -1
		for i, c := range out.Cookies {
			if c.Name == sc.Name {
				idx = i
				break
			}
		}
		if idx == -1 {
			domain, path := sc.Domain, sc.Path
			if domain == "" || path == "" {
				// Set-Cookie commonly omits Domain (host-only cookie) and
				// sometimes Path. A cookie with an empty Domain fails CDP's
				// Network.setCookie in the browser-watch sidecar
				// (network.SetCookie(...).WithDomain(...).WithPath(...)),
				// which aborts the ENTIRE cookie install on its first
				// error — so one domainless cookie would silently break
				// browser-watch auth, not just this cookie. Inherit from an
				// existing stored cookie (Kick issues them all under the
				// same domain/path) before falling back to the default.
				for _, c := range out.Cookies {
					if domain == "" && c.Domain != "" {
						domain = c.Domain
					}
					if path == "" && c.Path != "" {
						path = c.Path
					}
				}
				if domain == "" {
					domain = defaultCookieDomain
				}
				if path == "" {
					path = defaultCookiePath
				}
			}
			out.Cookies = append(out.Cookies, cookie{Name: sc.Name, Value: sc.Value, Domain: domain, Path: path})
			changed = true
		} else if out.Cookies[idx].Value != sc.Value {
			out.Cookies[idx].Value = sc.Value
			changed = true
		}
		// Keep the mirrored XSRFToken in step: cookieHeaderFor falls back to
		// it and encodeSession copies it into Session.CSRF.
		if sc.Name == "XSRF-TOKEN" && out.XSRFToken != sc.Value {
			out.XSRFToken = sc.Value
			changed = true
		}
	}
	return out, changed
}
