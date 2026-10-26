package api

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAccounts_Order(t *testing.T) {
	e, c := accountsEnv(t)
	e.s.d.Hub = NewHub()
	ch, cancel := e.s.d.Hub.Subscribe()
	defer cancel()
	a := e.createAccount("twitch", "Alpha")
	b := e.createAccount("kick", "Beta")
	d := e.createAccount("kick", "Delta")
	_, tok := e.session()
	before := len(c.changedList())

	listed := func() []string {
		t.Helper()
		_, body := e.do("GET", "/api/accounts", "", nil)
		var list []struct {
			ID string `json:"id"`
		}
		require.NoError(t, json.Unmarshal(body, &list))
		ids := make([]string, 0, len(list))
		for _, x := range list {
			ids = append(ids, x.ID)
		}
		return ids
	}
	require.Equal(t, []string{a, b, d}, listed(), "new accounts go last")

	resp, _ := e.do("PUT", "/api/accounts/order", tok, map[string]any{"ids": []string{d, a, b}})
	require.Equal(t, 204, resp.StatusCode)
	assert.Equal(t, []string{d, a, b}, listed())
	assert.Len(t, c.changedList(), before, "reorder must not reload runners")
	select {
	case ev := <-ch:
		assert.Equal(t, EventAccounts, ev.Name)
	case <-time.After(time.Second):
		t.Fatal("no accounts event after reorder")
	}

	for name, ids := range map[string]any{
		"unknown":    []string{d, a, "ghost"},
		"duplicate":  []string{d, a, a},
		"incomplete": []string{d, a},
		"extra":      []string{d, a, b, "ghost"},
		"empty":      []string{},
		"missing":    nil,
	} {
		resp, _ = e.do("PUT", "/api/accounts/order", tok, map[string]any{"ids": ids})
		assert.Equal(t, 400, resp.StatusCode, name)
	}
	assert.Equal(t, []string{d, a, b}, listed(), "a rejected order changes nothing")

	resp, _ = e.do("PUT", "/api/accounts/order", "", map[string]any{"ids": []string{a, b, d}})
	assert.Equal(t, 403, resp.StatusCode, "CSRF token required")
}
