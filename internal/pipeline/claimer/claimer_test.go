package claimer

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/aalejandrofer/grubdrops/internal/pipeline/dropstate"
	"github.com/aalejandrofer/grubdrops/internal/platform"
)

type fakeClaimer struct {
	res platform.ClaimResult
	got platform.DropProgress
}

func (f *fakeClaimer) ClaimDrop(_ context.Context, _ platform.Session, d platform.DropProgress) platform.ClaimResult {
	f.got = d
	return f.res
}

func TestAttempt(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	row := dropstate.Row{DropID: "d1", CampaignID: "c1", Status: dropstate.Claimable, Minutes: 60, Required: 60}
	cases := []struct {
		out    platform.ClaimOutcome
		status dropstate.Status
		reason dropstate.Reason
	}{
		{platform.ClaimOK, dropstate.Claimed, dropstate.NoReason},
		{platform.ClaimAlready, dropstate.Claimed, dropstate.NoReason},
		{platform.ClaimNeedsLink, dropstate.Blocked, dropstate.NeedsLink},
		{platform.ClaimFailed, dropstate.Claimable, dropstate.NoReason},
	}
	for _, tc := range cases {
		t.Run(tc.out.String(), func(t *testing.T) {
			fc := &fakeClaimer{res: platform.ClaimResult{Outcome: tc.out}}
			got, res := Attempt(context.Background(), fc, platform.Session{}, row, platform.DropProgress{DropID: "d1", InstanceID: "i"}, now)
			assert.Equal(t, tc.status, got.Status)
			assert.Equal(t, tc.reason, got.Reason)
			assert.Equal(t, tc.out, res.Outcome)
			assert.Equal(t, "i", fc.got.InstanceID, "instance id forwarded")
		})
	}
	fc := &fakeClaimer{res: platform.ClaimResult{Outcome: platform.ClaimFailed}}
	got, _ := Attempt(context.Background(), fc, platform.Session{}, row, platform.DropProgress{DropID: "d1"}, now)
	assert.Equal(t, 1, got.FailCount)
	assert.Equal(t, now.Add(time.Minute), got.RetryAfter)
}
