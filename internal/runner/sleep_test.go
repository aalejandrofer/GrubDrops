package runner

import (
	"context"
	"testing"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/plan"
	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/platform/platformtest"
)

func TestPickClearsExpiredSleepDeadlineBeforeSelectingDrop(t *testing.T) {
	r := newRunner(platformtest.NewFake(), nil)
	target := plan.Target{
		Campaign: platform.Campaign{ID: "campaign", EndsAt: time.Now().Add(time.Hour)},
		Benefit:  platform.DropBenefit{ID: "drop"},
	}
	r.pinned = &target
	r.sleepUntil = time.Now().Add(-time.Second)

	if err := r.pick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if r.State() != StateStarting {
		t.Fatalf("selected drop state = %s, want %s", r.State(), StateStarting)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.sleepUntil.IsZero() {
		t.Fatalf("expired scheduled sleep remains set: %v", r.sleepUntil)
	}
}
