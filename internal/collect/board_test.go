package collect

import (
	"errors"
	"testing"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/plan"
	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/runner"
)

var (
	rust      = plan.Game{Name: "Rust", Slug: "rust"}
	overwatch = plan.Game{Name: "Overwatch", Slug: "overwatch"}
	oct30     = time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)
	oct31     = time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC)
)

func camp(id, plat, game string, ends time.Time, drops int) platform.Campaign {
	c := platform.Campaign{ID: id, Platform: plat, Game: game, Name: "Camp " + id, EndsAt: ends, Status: "active"}
	for i := 0; i < drops; i++ {
		c.Benefits = append(c.Benefits, platform.DropBenefit{ID: id + "-b", CampaignID: id, RequiredMinutes: 30})
	}
	return c
}

func scan(acc, plat string, at time.Time, cs ...platform.Campaign) runner.Scan {
	return runner.Scan{AccountID: acc, Platform: plat, At: at, Campaigns: cs}
}

func TestBoard_MergesAccountsAndDedupes(t *testing.T) {
	b := NewBoard()
	t1 := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	c1 := camp("c1", "twitch", "Rust", oct31, 3)
	c2 := camp("c2", "twitch", "Rust", oct30, 1)
	b.Observe(scan("a1", "twitch", t1, c1))
	b.Observe(scan("a2", "twitch", t1, c1, c2))
	b.Observe(scan("a3", "kick", t1, camp("k1", "kick", "Rust", oct31, 1)))
	got := b.Games([]plan.Game{rust})
	if len(got) != 1 || len(got[0].Campaigns) != 3 {
		t.Fatalf("got %+v", got)
	}
	cs := got[0].Campaigns
	if cs[0].ID != "c2" || cs[1].ID != "c1" || cs[2].ID != "k1" {
		t.Fatalf("order %v %v %v", cs[0].ID, cs[1].ID, cs[2].ID)
	}
	if cs[1].Drops != 3 || cs[1].Platform != "twitch" || cs[1].Name != "Camp c1" || !cs[1].EndsAt.Equal(oct31) {
		t.Fatalf("summary %+v", cs[1])
	}
}

func TestBoard_GameWithoutCampaigns(t *testing.T) {
	b := NewBoard()
	t1 := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	b.Observe(scan("a1", "twitch", t1, camp("c1", "twitch", "Rust", oct31, 1)))
	got := b.Games([]plan.Game{rust, overwatch})
	if len(got) != 2 || got[1].Game != overwatch || len(got[1].Campaigns) != 0 {
		t.Fatalf("got %+v", got)
	}
	if !got[1].LastScanAt.Equal(t1) || !got[0].LastScanAt.Equal(t1) {
		t.Fatalf("LastScanAt %v %v", got[0].LastScanAt, got[1].LastScanAt)
	}
}

func TestBoard_ErrScanIgnored(t *testing.T) {
	b := NewBoard()
	t1 := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	t2 := t1.Add(time.Hour)
	b.Observe(scan("a1", "twitch", t1, camp("c1", "twitch", "Rust", oct31, 1)))
	b.Observe(runner.Scan{AccountID: "a1", Platform: "twitch", At: t2, Err: errors.New("boom")})
	got := b.Games([]plan.Game{rust})
	if !got[0].LastScanAt.Equal(t1) || len(got[0].Campaigns) != 1 {
		t.Fatalf("got %+v", got[0])
	}
}

func TestBoard_LatestScanReplacesAccount(t *testing.T) {
	b := NewBoard()
	t1 := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	b.Observe(scan("a1", "twitch", t1, camp("c1", "twitch", "Rust", oct31, 1)))
	b.Observe(scan("a1", "twitch", t1.Add(time.Minute)))
	got := b.Games([]plan.Game{rust})
	if len(got[0].Campaigns) != 0 {
		t.Fatalf("got %+v", got[0].Campaigns)
	}
}

func TestBoard_SkipsRewardAndUpcoming(t *testing.T) {
	b := NewBoard()
	reward := camp("r", "twitch", "Rust", oct31, 1)
	reward.Kind = "reward"
	upcoming := camp("u", "twitch", "Rust", oct31, 1)
	upcoming.Status = "upcoming"
	empty := camp("e", "twitch", "Rust", oct31, 0)
	b.Observe(scan("a1", "twitch", time.Now(), reward, upcoming, empty))
	if got := b.Games([]plan.Game{rust}); len(got[0].Campaigns) != 0 {
		t.Fatalf("got %+v", got[0].Campaigns)
	}
}

func TestBoard_Forget(t *testing.T) {
	b := NewBoard()
	b.Observe(scan("a1", "twitch", time.Now(), camp("c1", "twitch", "Rust", oct31, 1)))
	b.Forget("a1")
	if got := b.Games([]plan.Game{rust}); len(got[0].Campaigns) != 0 {
		t.Fatalf("got %+v", got[0].Campaigns)
	}
}
