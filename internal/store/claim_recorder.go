package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

// ClaimRecorder implements watcher.ClaimRecorder by writing a row to
// the claims table whenever the watcher confirms a successful claim.
// Without this the /drops Past tab + /history view stay empty because
// the InsertClaim query had no production caller.
type ClaimRecorder struct {
	Q *gen.Queries
}

// NewClaimRecorder returns a recorder backed by q.
func NewClaimRecorder(q *gen.Queries) *ClaimRecorder {
	return &ClaimRecorder{Q: q}
}

// RecordClaim writes a claims row for accountID + benefit. The ID is a
// fresh random hex string so re-claims (same benefit twice) don't
// collide on the PK; the (account_id, benefit_id) pair is the
// reportable key, not the row ID.
func (r *ClaimRecorder) RecordClaim(ctx context.Context, accountID string, b platform.DropBenefit) error {
	return r.RecordClaimWithCode(ctx, accountID, b, "")
}

// RecordClaimWithCode writes a claim row and stores the redemption
// code in value_meta_json so the /drops + /history surfaces can show
// it. Used by the F9 onsite-notification path (Minecraft codes etc).
// Empty code degrades to the same blob the bare-claim flow writes.
func (r *ClaimRecorder) RecordClaimWithCode(ctx context.Context, accountID string, b platform.DropBenefit, code string) error {
	if r == nil || r.Q == nil {
		return nil
	}
	meta := "{}"
	if code != "" {
		raw, _ := json.Marshal(struct {
			Code        string `json:"code"`
			BenefitName string `json:"benefit_name,omitempty"`
		}{Code: code, BenefitName: b.Name})
		meta = string(raw)
	}
	return r.Q.InsertClaim(ctx, gen.InsertClaimParams{
		ID:            newClaimID(),
		AccountID:     accountID,
		BenefitID:     b.ID,
		ClaimedAt:     time.Now().Unix(),
		ValueMetaJson: meta,
	})
}

// RecordClaimIfNew records a claim only when no row exists yet for
// (accountID, benefit.ID). Used by the inventory-ownership reconcile so a
// drop the user claimed MANUALLY (outside the bot) shows as collected,
// without inserting a duplicate row on every discovery cycle. Returns true
// when a new row was written.
func (r *ClaimRecorder) RecordClaimIfNew(ctx context.Context, accountID string, b platform.DropBenefit) (bool, error) {
	if r == nil || r.Q == nil || b.ID == "" {
		return false, nil
	}
	n, err := r.Q.CountClaimsFor(ctx, gen.CountClaimsForParams{AccountID: accountID, BenefitID: b.ID})
	if err != nil {
		return false, err
	}
	if n > 0 {
		return false, nil
	}
	if err := r.RecordClaim(ctx, accountID, b); err != nil {
		return false, err
	}
	return true, nil
}

// PruneClaim removes any claim row for (accountID, benefit.ID). Used by the
// reconcile prune to undo a false-positive COLLECTED mark: when inventory
// shows the drop is still in progress and unclaimed, no claims row should
// exist. No-op when nothing is stored.
func (r *ClaimRecorder) PruneClaim(ctx context.Context, accountID string, b platform.DropBenefit) (bool, error) {
	if r == nil || r.Q == nil || b.ID == "" {
		return false, nil
	}
	n, err := r.Q.CountClaimsFor(ctx, gen.CountClaimsForParams{AccountID: accountID, BenefitID: b.ID})
	if err != nil {
		return false, err
	}
	if n == 0 {
		return false, nil
	}
	if err := r.Q.DeleteClaimFor(ctx, gen.DeleteClaimForParams{AccountID: accountID, BenefitID: b.ID}); err != nil {
		return false, err
	}
	return true, nil
}

// ClaimedBenefitIDs returns the set of benefit ids this account already has a
// claim row for. The watcher uses it to skip re-mining a drop it has already
// claimed but that Twitch has dropped from the in-progress inventory (so its
// per-drop IsClaimed is no longer visible). Keyed by benefit id, which is
// unique per drop instance, so it never bleeds across campaigns.
func (r *ClaimRecorder) ClaimedBenefitIDs(ctx context.Context, accountID string) (map[string]bool, error) {
	if r == nil || r.Q == nil {
		return nil, nil
	}
	ids, err := r.Q.ListClaimedBenefitIDsForAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out, nil
}

func newClaimID() string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return "clm_" + hex.EncodeToString(b[:])
}
