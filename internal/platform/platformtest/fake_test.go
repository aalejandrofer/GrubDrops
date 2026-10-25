package platformtest

import (
	"context"
	"testing"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

func TestFake_HeartbeatAccruesAndClaimMarks(t *testing.T) {
	f := NewFake()
	f.Progress["d1"] = &platform.Progress{BenefitID: "d1"}
	ctx := context.Background()
	s := platform.Session{}

	h, err := f.StartWatch(ctx, s, platform.Stream{Channel: "c"})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := f.Heartbeat(ctx, h); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.Claim(ctx, s, platform.DropBenefit{ID: "d1"}); err != nil {
		t.Fatal(err)
	}
	rows, err := f.InventoryProgress(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].MinutesWatched != 2 || !rows[0].Claimed {
		t.Fatalf("rows = %+v", rows)
	}
	if n := f.CallCount("Heartbeat:"); n != 2 {
		t.Fatalf("heartbeats = %d", n)
	}
	if f.Calls[0] != "Start:c" {
		t.Fatalf("Calls[0] = %q", f.Calls[0])
	}
}
