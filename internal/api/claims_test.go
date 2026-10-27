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

	"github.com/aalejandrofer/grubdrops/internal/plan"
	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/runner"
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

// markEnv is an authed accounts env with the claim routes and one Twitch
// account whose runner plans b-cur (mining), b-next (Up next) and b-blk
// (Blocked).
func markEnv(t *testing.T) (*env, *calls, string) {
	t.Helper()
	e, c := accountsEnv(t)
	e.s.d.Claims = store.NewClaimLog(e.s.d.Q)
	e.s.routesClaims(e.s.protected)
	id := e.createAccount("twitch", "Alpha")
	camp := platform.Campaign{ID: "c1", Platform: "twitch", Game: "Rust", Name: "Rust drops"}
	tgt := func(b string) plan.Target {
		return plan.Target{Campaign: camp, Benefit: platform.DropBenefit{ID: b, Name: "name-" + b, ImageURL: "http://img/" + b}}
	}
	cur := tgt("b-cur")
	e.s.d.Pool = snapPool{snaps: []runner.Snapshot{{
		AccountID: id, Platform: "twitch", Current: &cur,
		UpNext:  []plan.Target{tgt("b-next")},
		Blocked: []plan.Blocked{{Target: tgt("b-blk"), Reason: plan.ReasonNotLinked}},
	}}}
	return e, c, id
}

func TestClaims_MarkCollected(t *testing.T) {
	e, c, id := markEnv(t)
	_, tok := e.session()
	before := len(c.changedList())
	for _, b := range []string{"b-cur", "b-next", "b-blk"} {
		resp, body := e.do("POST", "/api/claims", tok, map[string]string{"account_id": id, "benefit_id": b})
		require.Equal(t, http.StatusCreated, resp.StatusCode, string(body))
		var got claimItem
		unmarshal(t, body, &got)
		require.Equal(t, store.ClaimSourceManual, got.Source)
		require.Equal(t, "name-"+b, got.BenefitName)
		require.Equal(t, "Rust", got.Game)
		require.Equal(t, "Alpha", got.AccountName)
	}
	ids, err := e.s.d.Claims.ClaimedBenefitIDs(context.Background(), id)
	require.NoError(t, err)
	require.Equal(t, map[string]bool{"b-cur": true, "b-next": true, "b-blk": true}, ids)
	require.Len(t, c.changedList(), before+3, "each mark reloads the runner")
}

func TestClaims_MarkRejectsUnplanned(t *testing.T) {
	e, c, id := markEnv(t)
	_, tok := e.session()
	before := len(c.changedList())
	resp, body := e.do("POST", "/api/claims", tok, map[string]string{"account_id": id, "benefit_id": "nope"})
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
	require.Contains(t, string(body), "not_queued")
	resp, _ = e.do("POST", "/api/claims", tok, map[string]string{"account_id": "acc_missing", "benefit_id": "b-cur"})
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
	ids, err := e.s.d.Claims.ClaimedBenefitIDs(context.Background(), id)
	require.NoError(t, err)
	require.Empty(t, ids)
	require.Len(t, c.changedList(), before)
}

func TestClaims_Remove(t *testing.T) {
	e, c, id := markEnv(t)
	_, tok := e.session()
	require.NoError(t, e.s.d.Claims.Record(context.Background(), store.ClaimEntry{
		AccountID: id, Platform: "twitch", BenefitID: "a/b c", Source: store.ClaimSourceGrubDrops,
	}))
	before := len(c.changedList())
	path := "/api/claims/" + url.PathEscape(id) + "/" + url.PathEscape("a/b c")
	resp, body := e.do("DELETE", path, tok, nil)
	require.Equal(t, http.StatusNoContent, resp.StatusCode, string(body))
	ids, err := e.s.d.Claims.ClaimedBenefitIDs(context.Background(), id)
	require.NoError(t, err)
	require.Empty(t, ids)
	require.Equal(t, []string{id}, c.changedList()[before:])

	resp, _ = e.do("DELETE", path, tok, nil)
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestClaims_MarkNeedsCSRF(t *testing.T) {
	e, _, id := markEnv(t)
	resp, _ := e.do("POST", "/api/claims", "", map[string]string{"account_id": id, "benefit_id": "b-cur"})
	require.Equal(t, http.StatusForbidden, resp.StatusCode)
}
