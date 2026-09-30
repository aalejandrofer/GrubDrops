package dropstate

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var t0 = time.Unix(1_000_000, 0)

func base() Row {
	return Row{AccountID: "a", DropID: "d", CampaignID: "c", Platform: "twitch", Source: FromPlatform}
}

func row(s Status, r Reason, min, req int) Row {
	x := base()
	x.Status, x.Reason, x.Minutes, x.Required = s, r, min, req
	return x
}

func TestApply(t *testing.T) {
	userClaimed := row(Claimed, NoReason, 0, 60)
	userClaimed.Source = FromUser
	notEnrolled := row(Blocked, NotEnrolled, 10, 60)
	notEnrolled.RetryAfter = t0.Add(time.Hour)
	skipped := row(Blocked, UserSkip, 0, 60)
	skipped.Source = FromUser
	needsLink := row(Blocked, NeedsLink, 60, 60)
	needsLink.RetryAfter = t0.Add(RetryNeedsLink)

	subOnly := row(Blocked, SubOnly, 0, 60)
	subOnly.RetryAfter = t0.Add(RetrySubOnly)

	cases := []struct {
		name   string
		prev   Row
		obs    Observation
		at     time.Time
		want   Status
		reason Reason
		src    Source
	}{
		{"new drop, platform silent -> eligible", base(), Observation{Required: 60}, t0, Eligible, NoReason, FromPlatform},
		{"new drop, platform silent, zero required -> sub_only", base(), Observation{}, t0, Blocked, SubOnly, FromPlatform},
		{"known progress -> accruing", row(Eligible, NoReason, 0, 60), Observation{Known: true, Minutes: 10, Required: 60}, t0, Accruing, NoReason, FromPlatform},
		{"known full -> claimable", row(Accruing, NoReason, 50, 60), Observation{Known: true, Minutes: 60, Required: 60}, t0, Claimable, NoReason, FromPlatform},
		{"known claimed -> claimed", row(Accruing, NoReason, 10, 60), Observation{Known: true, Claimed: true, Minutes: 60, Required: 60}, t0, Claimed, NoReason, FromPlatform},
		{"platform claimed never demoted", row(Claimed, NoReason, 60, 60), Observation{Known: true, Minutes: 0, Required: 60}, t0, Claimed, NoReason, FromPlatform},
		{"user mark kept while platform silent", userClaimed, Observation{Required: 60}, t0, Claimed, NoReason, FromUser},
		{"user mark overwritten by definite platform answer", userClaimed, Observation{Known: true, Minutes: 5, Required: 60}, t0, Accruing, NoReason, FromPlatform},
		{"accruing then platform silent -> not_enrolled", row(Accruing, NoReason, 10, 60), Observation{Required: 60}, t0, Blocked, NotEnrolled, FromPlatform},
		{"not_enrolled held inside window", notEnrolled, Observation{Required: 60}, t0, Blocked, NotEnrolled, FromPlatform},
		{"not_enrolled re-derived after window", notEnrolled, Observation{Required: 60}, t0.Add(2 * time.Hour), Accruing, NoReason, FromPlatform},
		{"unmineable placeholder -> not_enrolled", base(), Observation{Known: true, Unmineable: true}, t0, Blocked, NotEnrolled, FromPlatform},
		{"user skip survives progress", skipped, Observation{Known: true, Minutes: 30, Required: 60}, t0, Blocked, UserSkip, FromUser},
		{"claimed beats user skip", skipped, Observation{Known: true, Claimed: true, Minutes: 60, Required: 60}, t0, Claimed, NoReason, FromPlatform},
		{"needs_link held inside window", needsLink, Observation{Known: true, Minutes: 60, Required: 60}, t0, Blocked, NeedsLink, FromPlatform},
		{"needs_link re-derived after window", needsLink, Observation{Known: true, Minutes: 60, Required: 60}, t0.Add(25 * time.Hour), Claimable, NoReason, FromPlatform},
		{"sub_only held inside window", subOnly, Observation{Required: 60}, t0, Blocked, SubOnly, FromPlatform},
		{"sub_only re-derived after window", subOnly, Observation{Required: 60}, t0.Add(25 * time.Hour), Eligible, NoReason, FromPlatform},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Apply(tc.prev, tc.obs, tc.at)
			assert.Equal(t, tc.want, got.Status)
			assert.Equal(t, tc.reason, got.Reason)
			assert.Equal(t, tc.src, got.Source)
			assert.Equal(t, "d", got.DropID, "identity preserved")
		})
	}
}

func TestApply_PlatformClaimedKeepsHighestMinutes(t *testing.T) {
	got := Apply(row(Claimed, NoReason, 60, 60), Observation{Known: true, Minutes: 0, Required: 60}, t0)
	assert.Equal(t, 60, got.Minutes)
}

func TestApply_KeepsClaimBackoff(t *testing.T) {
	r := ClaimFailedAttempt(row(Claimable, NoReason, 60, 60), t0)
	got := Apply(r, Observation{Known: true, Minutes: 60, Required: 60}, t0.Add(10*time.Second))
	assert.Equal(t, Claimable, got.Status)
	assert.Equal(t, r.RetryAfter, got.RetryAfter, "sync must not reset claim backoff")
	assert.Equal(t, 1, got.FailCount)
}

func TestClaimFailedAttempt_Ladder(t *testing.T) {
	r := row(Claimable, NoReason, 60, 60)
	for _, wait := range []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 30 * time.Minute} {
		r = ClaimFailedAttempt(r, t0)
		require.Equal(t, Claimable, r.Status)
		require.Equal(t, t0.Add(wait), r.RetryAfter)
		require.False(t, ReadyToClaim(r, t0))
		require.True(t, ReadyToClaim(r, t0.Add(wait)))
	}
	r = ClaimFailedAttempt(r, t0)
	assert.Equal(t, Blocked, r.Status)
	assert.Equal(t, ClaimFailed, r.Reason)
	assert.Equal(t, t0.Add(RetryClaimFailed), r.RetryAfter)
}

func TestClaimOutcomes(t *testing.T) {
	ok := ClaimOK(row(Claimable, NoReason, 60, 60), t0)
	assert.Equal(t, Claimed, ok.Status)
	assert.Equal(t, FromPlatform, ok.Source)
	link := ClaimNeedsLink(row(Claimable, NoReason, 60, 60), t0)
	assert.Equal(t, Blocked, link.Status)
	assert.Equal(t, NeedsLink, link.Reason)
	assert.Equal(t, t0.Add(RetryNeedsLink), link.RetryAfter)
}

func TestUserActions(t *testing.T) {
	m := MarkCollected(row(Accruing, NoReason, 10, 60), t0)
	assert.Equal(t, Claimed, m.Status)
	assert.Equal(t, FromUser, m.Source)

	s := Skip(row(Accruing, NoReason, 10, 60), t0)
	assert.Equal(t, Blocked, s.Status)
	assert.Equal(t, UserSkip, s.Reason)
	assert.True(t, s.RetryAfter.IsZero(), "user_skip never auto-expires")

	failed := row(Blocked, ClaimFailed, 60, 60)
	failed.FailCount = 5
	r := Retry(failed, t0)
	assert.Equal(t, Claimable, r.Status)
	assert.Equal(t, 0, r.FailCount)
}

func TestExpireAndBlockLink_LeaveClaimedAlone(t *testing.T) {
	c := row(Claimed, NoReason, 60, 60)
	assert.Equal(t, Claimed, Expire(c, t0).Status)
	assert.Equal(t, Claimed, BlockLink(c, t0).Status)
	assert.Equal(t, Expired, Expire(row(Accruing, NoReason, 10, 60), t0).Reason)
	assert.Equal(t, Unlinked, BlockLink(row(Eligible, NoReason, 0, 60), t0).Reason)
	sk := row(Blocked, UserSkip, 0, 60)
	assert.Equal(t, UserSkip, BlockLink(sk, t0).Reason)
}

func TestMineable(t *testing.T) {
	assert.True(t, Mineable(row(Eligible, NoReason, 0, 60)))
	assert.True(t, Mineable(row(Accruing, NoReason, 1, 60)))
	assert.False(t, Mineable(row(Claimable, NoReason, 60, 60)))
	assert.False(t, Mineable(row(Blocked, NotEnrolled, 0, 60)))
	assert.False(t, Mineable(Row{}))
}

func TestMutators_NeverDemotePlatformClaim(t *testing.T) {
	platformClaimed := row(Claimed, NoReason, 60, 60)
	platformClaimed.Source = FromPlatform

	// ClaimNeedsLink must not demote a platform claim
	got := ClaimNeedsLink(platformClaimed, t0)
	assert.Equal(t, Claimed, got.Status)
	assert.Equal(t, FromPlatform, got.Source)

	// ClaimFailedAttempt must not demote a platform claim
	got = ClaimFailedAttempt(platformClaimed, t0)
	assert.Equal(t, Claimed, got.Status)
	assert.Equal(t, FromPlatform, got.Source)

	// Skip must not demote a platform claim
	got = Skip(platformClaimed, t0)
	assert.Equal(t, Claimed, got.Status)
	assert.Equal(t, FromPlatform, got.Source)

	// Retry must not demote a platform claim
	got = Retry(platformClaimed, t0)
	assert.Equal(t, Claimed, got.Status)
	assert.Equal(t, FromPlatform, got.Source)

	// MarkCollected must not downgrade a platform claim to user source
	got = MarkCollected(platformClaimed, t0)
	assert.Equal(t, Claimed, got.Status)
	assert.Equal(t, FromPlatform, got.Source)

	// But Skip on a user-claimed row DOES produce Blocked/UserSkip
	userClaimed := row(Claimed, NoReason, 60, 60)
	userClaimed.Source = FromUser
	got = Skip(userClaimed, t0)
	assert.Equal(t, Blocked, got.Status)
	assert.Equal(t, UserSkip, got.Reason)
	assert.Equal(t, FromUser, got.Source)
}

// C1: a backfilled ghost-skip has Required 0. When the platform stays silent
// (Kick reports unlisted rewards Known=false) but the campaign now carries the
// requirement, the re-derive must use it instead of sticking at sub_only.
func TestApply_UnknownBackfilledRowAdoptsRequired(t *testing.T) {
	for _, reason := range []Reason{NotEnrolled, SubOnly} {
		prev := row(Blocked, reason, 0, 0)
		prev.RetryAfter = t0.Add(-time.Minute)
		got := Apply(prev, Observation{Required: 120}, t0)
		assert.Equal(t, Eligible, got.Status, string(reason))
		assert.Equal(t, NoReason, got.Reason, string(reason))
		assert.Equal(t, 120, got.Required, string(reason))
	}
}

// I3 (superseded, see below): a campaign-level link block used to re-derive
// on the next Known observation regardless of source. That let a mid-watch
// session Progress event (loop.applyProgress, also Known=true) lift the
// block between reconciles even though the campaign was still unlinked, so
// the planner mined a drop whose claim could never succeed. Lifting an
// unlinked block is now the reconciler's job alone (dropstate.Retry once it
// confirms the campaign is linked); see TestApply_UnlinkedNeverLiftedByObservation
// and reconcile.TestRun_LinkedAfterUnlinkedUnblocksImmediately.

// A campaign-level link block behaves like user_skip: only a claimed
// observation can move it. Any other Known observation (including one from
// a mid-watch session Progress event) must leave it Blocked/Unlinked; only
// the reconciler (via dropstate.Retry, once it has confirmed the campaign
// is linked) may lift it.
func TestApply_UnlinkedNeverLiftedByObservation(t *testing.T) {
	prev := BlockLink(row(Accruing, NoReason, 10, 60), t0)
	require.Equal(t, Unlinked, prev.Reason)

	got := Apply(prev, Observation{Known: true, Minutes: 20, Required: 60}, t0.Add(time.Minute))
	assert.Equal(t, Blocked, got.Status)
	assert.Equal(t, Unlinked, got.Reason)

	got = Apply(prev, Observation{Known: true, Minutes: 0, Required: 60}, t0.Add(time.Minute))
	assert.Equal(t, Blocked, got.Status)
	assert.Equal(t, Unlinked, got.Reason)

	got = Apply(prev, Observation{Known: true, Claimed: true, Minutes: 60, Required: 60}, t0.Add(time.Minute))
	assert.Equal(t, Claimed, got.Status)
	assert.Equal(t, NoReason, got.Reason)
}

// A row that falls back from Claimable to Accruing/Eligible sheds its claim
// failure count so a later claim starts a fresh backoff ladder.
func TestApply_ClaimableDemotionResetsFailCount(t *testing.T) {
	prev := row(Claimable, NoReason, 60, 60)
	prev.FailCount = 3
	prev.RetryAfter = t0.Add(-time.Minute)
	got := Apply(prev, Observation{Known: true, Minutes: 30, Required: 120}, t0)
	assert.Equal(t, Accruing, got.Status)
	assert.Equal(t, 0, got.FailCount)
	got = Apply(prev, Observation{Known: true, Minutes: 0, Required: 60}, t0)
	assert.Equal(t, Eligible, got.Status)
	assert.Equal(t, 0, got.FailCount)
}

// ProbeNotEnrolled is the stall claim-probe's failure outcome: blocked with the
// 1h re-check window, never counted as a claim failure.
func TestProbeNotEnrolled(t *testing.T) {
	normal := row(Eligible, NoReason, 0, 60)
	normal.Source = FromUser
	got := ProbeNotEnrolled(normal, t0)
	assert.Equal(t, Blocked, got.Status)
	assert.Equal(t, NotEnrolled, got.Reason)
	assert.Equal(t, FromPlatform, got.Source)
	assert.Equal(t, t0.Add(RetryNotEnrolled), got.RetryAfter)
	assert.Equal(t, 0, got.FailCount, "a probe is not a claim failure")

	withFails := row(Eligible, NoReason, 0, 60)
	withFails.FailCount = 2
	assert.Equal(t, 2, ProbeNotEnrolled(withFails, t0).FailCount, "FailCount carried, never incremented")

	claimed := row(Claimed, NoReason, 60, 60)
	assert.Equal(t, claimed, ProbeNotEnrolled(claimed, t0), "platform-confirmed claim untouched")
	skipped := row(Blocked, UserSkip, 0, 60)
	skipped.Source = FromUser
	assert.Equal(t, skipped, ProbeNotEnrolled(skipped, t0), "user skip untouched")
}
