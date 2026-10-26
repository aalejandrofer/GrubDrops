package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/store"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

func claimsEnv(t *testing.T) *env {
	t.Helper()
	e := newEnv(t)
	e.s.d.Claims = store.NewClaimLog(e.s.d.Q)
	e.s.routesClaims(e.s.protected)
	return e
}

func seedClaimAccount(t *testing.T, e *env, id, name string) {
	t.Helper()
	now := time.Now().Unix()
	_, err := e.s.d.Q.CreateAccount(context.Background(), gen.CreateAccountParams{
		ID: id, Platform: "twitch", DisplayName: name, Status: "idle",
		FingerprintJson: "{}", Enabled: 1, CreatedAt: now, UpdatedAt: now,
	})
	require.NoError(t, err)
}

func recordClaim(t *testing.T, e *env, acct, benefit, game string, at int64) {
	t.Helper()
	require.NoError(t, e.s.d.Claims.Record(context.Background(), store.ClaimEntry{
		AccountID: acct, Platform: "twitch", Game: game, CampaignName: "camp",
		BenefitID: benefit, BenefitName: "name-" + benefit, ImageURL: "http://img/" + benefit,
		Code: "code-" + benefit, Source: store.ClaimSourceGrubDrops, ClaimedAt: time.Unix(at, 0),
	}))
}

func loginAdmin(t *testing.T, e *env) {
	t.Helper()
	_, tok := e.session()
	resp, _ := e.do("POST", "/api/setup", tok, map[string]string{"password": "hunter2hunter2", "confirm": "hunter2hunter2"})
	require.Equal(t, 204, resp.StatusCode)
}

func TestClaims_HandlerShape(t *testing.T) {
	e := claimsEnv(t)
	loginAdmin(t, e)
	seedClaimAccount(t, e, "a1", "Alice")
	recordClaim(t, e, "a1", "b1", "Rust", 1_700_000_001)
	recordClaim(t, e, "a1", "b2", "Apex", 1_700_000_002)
	recordClaim(t, e, "a1", "b3", "Apex", 1_700_000_003)

	resp, b := e.do("GET", "/api/claims?limit=2", "", nil)
	require.Equal(t, 200, resp.StatusCode, string(b))
	var got map[string]any
	require.NoError(t, json.Unmarshal(b, &got))
	require.EqualValues(t, 3, got["total"])
	items := got["items"].([]any)
	require.Len(t, items, 2)
	item := items[0].(map[string]any)
	keys := []string{}
	for k := range item {
		keys = append(keys, k)
	}
	require.ElementsMatch(t, []string{"account_id", "account_name", "platform", "game", "campaign_name",
		"benefit_id", "benefit_name", "image_url", "code", "source", "claimed_at"}, keys)
	require.Equal(t, "Alice", item["account_name"])
	require.Equal(t, "b3", item["benefit_id"])
	require.Equal(t, "2023-11-14T22:13:23Z", item["claimed_at"])
	require.Equal(t, []any{"Apex", "Rust"}, got["games"])
	require.Equal(t, []any{map[string]any{"id": "a1", "name": "Alice"}}, got["accounts"])

	_, b = e.do("GET", "/api/claims?game=Rust&account=a1", "", nil)
	require.NoError(t, json.Unmarshal(b, &got))
	require.EqualValues(t, 1, got["total"])
}

func TestClaims_RemovedAccountKept(t *testing.T) {
	e := claimsEnv(t)
	loginAdmin(t, e)
	recordClaim(t, e, "gone", "b1", "Rust", 1_700_000_001)
	_, b := e.do("GET", "/api/claims", "", nil)
	var got struct {
		Items []map[string]any `json:"items"`
	}
	require.NoError(t, json.Unmarshal(b, &got))
	require.Len(t, got.Items, 1)
	require.Equal(t, "", got.Items[0]["account_name"])
	require.Equal(t, "gone", got.Items[0]["account_id"])
}

func TestClaims_BadLimit400(t *testing.T) {
	e := claimsEnv(t)
	loginAdmin(t, e)
	for _, q := range []string{"limit=abc", "offset=x", "limit=-1", "offset=-5"} {
		resp, b := e.do("GET", "/api/claims?"+q, "", nil)
		require.Equal(t, http.StatusBadRequest, resp.StatusCode, q)
		require.JSONEq(t, `{"error":"bad_request"}`, string(b))
	}
	// over-cap limit is clamped, not rejected
	resp, _ := e.do("GET", "/api/claims?limit=1000", "", nil)
	require.Equal(t, 200, resp.StatusCode)
}

func TestClaims_Query(t *testing.T) {
	e := claimsEnv(t)
	loginAdmin(t, e)
	seedClaimAccount(t, e, "a1", "Alice")
	recordClaim(t, e, "a1", "b1", "Rust", 1_700_000_001)
	recordClaim(t, e, "a1", "b2", "Apex", 1_700_000_002)
	get := func(qs string) (int, int) {
		_, b := e.do("GET", "/api/claims?"+qs, "", nil)
		var got struct {
			Total int   `json:"total"`
			Items []any `json:"items"`
		}
		require.NoError(t, json.Unmarshal(b, &got))
		return got.Total, len(got.Items)
	}
	total, n := get("q=" + url.QueryEscape("  NAME-B1 "))
	require.Equal(t, 1, total)
	require.Equal(t, 1, n)
	total, _ = get("q=apex&game=Rust")
	require.Equal(t, 0, total)
	total, _ = get("q=" + url.QueryEscape(strings.Repeat("x", 500)))
	require.Equal(t, 0, total)
	require.Equal(t, strings.Repeat("é", 100), claimQuery(" "+strings.Repeat("é", 150)))
	require.Equal(t, strings.Repeat("a", 99), claimQuery(strings.Repeat("a", 99)+" b"))
}
