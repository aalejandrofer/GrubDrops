package twitch

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/nlpodyssey/gopickle/pickle"
	"github.com/nlpodyssey/gopickle/types"
)

// TwitchDropsMiner (DevilXD) keeps its Android session in cookies.jar, an
// aiohttp CookieJar. Newer aiohttp writes JSON keyed "domain|path"; older
// aiohttp pickles a defaultdict of http.cookies.SimpleCookie. The access token
// is the auth-token cookie.

const tdmTokenCookie = "auth-token"

// TokenFromTDMJar returns the auth-token from a TwitchDropsMiner cookies.jar.
// ErrNoAccessToken when the file holds none. The error never contains the token.
func TokenFromTDMJar(b []byte) (string, error) {
	if t := bytes.TrimSpace(b); len(t) > 0 && t[0] == '{' {
		return tdmJSONToken(t)
	}
	return tdmPickleToken(b)
}

func tdmJSONToken(b []byte) (string, error) {
	var jar map[string]map[string]struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal(b, &jar); err != nil {
		return "", ErrNoAccessToken
	}
	for key, cookies := range jar {
		domain, _, _ := strings.Cut(key, "|")
		if c, ok := cookies[tdmTokenCookie]; ok && twitchDomain(domain) && c.Value != "" {
			return c.Value, nil
		}
	}
	return "", ErrNoAccessToken
}

// tdmPickleToken reads the pickle without running code: only the three
// classes a CookieJar pickle holds are allowed, each rebuilt as a plain dict.
func tdmPickleToken(b []byte) (string, error) {
	u := pickle.NewUnpickler(bytes.NewReader(b))
	u.FindClass = func(module, name string) (any, error) {
		switch module + "." + name {
		case "collections.defaultdict", "http.cookies.SimpleCookie", "http.cookies.Morsel":
			return pyDictClass{}, nil
		}
		return nil, fmt.Errorf("cookies.jar: unexpected class %s.%s", module, name)
	}
	root, err := u.Load()
	if err != nil {
		return "", ErrNoAccessToken
	}
	jar, ok := root.(*pyDict)
	if !ok {
		return "", ErrNoAccessToken
	}
	for _, e := range *jar.items {
		// The key is a (domain, path) tuple, or the bare domain on old aiohttp.
		var domain string
		switch k := e.Key.(type) {
		case string:
			domain = k
		case *types.Tuple:
			if k.Len() > 0 {
				domain, _ = k.Get(0).(string)
			}
		}
		cookies, ok := e.Value.(*pyDict)
		if !ok || !twitchDomain(domain) {
			continue
		}
		m, ok := cookies.items.Get(tdmTokenCookie)
		morsel, ok2 := m.(*pyDict)
		if !ok || !ok2 || morsel.state == nil {
			continue
		}
		if v, _ := morsel.state.Get("value"); v != nil {
			if s, _ := v.(string); s != "" {
				return s, nil
			}
		}
	}
	return "", ErrNoAccessToken
}

func twitchDomain(d string) bool {
	d = strings.TrimPrefix(d, ".")
	return d == "twitch.tv" || strings.HasSuffix(d, ".twitch.tv")
}

// pyDictClass stands in for defaultdict, SimpleCookie and Morsel: all three
// are dict subclasses. Called through REDUCE (defaultdict) or NEWOBJ.
type pyDictClass struct{}

func (pyDictClass) Call(...any) (any, error)  { return newPyDict(), nil }
func (pyDictClass) PyNew(...any) (any, error) { return newPyDict(), nil }

// pyDict holds dict items and, for a Morsel, the __setstate__ dict with its
// key and value.
type pyDict struct {
	items *types.Dict
	state *types.Dict
}

func newPyDict() *pyDict { return &pyDict{items: types.NewDict()} }

func (d *pyDict) Set(key, value any) { d.items.Set(key, value) }

func (d *pyDict) PySetState(state any) error {
	s, ok := state.(*types.Dict)
	if !ok {
		return fmt.Errorf("cookies.jar: unexpected state %T", state)
	}
	d.state = s
	return nil
}
