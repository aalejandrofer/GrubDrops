package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/platform/platformtest"
	"github.com/aalejandrofer/grubdrops/internal/platform/twitch"
)

// androidBackend scripts AndroidLogin on top of platformtest.Fake: "good"
// pastes resolve to user 42 (alice), "other" to user 7 (bob).
type androidBackend struct {
	*platformtest.Fake
}

func (b *androidBackend) Name() string { return "twitch" }

func (b *androidBackend) AndroidLogin(_ context.Context, raw string) (platform.Session, string, error) {
	switch raw {
	case "good", "abcdefghijklmnopqrstuvwxyz0123":
		return platform.Session{AccessToken: "tok-a", ClientID: twitch.ClientAndroid, UserID: "42", ExpiresAt: time.Now().Add(time.Hour)}, "alice", nil
	case "other":
		return platform.Session{AccessToken: "tok-b", ClientID: twitch.ClientAndroid, UserID: "7", ExpiresAt: time.Now().Add(time.Hour)}, "bob", nil
	case "tv":
		return platform.Session{}, "", twitch.ErrWrongClient
	case "dead":
		return platform.Session{}, "", twitch.ErrTokenInvalid
	}
	return platform.Session{}, "", twitch.ErrNoAccessToken
}

func TestTwitchAndroid_LoginURL(t *testing.T) {
	e, _ := accountsEnv(t, &androidBackend{Fake: platformtest.NewFake()})
	resp, b := e.do("GET", "/api/login/twitch/android", "", nil)
	require.Equal(t, 200, resp.StatusCode, string(b))
	assert.Equal(t, twitch.AndroidLoginURL(), jsonField(t, b, "login_url"))
}

func TestTwitchAndroid_CreatesThenUpdatesSameUser(t *testing.T) {
	e, c := accountsEnv(t, &androidBackend{Fake: platformtest.NewFake()})
	_, tok := e.session()
	ctx := context.Background()

	resp, b := e.do("POST", "/api/login/twitch/android", tok, map[string]string{"url": "good"})
	require.Equal(t, 200, resp.StatusCode, string(b))
	var out struct {
		ID      string `json:"id"`
		Login   string `json:"login"`
		Created bool   `json:"created"`
	}
	require.NoError(t, json.Unmarshal(b, &out))
	assert.True(t, out.Created)
	assert.Equal(t, "alice", out.Login)
	acc, err := e.s.d.Q.GetAccount(ctx, out.ID)
	require.NoError(t, err)
	assert.Equal(t, "twitch", acc.Platform)
	assert.Equal(t, "alice", acc.DisplayName)
	sess, ok, err := e.s.d.Sessions.Get(ctx, out.ID)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, twitch.ClientAndroid, sess.ClientID)
	assert.Equal(t, "42", sess.UserID)
	assert.Equal(t, []string{out.ID}, c.changedList())

	// Same Twitch user again: the existing account is updated, no new one.
	resp, b = e.do("POST", "/api/login/twitch/android", tok, map[string]string{"url": "good"})
	require.Equal(t, 200, resp.StatusCode, string(b))
	assert.Equal(t, out.ID, jsonField(t, b, "id"))
	var again struct{ Created bool }
	require.NoError(t, json.Unmarshal(b, &again))
	assert.False(t, again.Created)

	// A different user gets their own account.
	resp, b = e.do("POST", "/api/login/twitch/android", tok, map[string]string{"url": "other"})
	require.Equal(t, 200, resp.StatusCode, string(b))
	assert.NotEqual(t, out.ID, jsonField(t, b, "id"))

	accs, err := e.s.d.Q.ListAllAccounts(ctx)
	require.NoError(t, err)
	assert.Len(t, accs, 2)
}

func TestTwitchAndroid_ExistingAccountRelogin(t *testing.T) {
	e, _ := accountsEnv(t, &androidBackend{Fake: platformtest.NewFake()})
	id := e.createAccount("twitch", "Mine")
	_, tok := e.session()
	resp, b := e.do("POST", "/api/login/twitch/android", tok, map[string]string{"url": "good", "account_id": id})
	require.Equal(t, 200, resp.StatusCode, string(b))
	assert.Equal(t, id, jsonField(t, b, "id"))
	sess, ok, err := e.s.d.Sessions.Get(context.Background(), id)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "tok-a", sess.AccessToken)
	acc, err := e.s.d.Q.GetAccount(context.Background(), id)
	require.NoError(t, err)
	assert.Equal(t, "Mine", acc.DisplayName, "relogin keeps the name")
}

func TestTwitchAndroid_Errors(t *testing.T) {
	e, c := accountsEnv(t, &androidBackend{Fake: platformtest.NewFake()})
	kid := e.createAccount("kick", "K")
	c.changed = nil
	_, tok := e.session()

	resp, _ := e.do("POST", "/api/login/twitch/android", "", map[string]string{"url": "good"})
	assert.Equal(t, 403, resp.StatusCode, "no CSRF token")

	for raw, code := range map[string]string{
		"garbage": "android_no_token",
		"tv":      "android_wrong_client",
		"dead":    "android_invalid_token",
	} {
		resp, b := e.do("POST", "/api/login/twitch/android", tok, map[string]string{"url": raw})
		assert.Equal(t, 400, resp.StatusCode, raw)
		assert.Equal(t, code, jsonField(t, b, "error"), raw)
	}

	resp, _ = e.do("POST", "/api/login/twitch/android", tok, map[string]any{"url": 5})
	assert.Equal(t, 400, resp.StatusCode, "malformed body")
	resp, _ = e.do("POST", "/api/login/twitch/android", tok, map[string]string{"url": "good", "account_id": kid})
	assert.Equal(t, 400, resp.StatusCode, "kick account")
	resp, _ = e.do("POST", "/api/login/twitch/android", tok, map[string]string{"url": "good", "account_id": "acc_missing"})
	assert.Equal(t, 404, resp.StatusCode)

	accs, err := e.s.d.Q.ListAllAccounts(context.Background())
	require.NoError(t, err)
	assert.Len(t, accs, 1, "failed logins create nothing")
	assert.Empty(t, c.changedList())
}

func TestTwitchAndroid_TDMJar(t *testing.T) {
	e, _ := accountsEnv(t, &androidBackend{Fake: platformtest.NewFake()})
	_, tok := e.session()
	jar := `{"www.twitch.tv|/": {"auth-token": {"key": "auth-token", "value": "abcdefghijklmnopqrstuvwxyz0123"}}}`
	resp, b := e.do("POST", "/api/login/twitch/android", tok, map[string]string{"jar": base64.StdEncoding.EncodeToString([]byte(jar))})
	require.Equal(t, 200, resp.StatusCode, string(b))
	assert.Equal(t, "alice", jsonField(t, b, "login"))

	for name, in := range map[string]string{
		"not base64": "%%%",
		"no token":   base64.StdEncoding.EncodeToString([]byte(`{"www.twitch.tv|/": {}}`)),
	} {
		resp, b := e.do("POST", "/api/login/twitch/android", tok, map[string]string{"jar": in})
		assert.Equal(t, 400, resp.StatusCode, name)
		assert.Contains(t, string(b), "tdm_no_token", name)
	}
}
