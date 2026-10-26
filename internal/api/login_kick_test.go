package api

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

const kickCookies = "# Netscape HTTP Cookie File\n" +
	".kick.com\tTRUE\t/\tTRUE\t1781000000\tkick_session\tsess-val\n" +
	"#HttpOnly_.kick.com\tTRUE\t/\tTRUE\t1781000000\tsession_token\ttok-val\n" +
	".kick.com\tTRUE\t/\tTRUE\t1781000000\tXSRF-TOKEN\txsrf-val\n"

type fakeRegistrar struct {
	mu       sync.Mutex
	verifyOK bool
	channels map[string][]string
}

func (f *fakeRegistrar) RegisterChannels(id string, ch []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.channels == nil {
		f.channels = map[string][]string{}
	}
	f.channels[id] = ch
}

func (f *fakeRegistrar) VerifyAuth(context.Context, platform.Session) error {
	if f.verifyOK {
		return nil
	}
	return errors.New("bad session")
}

func TestKickSession_StoresAndRegisters(t *testing.T) {
	e, c := accountsEnv(t)
	reg := &fakeRegistrar{verifyOK: true}
	e.s.d.Registrar = reg
	id := e.createAccount("kick", "K")
	c.changed = nil
	_, tok := e.session()
	path := "/api/accounts/" + id + "/kick/session"

	resp, b := e.do("POST", path, tok, map[string]any{"cookies_txt": kickCookies, "channels": []string{"foo", " Bar ", "FOO", ""}})
	require.Equal(t, 200, resp.StatusCode, string(b))
	assert.JSONEq(t, `{"verified":true}`, string(b))
	assert.Equal(t, []string{"foo", "Bar"}, reg.channels[id])
	_, ok, err := e.s.d.Sessions.Get(context.Background(), id)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, []string{id}, c.changedList())

	reg.verifyOK = false
	resp, b = e.do("POST", path, tok, map[string]any{"cookies_txt": kickCookies})
	require.Equal(t, 200, resp.StatusCode)
	assert.JSONEq(t, `{"verified":false}`, string(b))

	noSess := ".kick.com\tTRUE\t/\tTRUE\t1\tXSRF-TOKEN\tx\n"
	resp, _ = e.do("POST", path, tok, map[string]any{"cookies_txt": noSess})
	assert.Equal(t, 400, resp.StatusCode)

	tid := e.createAccount("twitch", "T")
	resp, _ = e.do("POST", "/api/accounts/"+tid+"/kick/session", tok, map[string]any{"cookies_txt": kickCookies})
	assert.Equal(t, 400, resp.StatusCode)
}
