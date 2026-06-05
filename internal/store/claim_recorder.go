package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"time"

	"github.com/aalejandrofer/dropsminer/internal/platform"
	"github.com/aalejandrofer/dropsminer/internal/store/gen"
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
	if r == nil || r.Q == nil {
		return nil
	}
	id := newClaimID()
	return r.Q.InsertClaim(ctx, gen.InsertClaimParams{
		ID:            id,
		AccountID:     accountID,
		BenefitID:     b.ID,
		ClaimedAt:     time.Now().Unix(),
		ValueMetaJson: "{}",
	})
}

func newClaimID() string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return "clm_" + hex.EncodeToString(b[:])
}
