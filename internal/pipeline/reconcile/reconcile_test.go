package reconcile

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/pipeline/dropstate"
	"github.com/aalejandrofer/grubdrops/internal/platform"
)

type fakeBackend struct {
	platform.Backend // nil: unimplemented methods panic
	camps            []platform.Campaign
	progress         []platform.DropProgress
	progErr          error
}

func (f *fakeBackend) ListActiveCampaigns(context.Context, platform.Session) ([]platform.Campaign, error) {
	return f.camps, nil
}

func (f *fakeBackend) DropProgress(context.Context, platform.Session, []platform.Campaign) ([]platform.DropProgress, error) {
	return f.progress, f.progErr
}

type fakePersister struct{ got []platform.Campaign }

func (p *fakePersister) PersistCampaigns(_ context.Context, c []platform.Campaign) error {
	p.got = append(p.got, c...)
	return nil
}

var now = time.Unix(1_700_000_000, 0)

func camp(id, game string, drops ...string) platform.Campaign {
	c := platform.Campaign{ID: id, Platform: "twitch", Game: game, Status: "active", AccountLinked: true, AccountLinkChecked: true}
	for _, d := range drops {
		c.Benefits = append(c.Benefits, platform.DropBenefit{ID: d, CampaignID: id, RequiredMinutes: 60})
	}
	return c
}

func cfg(f *fakeBackend, p *fakePersister) Config {
	return Config{AccountID: "a", Platform: "twitch", Backend: f, Progress: f, Persister: p,
		AllowGame: func(g string) bool { return g == "G" }}
}

func byDrop(rows []dropstate.Row) map[string]dropstate.Row {
	m := map[string]dropstate.Row{}
	for _, r := range rows {
		m[r.DropID] = r
	}
	return m
}

func TestRun_ClaimedAfterLeavingInventory(t *testing.T) {
	f := &fakeBackend{camps: []platform.Campaign{camp("c1", "G", "d1")},
		progress: []platform.DropProgress{{DropID: "d1", CampaignID: "c1", Minutes: 60, Required: 60, Claimed: true, Known: true}}}
	prev := map[string]dropstate.Row{"d1": {AccountID: "a", DropID: "d1", CampaignID: "c1", Platform: "twitch", Status: dropstate.Accruing, Minutes: 30, Required: 60, Source: dropstate.FromPlatform}}
	res, err := Run(context.Background(), cfg(f, &fakePersister{}), prev, now)
	require.NoError(t, err)
	assert.Equal(t, dropstate.Claimed, byDrop(res.Rows)["d1"].Status)
}

func TestRun_NewDropSeedsIdentity(t *testing.T) {
	f := &fakeBackend{camps: []platform.Campaign{camp("c1", "G", "d1")},
		progress: []platform.DropProgress{{DropID: "d1", CampaignID: "c1", Minutes: 5, Required: 60, Known: true}}}
	res, err := Run(context.Background(), cfg(f, &fakePersister{}), nil, now)
	require.NoError(t, err)
	r := byDrop(res.Rows)["d1"]
	assert.Equal(t, "a", r.AccountID)
	assert.Equal(t, "c1", r.CampaignID)
	assert.Equal(t, "twitch", r.Platform)
	assert.Equal(t, dropstate.Accruing, r.Status)
}

func TestRun_ProgressErrorChangesNothing(t *testing.T) {
	f := &fakeBackend{camps: []platform.Campaign{camp("c1", "G", "d1")}, progErr: errors.New("details 500")}
	prev := map[string]dropstate.Row{"d1": {DropID: "d1", Status: dropstate.Accruing, Minutes: 30, Required: 60}}
	res, err := Run(context.Background(), cfg(f, &fakePersister{}), prev, now)
	require.Error(t, err)
	assert.Empty(t, res.Rows)
}

func TestRun_MultiItemDropOneRow(t *testing.T) {
	c := camp("c1", "G", "d1", "d1")
	f := &fakeBackend{camps: []platform.Campaign{c},
		progress: []platform.DropProgress{{DropID: "d1", CampaignID: "c1", Required: 60, Known: true}}}
	res, err := Run(context.Background(), cfg(f, &fakePersister{}), nil, now)
	require.NoError(t, err)
	assert.Len(t, res.Rows, 1)
}

func TestRun_UnlinkedCampaignBlocked_UnlessForced(t *testing.T) {
	c := camp("c1", "G", "d1")
	c.AccountLinked = false
	f := &fakeBackend{camps: []platform.Campaign{c},
		progress: []platform.DropProgress{{DropID: "d1", CampaignID: "c1", Required: 60, Known: true}}}
	res, err := Run(context.Background(), cfg(f, &fakePersister{}), nil, now)
	require.NoError(t, err)
	assert.Equal(t, dropstate.NeedsLink, byDrop(res.Rows)["d1"].Reason)

	cf := cfg(f, &fakePersister{})
	cf.ForceLinked = func(id string) bool { return id == "c1" }
	res, err = Run(context.Background(), cf, nil, now)
	require.NoError(t, err)
	assert.Equal(t, dropstate.Eligible, byDrop(res.Rows)["d1"].Status)
}

func TestRun_ExpiredCampaign(t *testing.T) {
	c := camp("c1", "G", "d1")
	c.Status = "expired"
	f := &fakeBackend{camps: []platform.Campaign{c}}
	prev := map[string]dropstate.Row{"d1": {DropID: "d1", Status: dropstate.Accruing, Minutes: 30, Required: 60}}
	res, err := Run(context.Background(), cfg(f, &fakePersister{}), prev, now)
	require.NoError(t, err)
	assert.Equal(t, dropstate.Expired, byDrop(res.Rows)["d1"].Reason)
	assert.Empty(t, res.Campaigns, "expired campaigns are not mining candidates")
}

func TestRun_ScopeAndPersist(t *testing.T) {
	nullGame := camp("c2", "", "d2")
	nullGame.AllowedChannels = []string{"fav"}
	reward := camp("c3", "G", "d3")
	reward.Kind = "reward"
	f := &fakeBackend{camps: []platform.Campaign{camp("c1", "Other", "d1"), nullGame, reward},
		progress: []platform.DropProgress{{DropID: "d2", CampaignID: "c2", Required: 60, Known: true}}}
	p := &fakePersister{}
	cf := cfg(f, p)
	cf.AllowChannel = func(chs []string) bool { return len(chs) > 0 && chs[0] == "fav" }
	res, err := Run(context.Background(), cf, nil, now)
	require.NoError(t, err)
	got := byDrop(res.Rows)
	assert.NotContains(t, got, "d1", "non-whitelisted game")
	assert.Contains(t, got, "d2", "null-game campaign allowed by channel")
	assert.NotContains(t, got, "d3", "reward kind has no watch time")
	require.Len(t, p.got, 1)
	assert.Equal(t, "c2", p.got[0].ID)
}
