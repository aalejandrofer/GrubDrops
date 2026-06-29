package api

import (
	"bytes"
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/store"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
	"github.com/aalejandrofer/grubdrops/internal/web"
)

// collectTestKey is an age secret key used only in tests.
const collectTestKey = "AGE-SECRET-KEY-1DZCAXYWJM6M42NSX5GR4QWZZ2JXEYKJ9ZKWYFYSNU997775JJ6XSY85FK9"

func platformSessionFixture() platform.Session {
	return platform.Session{AccessToken: "tok"}
}

// TestSessionForPlatform_FallsBackToDisabledAccount proves a drop collected on
// a now-disabled account is still serviceable: sessionForPlatform must return a
// session from a DISABLED account when no enabled account on the platform has
// one, so lazyFetchBenefits can populate the items panel.
func TestSessionForPlatform_FallsBackToDisabledAccount(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	q := gen.New(db)

	c, err := store.NewCryptor(collectTestKey)
	require.NoError(t, err)
	sessions := store.NewSessionStore(db, q, c)

	now := time.Now().Unix()
	_, err = q.CreateAccount(ctx, gen.CreateAccountParams{
		ID: "acc-off", Platform: "twitch", DisplayName: "Phluses",
		Status: "idle", FingerprintJson: "{}", Enabled: 0, // DISABLED
		CreatedAt: now, UpdatedAt: now,
	})
	require.NoError(t, err)
	// Give the disabled account a stored session.
	require.NoError(t, sessions.Put(ctx, "acc-off", platformSessionFixture()))

	d := &dropsDeps{q: q, sessions: sessions}
	sess, ok := d.sessionForPlatform(ctx, "twitch")
	require.True(t, ok, "must fall back to the disabled account's session")
	require.Equal(t, "acc-off", sess.AccountID)
}

func testRenderer(t *testing.T) Renderer {
	t.Helper()
	tmpl, err := web.Templates()
	require.NoError(t, err)
	return tmpl
}

// seedCampaignWithBenefit inserts a campaign + one benefit for collect tests.
func seedCampaignWithBenefit(t *testing.T, ctx context.Context, q *gen.Queries, campID, benID, plat string) {
	t.Helper()
	now := time.Now().Unix()
	require.NoError(t, q.UpsertCampaign(ctx, gen.UpsertCampaignParams{
		ID: campID, Platform: plat, Game: "Minecraft", Name: "MC Drop",
		StartsAt: now - 60, EndsAt: now + 3600, Status: "active",
		RawJson: "{}", DiscoveredAt: now, Kind: "drop",
		AccountLinked: 1, AccountLinkUrl: "",
	}))
	require.NoError(t, q.UpsertBenefit(ctx, gen.UpsertBenefitParams{
		ID: benID, CampaignID: campID, Name: "Builder Cape", RequiredMinutes: 5, ImageUrl: "",
	}))
}

func TestAddClaim_WritesClaimAndOverride(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	q := gen.New(db)

	now := time.Now().Unix()
	_, err = q.CreateAccount(ctx, gen.CreateAccountParams{
		ID: "acc-1", Platform: "twitch", DisplayName: "TTik3r",
		Status: "idle", FingerprintJson: "{}", Enabled: 1, CreatedAt: now, UpdatedAt: now,
	})
	require.NoError(t, err)
	seedCampaignWithBenefit(t, ctx, q, "camp-1", "ben-1", "twitch")

	d := &dropsDeps{q: q, t: testRenderer(t), loc: time.UTC}

	form := url.Values{}
	form.Set("account_id", "acc-1")
	form.Set("benefit_id", "ben-1")
	form.Set("campaign_id", "camp-1")
	req := httptest.NewRequest(http.MethodPost, "/drops/claim/add", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	d.addClaim(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	// Claim row exists.
	n, err := q.CountClaimsFor(ctx, gen.CountClaimsForParams{AccountID: "acc-1", BenefitID: "ben-1"})
	require.NoError(t, err)
	require.Equal(t, int64(1), n)
	// Override key exists.
	rows, err := q.ListKVByPrefix(ctx, sql.NullString{String: store.CollectOverridePrefix, Valid: true})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, store.CollectOverridePrefix+"ben-1:acc-1", rows[0].Key)
}

func TestRemoveClaim_DeletesClaimAndOverride(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	q := gen.New(db)

	now := time.Now().Unix()
	_, err = q.CreateAccount(ctx, gen.CreateAccountParams{
		ID: "acc-1", Platform: "twitch", DisplayName: "TTik3r",
		Status: "idle", FingerprintJson: "{}", Enabled: 1, CreatedAt: now, UpdatedAt: now,
	})
	require.NoError(t, err)
	seedCampaignWithBenefit(t, ctx, q, "camp-1", "ben-1", "twitch")
	require.NoError(t, q.InsertClaim(ctx, gen.InsertClaimParams{
		ID: store.NewClaimID(), AccountID: "acc-1", BenefitID: "ben-1",
		ClaimedAt: now, ValueMetaJson: `{"manual":true}`,
	}))
	require.NoError(t, q.UpsertSettingString(ctx, gen.UpsertSettingStringParams{
		Key: store.CollectOverridePrefix + "ben-1:acc-1", Value: []byte("1"),
	}))

	d := &dropsDeps{q: q, t: testRenderer(t), loc: time.UTC}
	form := url.Values{}
	form.Set("account_id", "acc-1")
	form.Set("benefit_id", "ben-1")
	form.Set("campaign_id", "camp-1")
	req := httptest.NewRequest(http.MethodPost, "/drops/claim/remove", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	d.removeClaim(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	n, err := q.CountClaimsFor(ctx, gen.CountClaimsForParams{AccountID: "acc-1", BenefitID: "ben-1"})
	require.NoError(t, err)
	require.Equal(t, int64(0), n)
	rows, err := q.ListKVByPrefix(ctx, sql.NullString{String: store.CollectOverridePrefix, Valid: true})
	require.NoError(t, err)
	require.Empty(t, rows, "uncollecting must clear the protection override")
}

func TestAddableAccounts_ExcludesCollectedAndCrossPlatform(t *testing.T) {
	accs := []gen.Account{
		{ID: "a1", Platform: "twitch", DisplayName: "TTik3r", Enabled: 1},
		{ID: "a2", Platform: "twitch", DisplayName: "Phluses", Enabled: 0}, // disabled still offered
		{ID: "a3", Platform: "kick", DisplayName: "KickOnly", Enabled: 1},  // wrong platform
	}
	collected := []collectedMark{{AccountID: "a1", BenefitID: "ben-1"}}

	got := addableAccounts(accs, "twitch", collected)

	if len(got) != 1 {
		t.Fatalf("got %d addable, want 1 (a2 only)", len(got))
	}
	if got[0].AccountID != "a2" {
		t.Fatalf("addable = %q, want a2", got[0].AccountID)
	}
}

func TestCampaignItems_AddMenuListsEligibleAccounts(t *testing.T) {
	detail := campaignDetailRow{
		ID: "camp-1", Platform: "twitch", CSRFToken: "csrf",
		Benefits: []campaignBenefitRow{{
			Name: "Builder Cape", RequiredMinutes: 5, BenefitID: "ben-1",
			Collected: nil,
			Addable:   []addableAccount{{Login: "TTik3r", Platform: "twitch", AccountID: "a1"}},
		}},
	}
	out := renderCampaignItems_render(t, detail)
	if !strings.Contains(out, `hx-post="/drops/claim/add"`) {
		t.Errorf("add menu missing the claim/add post")
	}
	if !strings.Contains(out, `"benefit_id":"ben-1"`) {
		t.Errorf("add option must carry the benefit id")
	}
	if !strings.Contains(out, ">TTik3r<") {
		t.Errorf("add menu must list the eligible account")
	}
}

// renderCampaignItems_render executes the items partial directly.
func renderCampaignItems_render(t *testing.T, detail campaignDetailRow) string {
	t.Helper()
	tmpl, err := web.Templates()
	require.NoError(t, err)
	var buf bytes.Buffer
	require.NoError(t, tmpl.ExecuteTemplate(&buf, "drops_campaign_items", detail))
	return buf.String()
}
