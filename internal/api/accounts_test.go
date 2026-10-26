package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/runner"
	"github.com/aalejandrofer/grubdrops/internal/store"
)

const testAgeKey = "AGE-SECRET-KEY-1DZCAXYWJM6M42NSX5GR4QWZZ2JXEYKJ9ZKWYFYSNU997775JJ6XSY85FK9"

type snapPool struct{ snaps []runner.Snapshot }

func (p snapPool) Snapshots() []runner.Snapshot   { return p.snaps }
func (snapPool) SetOrder(string, []string)        {}
func (snapPool) ResetOrder(string)                {}
func (snapPool) OrderFunc(string) func() []string { return nil }

// calls records AccountChanged / AccountRemoved invocations.
type calls struct {
	mu      sync.Mutex
	changed []string
	removed []string
}

func (c *calls) onChanged(id string) { c.mu.Lock(); c.changed = append(c.changed, id); c.mu.Unlock() }
func (c *calls) onRemoved(id string) { c.mu.Lock(); c.removed = append(c.removed, id); c.mu.Unlock() }
func (c *calls) changedList() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.changed...)
}

// accountsEnv is an authed env with the accounts deps wired.
func accountsEnv(t *testing.T, backends ...platform.Backend) (*env, *calls) {
	t.Helper()
	e := newEnv(t)
	cr, err := store.NewCryptor(testAgeKey)
	require.NoError(t, err)
	reg := platform.NewRegistry()
	for _, b := range backends {
		reg.Register(b)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	c := &calls{}
	e.s.d.Sessions = store.NewSessionStore(e.s.d.DB, e.s.d.Q, cr)
	e.s.d.Registry = reg
	e.s.d.RootCtx = ctx
	e.s.d.AccountChanged = c.onChanged
	e.s.d.AccountRemoved = c.onRemoved
	e.s.routesAccounts(e.s.protected)
	e.setup("password123")
	return e, c
}

func (e *env) createAccount(plat, name string) string {
	e.t.Helper()
	_, tok := e.session()
	resp, b := e.do("POST", "/api/accounts", tok, map[string]string{"platform": plat, "name": name})
	require.Equal(e.t, 201, resp.StatusCode, string(b))
	return jsonField(e.t, b, "id")
}

func TestAccounts_CreateDefaultsName(t *testing.T) {
	e, c := accountsEnv(t)
	id := e.createAccount("twitch", "")
	kid := e.createAccount("kick", "")
	assert.Equal(t, []string{id, kid}, c.changedList())
	a, err := e.s.d.Q.GetAccount(context.Background(), id)
	require.NoError(t, err)
	assert.Equal(t, "Twitch account", a.DisplayName)
	k, err := e.s.d.Q.GetAccount(context.Background(), kid)
	require.NoError(t, err)
	assert.Equal(t, "Kick account", k.DisplayName)
}

func TestAccounts_CreateBadPlatform400(t *testing.T) {
	e, _ := accountsEnv(t)
	_, tok := e.session()
	resp, _ := e.do("POST", "/api/accounts", tok, map[string]string{"platform": "youtube"})
	assert.Equal(t, 400, resp.StatusCode)
}

func TestAccounts_ListStateAndNeedsLogin(t *testing.T) {
	e, _ := accountsEnv(t)
	a := e.createAccount("twitch", "Alpha")
	b := e.createAccount("kick", "Beta")
	require.NoError(t, e.s.d.Sessions.Put(context.Background(), a, platform.Session{AccessToken: "x", ExpiresAt: time.Now().Add(time.Hour)}))
	e.s.d.Pool = snapPool{snaps: []runner.Snapshot{{AccountID: a, State: runner.StateWatching}}}

	resp, body := e.do("GET", "/api/accounts", "", nil)
	require.Equal(t, 200, resp.StatusCode)
	var list []struct {
		ID         string `json:"id"`
		State      string `json:"state"`
		NeedsLogin bool   `json:"needs_login"`
		Name       string `json:"name"`
	}
	unmarshal(t, body, &list)
	require.Len(t, list, 2)
	byID := map[string]int{list[0].ID: 0, list[1].ID: 1}
	la, lb := list[byID[a]], list[byID[b]]
	assert.Equal(t, "watching", la.State)
	assert.False(t, la.NeedsLogin)
	assert.Equal(t, "idle", lb.State)
	assert.True(t, lb.NeedsLogin)

	e.s.d.Pool = snapPool{snaps: []runner.Snapshot{{AccountID: a, State: runner.StateNeedsLogin}}}
	_, body = e.do("GET", "/api/accounts", "", nil)
	unmarshal(t, body, &list)
	for _, it := range list {
		assert.True(t, it.NeedsLogin, it.ID)
	}
}

func TestAccounts_DeleteStopsAndKeepsHistory(t *testing.T) {
	e, c := accountsEnv(t)
	id := e.createAccount("twitch", "Alpha")
	ctx := context.Background()
	require.NoError(t, e.s.d.Sessions.Put(ctx, id, platform.Session{AccessToken: "x", ExpiresAt: time.Now().Add(time.Hour)}))
	_, err := e.s.d.DB.Exec(`INSERT INTO claim_log (account_id, benefit_id, platform, source, claimed_at) VALUES (?, 'b1', 'twitch', 'runner', 1)`, id)
	require.NoError(t, err)

	_, tok := e.session()
	resp, _ := e.do("DELETE", "/api/accounts/"+id, tok, nil)
	require.Equal(t, 204, resp.StatusCode)

	_, err = e.s.d.Q.GetAccount(ctx, id)
	assert.ErrorIs(t, err, sql.ErrNoRows)
	_, ok, err := e.s.d.Sessions.Get(ctx, id)
	require.NoError(t, err)
	assert.False(t, ok)
	var n int
	require.NoError(t, e.s.d.DB.QueryRow(`SELECT COUNT(*) FROM claim_log WHERE account_id = ?`, id).Scan(&n))
	assert.Equal(t, 1, n)
	assert.Equal(t, []string{id}, c.removed)

	resp, _ = e.do("DELETE", "/api/accounts/"+id, tok, nil)
	assert.Equal(t, 404, resp.StatusCode)
}

func TestAccounts_Rename(t *testing.T) {
	e, c := accountsEnv(t)
	renamed := map[string]string{}
	e.s.d.AccountRenamed = func(id, name string) { renamed[id] = name }
	id := e.createAccount("kick", "Old")
	_, tok := e.session()
	resp, _ := e.do("PATCH", "/api/accounts/"+id, tok, map[string]string{"name": " New "})
	require.Equal(t, 204, resp.StatusCode)
	assert.Equal(t, map[string]string{id: "New"}, renamed)
	a, err := e.s.d.Q.GetAccount(context.Background(), id)
	require.NoError(t, err)
	assert.Equal(t, "New", a.DisplayName)
	assert.Equal(t, []string{id}, c.changedList(), "rename must not fire AccountChanged")

	resp, _ = e.do("PATCH", "/api/accounts/"+id, tok, map[string]string{"name": " "})
	assert.Equal(t, 400, resp.StatusCode)
	resp, _ = e.do("PATCH", "/api/accounts/nope", tok, map[string]string{"name": "X"})
	assert.Equal(t, 404, resp.StatusCode)
}

func jsonField(t *testing.T, b []byte, key string) string {
	t.Helper()
	var m map[string]any
	unmarshal(t, b, &m)
	s, _ := m[key].(string)
	return s
}

func unmarshal(t *testing.T, b []byte, v any) {
	t.Helper()
	require.NoError(t, json.Unmarshal(b, v), string(b))
}

func TestAccounts_ListAuthCheckedAtRFC3339(t *testing.T) {
	e, _ := accountsEnv(t)
	id := e.createAccount("twitch", "A")
	e.createAccount("kick", "B")
	_, err := e.s.d.DB.Exec(`INSERT INTO kv (key, value) VALUES (?, ?)`, "authcheck:"+id, []byte(`{"ok":true,"checked_at":1700000000,"msg":"fine"}`))
	require.NoError(t, err)
	_, body := e.do("GET", "/api/accounts", "", nil)
	var list []struct {
		ID   string `json:"id"`
		Auth *struct {
			OK        bool    `json:"ok"`
			Detail    string  `json:"detail"`
			CheckedAt *string `json:"checked_at"`
		} `json:"auth"`
	}
	unmarshal(t, body, &list)
	for _, it := range list {
		if it.ID != id {
			assert.Nil(t, it.Auth)
			continue
		}
		require.NotNil(t, it.Auth)
		assert.Equal(t, "2023-11-14T22:13:20Z", *it.Auth.CheckedAt)
		assert.Equal(t, "fine", it.Auth.Detail)
	}
}
