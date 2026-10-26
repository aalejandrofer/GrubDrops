package kick

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// memLinks is an in-memory LinkStore that outlives a Backend.
type memLinks struct {
	mu sync.Mutex
	m  map[string]string
}

func newMemLinks() *memLinks { return &memLinks{m: map[string]string{}} }

func (l *memLinks) NeedsLink(context.Context) (map[string]string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := map[string]string{}
	for k, v := range l.m {
		out[k] = v
	}
	return out, nil
}

func (l *memLinks) SetNeedsLink(_ context.Context, acc, camp, u string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.m[acc+":"+camp] = u
	return nil
}

func (l *memLinks) ClearNeedsLink(_ context.Context, acc, camp string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.m, acc+":"+camp)
	return nil
}

const (
	claimPath    = "https://web.kick.com/api/v1/drops/claim"
	progressPath = "https://web.kick.com/api/v1/drops/progress"
	riotURL      = "https://auth.riotgames.com/authorize?client_id=x"
)

func valoCampaigns() fakeResp {
	return fakeResp{200, `{"data":[
		{"id":"camp1","game":"VALORANT","name":"Valo Drops","status":"active",
		 "rewards":[{"id":"r1","name":"Riot Icon","required_minutes":30}]}
	]}`}
}

func withFakeLinks(f *fakeDoer, ls LinkStore) *Backend {
	b := New(nil, nil, "grubdrops-browser-{slug}", 9090, 10*time.Minute, WithLinkStore(ls))
	b.api = &api{d: f}
	return b
}

func valoDrop() platform.DropBenefit {
	return platform.DropBenefit{ID: "r1", CampaignID: "camp1", Name: "Riot Icon", RequiredMinutes: 30}
}

// The runner's claim path records the link need, persists it, and a new
// Backend on the same store flags the campaign without any claim attempt.
func TestKickBackend_ClaimLinkRefusalPersists(t *testing.T) {
	ls := newMemLinks()
	f := &fakeDoer{resp: map[string]fakeResp{
		claimPath: {400, `{"data":{"connect_url":"` + riotURL + `"}}`},
		"https://web.kick.com/api/v1/drops/campaigns": valoCampaigns(),
	}}
	b := withFakeLinks(f, ls)
	err := b.Claim(context.Background(), sess("acc1"), valoDrop())
	require.Error(t, err)
	assert.True(t, errors.Is(err, platform.ErrClaimNeedsLink), "runner must see the platform-wide link error")

	camps, err := b.ListActiveCampaigns(context.Background(), sess("acc1"))
	require.NoError(t, err)
	require.Len(t, camps, 1)
	assert.True(t, camps[0].ClaimNeedsLink)
	assert.Equal(t, map[string]string{"acc1:camp1": riotURL}, ls.m)

	f2 := &fakeDoer{resp: map[string]fakeResp{
		"https://web.kick.com/api/v1/drops/campaigns": valoCampaigns(),
	}}
	b2 := withFakeLinks(f2, ls)
	camps, err = b2.ListActiveCampaigns(context.Background(), sess("acc1"))
	require.NoError(t, err)
	require.Len(t, camps, 1)
	assert.True(t, camps[0].ClaimNeedsLink, "restart must keep the link need")
	assert.Equal(t, riotURL, camps[0].AccountLinkURL)
}

// A successful claim clears the persisted link need.
func TestKickBackend_ClaimSuccessClearsLink(t *testing.T) {
	ls := newMemLinks()
	require.NoError(t, ls.SetNeedsLink(context.Background(), "acc1", "camp1", riotURL))
	f := &fakeDoer{resp: map[string]fakeResp{
		claimPath: {200, `{"data":{}}`},
		"https://web.kick.com/api/v1/drops/campaigns": valoCampaigns(),
	}}
	b := withFakeLinks(f, ls)
	require.NoError(t, b.Claim(context.Background(), sess("acc1"), valoDrop()))
	assert.Empty(t, ls.m)
	camps, err := b.ListActiveCampaigns(context.Background(), sess("acc1"))
	require.NoError(t, err)
	assert.False(t, camps[0].ClaimNeedsLink)
}

// The sweep skips a persisted link-refused campaign: no claim spam after a
// restart.
func TestKickBackend_SweepSkipsPersistedLink(t *testing.T) {
	ls := newMemLinks()
	require.NoError(t, ls.SetNeedsLink(context.Background(), "acc1", "camp1", riotURL))
	f := &fakeDoer{resp: map[string]fakeResp{
		progressPath: {200, `{"data":[{"id":"camp1","rewards":[
			{"id":"r1","name":"Riot Icon","progress":1,"claimed":false,"required_units":30}]}]}`},
		claimPath: {400, `{"data":{"connect_url":"` + riotURL + `"}}`},
	}}
	b := withFakeLinks(f, ls)
	_, err := b.SweepCompletedClaims(context.Background(), sess("acc1"))
	require.NoError(t, err)
	for _, c := range f.calls {
		assert.NotEqual(t, claimPath, c.path)
	}
}

// RecheckLinks forgets the refusal, retries at once and reports the result.
func TestKickBackend_RecheckLinks(t *testing.T) {
	progress := fakeResp{200, `{"data":[{"id":"camp1","rewards":[
		{"id":"r1","name":"Riot Icon","progress":1,"claimed":false,"required_units":30}]}]}`}

	t.Run("still refused", func(t *testing.T) {
		ls := newMemLinks()
		require.NoError(t, ls.SetNeedsLink(context.Background(), "acc1", "camp1", riotURL))
		f := &fakeDoer{resp: map[string]fakeResp{
			progressPath: progress,
			claimPath:    {400, `{"data":{"connect_url":"` + riotURL + `"}}`},
		}}
		b := withFakeLinks(f, ls)
		claimed, still, err := b.RecheckLinks(context.Background(), sess("acc1"))
		require.NoError(t, err)
		assert.Empty(t, claimed)
		assert.Equal(t, 1, still)
		assert.Equal(t, map[string]string{"acc1:camp1": riotURL}, ls.m)
	})

	t.Run("linked now", func(t *testing.T) {
		ls := newMemLinks()
		require.NoError(t, ls.SetNeedsLink(context.Background(), "acc1", "camp1", riotURL))
		require.NoError(t, ls.SetNeedsLink(context.Background(), "acc2", "camp1", riotURL))
		f := &fakeDoer{resp: map[string]fakeResp{
			progressPath: progress,
			claimPath:    {200, `{"data":{}}`},
		}}
		b := withFakeLinks(f, ls)
		claimed, still, err := b.RecheckLinks(context.Background(), sess("acc1"))
		require.NoError(t, err)
		require.Len(t, claimed, 1)
		assert.Equal(t, "Riot Icon", claimed[0].Title)
		assert.Equal(t, 0, still)
		assert.Equal(t, map[string]string{"acc2:camp1": riotURL}, ls.m, "other accounts keep theirs")
	})
}
