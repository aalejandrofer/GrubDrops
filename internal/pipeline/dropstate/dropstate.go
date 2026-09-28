// Package dropstate is the per-(account, drop) state model for pipeline v2.
// Every transition is a pure function of (previous row, input, now) so the
// rules are table-testable and the account loop is the only writer.
package dropstate

import "time"

type Status string

const (
	Eligible  Status = "eligible"
	Accruing  Status = "accruing"
	Claimable Status = "claimable"
	Claimed   Status = "claimed"
	Blocked   Status = "blocked"
)

type Reason string

const (
	NoReason    Reason = ""
	NeedsLink   Reason = "needs_link"
	ClaimFailed Reason = "claim_failed"
	SubOnly     Reason = "sub_only"
	NoChannels  Reason = "no_channels"
	Expired     Reason = "expired"
	NotEnrolled Reason = "not_enrolled"
	UserSkip    Reason = "user_skip"
	// Unlinked is the campaign-level link block: the campaign needs an
	// external account the user has not linked. Unlike the claim-level
	// NeedsLink it has no hold; the next definite answer re-derives it.
	Unlinked Reason = "unlinked"
)

type Source string

const (
	FromPlatform Source = "platform"
	FromUser     Source = "user"
)

// Retry windows for blocked rows. user_skip never auto-expires.
const (
	RetryNeedsLink   = 24 * time.Hour
	RetryClaimFailed = 6 * time.Hour
	RetryNotEnrolled = time.Hour
	RetrySubOnly     = 24 * time.Hour
	RetryExpired     = 24 * time.Hour
	MaxClaimFailures = 5
)

var claimBackoff = []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute}

type Row struct {
	AccountID  string
	DropID     string
	CampaignID string
	Platform   string
	Status     Status
	Reason     Reason
	Minutes    int
	Required   int
	Source     Source
	FailCount  int
	RetryAfter time.Time
	SyncedAt   time.Time
	UpdatedAt  time.Time
}

// IsZero reports a row that has never been stored or derived.
func (r Row) IsZero() bool { return r.Status == "" }

// Observation is what the platform said about one drop in one sync.
type Observation struct {
	// Known is true when the platform returned a definite record for the
	// drop. False means it said nothing, which proves nothing.
	Known bool
	// Unmineable marks drops the platform cannot track (scrape placeholders).
	Unmineable bool
	Claimed    bool
	Minutes    int
	Required   int
}

func derive(minutes, required int) (Status, Reason) {
	switch {
	case required <= 0:
		return Blocked, SubOnly
	case minutes >= required:
		return Claimable, NoReason
	case minutes > 0:
		return Accruing, NoReason
	default:
		return Eligible, NoReason
	}
}

func retryFor(reason Reason, now time.Time) time.Time {
	switch reason {
	case NeedsLink, Unlinked:
		return now.Add(RetryNeedsLink)
	case ClaimFailed:
		return now.Add(RetryClaimFailed)
	case NotEnrolled, NoChannels:
		return now.Add(RetryNotEnrolled)
	case SubOnly:
		return now.Add(RetrySubOnly)
	case Expired:
		return now.Add(RetryExpired)
	}
	return time.Time{}
}

func (r Row) with(s Status, reason Reason, now time.Time) Row {
	r.Status, r.Reason, r.UpdatedAt = s, reason, now
	r.RetryAfter = retryFor(reason, now)
	return r
}

// Apply folds one platform observation into the previous row. The caller
// seeds identity fields (AccountID, DropID, CampaignID, Platform) on a new row.
func Apply(prev Row, obs Observation, now time.Time) Row {
	r := prev
	if obs.Known && !obs.Unmineable {
		r.Minutes, r.Required, r.SyncedAt = obs.Minutes, obs.Required, now
	}
	// A platform-confirmed claim is final.
	if prev.Status == Claimed && prev.Source == FromPlatform {
		if r.Minutes < prev.Minutes {
			r.Minutes = prev.Minutes
		}
		return r
	}
	if !obs.Known {
		// A backfilled row carries no requirement; adopt the catalog's so a
		// re-derive does not read (0, 0) as sub_only forever.
		if prev.Required <= 0 && obs.Required > 0 {
			r.Required = obs.Required
		}
		switch {
		case prev.IsZero():
			r.Required = obs.Required
			r.Source = FromPlatform
			st, rs := derive(0, obs.Required)
			return r.with(st, rs, now)
		case (prev.Status == Accruing || prev.Status == Claimable) && prev.Source == FromPlatform:
			return r.with(Blocked, NotEnrolled, now)
		case prev.Status == Blocked && (prev.Reason == NotEnrolled || prev.Reason == SubOnly) && !now.Before(prev.RetryAfter):
			st, rs := derive(prev.Minutes, r.Required)
			return r.with(st, rs, now)
		}
		return r
	}
	if obs.Unmineable {
		if prev.Status == Claimed {
			return r
		}
		r.Source = FromPlatform
		return r.with(Blocked, NotEnrolled, now)
	}
	if obs.Claimed {
		r.Source, r.FailCount = FromPlatform, 0
		return r.with(Claimed, NoReason, now)
	}
	if prev.Status == Blocked && prev.Reason == UserSkip {
		return r
	}
	if prev.Status == Blocked && (prev.Reason == NeedsLink || prev.Reason == ClaimFailed) && now.Before(prev.RetryAfter) {
		return r
	}
	st, rs := derive(r.Minutes, r.Required)
	if st == Claimable && prev.Status == Claimable && now.Before(prev.RetryAfter) {
		return r // keep claim backoff
	}
	if prev.Status == Blocked || (prev.Status == Claimable && st != Claimable) {
		r.FailCount = 0
	}
	r.Source = FromPlatform
	return r.with(st, rs, now)
}

// ClaimOK records a successful or already-claimed claim.
func ClaimOK(prev Row, now time.Time) Row {
	r := prev.with(Claimed, NoReason, now)
	r.Source, r.FailCount = FromPlatform, 0
	return r
}

// ClaimNeedsLink blocks a drop whose claim needs an external account link.
func ClaimNeedsLink(prev Row, now time.Time) Row {
	if prev.Status == Claimed && prev.Source == FromPlatform {
		return prev
	}
	r := prev.with(Blocked, NeedsLink, now)
	r.Source = FromPlatform
	return r
}

// ClaimFailedAttempt counts a failed claim: backoff 1m, 5m, 30m, then
// blocked:claim_failed after MaxClaimFailures.
func ClaimFailedAttempt(prev Row, now time.Time) Row {
	if prev.Status == Claimed && prev.Source == FromPlatform {
		return prev
	}
	r := prev
	r.FailCount++
	r.UpdatedAt = now
	if r.FailCount >= MaxClaimFailures {
		return r.with(Blocked, ClaimFailed, now)
	}
	i := r.FailCount - 1
	if i >= len(claimBackoff) {
		i = len(claimBackoff) - 1
	}
	r.RetryAfter = now.Add(claimBackoff[i])
	return r
}

// ProbeNotEnrolled records a failed stall claim-probe: the platform neither
// tracks the drop nor accepts a claim for it, so block it not_enrolled (1h
// re-check). A probe is not a claim of a completed drop, so FailCount is
// left alone. Platform-confirmed claims and user skips are kept.
func ProbeNotEnrolled(prev Row, now time.Time) Row {
	if (prev.Status == Claimed && prev.Source == FromPlatform) || (prev.Status == Blocked && prev.Reason == UserSkip) {
		return prev
	}
	r := prev.with(Blocked, NotEnrolled, now)
	r.Source = FromPlatform
	return r
}

// MarkCollected is the user asserting the drop is claimed.
func MarkCollected(prev Row, now time.Time) Row {
	if prev.Status == Claimed {
		return prev
	}
	r := prev.with(Claimed, NoReason, now)
	r.Source = FromUser
	return r
}

// Skip is the user excluding a drop from mining until they retry it.
func Skip(prev Row, now time.Time) Row {
	if prev.Status == Claimed && prev.Source == FromPlatform {
		return prev
	}
	r := prev.with(Blocked, UserSkip, now)
	r.Source = FromUser
	return r
}

// Retry clears any block and re-derives from the last known minutes.
func Retry(prev Row, now time.Time) Row {
	if prev.Status == Claimed && prev.Source == FromPlatform {
		return prev
	}
	st, rs := derive(prev.Minutes, prev.Required)
	r := prev.with(st, rs, now)
	r.FailCount, r.Source = 0, FromPlatform
	return r
}

// Expire blocks an unclaimed drop whose campaign has ended.
func Expire(prev Row, now time.Time) Row {
	if prev.Status == Claimed {
		return prev
	}
	return prev.with(Blocked, Expired, now)
}

// BlockLink blocks a drop whose campaign needs an unlinked external account.
func BlockLink(prev Row, now time.Time) Row {
	if prev.Status == Claimed || (prev.Status == Blocked && prev.Reason == UserSkip) {
		return prev
	}
	return prev.with(Blocked, Unlinked, now)
}

// Mineable reports whether watching can still advance the drop.
func Mineable(r Row) bool { return r.Status == Eligible || r.Status == Accruing }

// ReadyToClaim reports a claimable row whose backoff has elapsed.
func ReadyToClaim(r Row, now time.Time) bool {
	return r.Status == Claimable && !now.Before(r.RetryAfter)
}
