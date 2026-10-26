package discover

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/platform/twitch"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func counting(n *atomic.Int32, camps []platform.Campaign, err error) Fetcher {
	return func(context.Context) ([]platform.Campaign, error) {
		n.Add(1)
		return camps, err
	}
}

func TestGet_CachesAndRefreshIsRateLimited(t *testing.T) {
	var n atomic.Int32
	c := &clock{t: time.Unix(1000, 0)}
	s := New(Source{Platform: "kick", Fetch: counting(&n, []platform.Campaign{{ID: "a"}}, nil), Timeout: time.Second})
	s.Now = c.now

	s.Get(context.Background(), false)
	s.Get(context.Background(), false)
	assert.EqualValues(t, 1, n.Load(), "second call served from cache")

	c.t = c.t.Add(30 * time.Second)
	s.Get(context.Background(), true)
	assert.EqualValues(t, 1, n.Load(), "refresh inside MinRefresh served from cache")

	c.t = c.t.Add(31 * time.Second)
	s.Get(context.Background(), true)
	assert.EqualValues(t, 2, n.Load(), "refresh after MinRefresh fetches")
	assert.Equal(t, c.t.Add(DefaultMinRefresh), s.NextRefresh())

	c.t = c.t.Add(DefaultTTL)
	s.Get(context.Background(), false)
	assert.EqualValues(t, 3, n.Load(), "stale cache fetches")
}

func TestGet_PartialFailure(t *testing.T) {
	var tw, kk atomic.Int32
	c := &clock{t: time.Unix(1000, 0)}
	s := New(
		Source{Platform: "twitch", Fetch: counting(&tw, nil, errors.New("boom")), Timeout: time.Second},
		Source{Platform: "kick", Fetch: counting(&kk, []platform.Campaign{{ID: "k"}}, nil), Timeout: time.Second},
	)
	s.Now = c.now

	snap := s.Get(context.Background(), false)
	require.Error(t, snap.Results["twitch"].Err)
	assert.Equal(t, "failed", ErrCode(snap.Results["twitch"].Err))
	assert.NoError(t, snap.Results["kick"].Err)
	assert.Len(t, snap.Results["kick"].Campaigns, 1)

	c.t = c.t.Add(DefaultErrTTL)
	s.Get(context.Background(), false)
	assert.EqualValues(t, 2, tw.Load(), "a failed snapshot expires after ErrTTL")
}

func TestGet_Timeout(t *testing.T) {
	slow := func(ctx context.Context) ([]platform.Campaign, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	s := New(Source{Platform: "twitch", Fetch: slow, Timeout: 10 * time.Millisecond})
	snap := s.Get(context.Background(), false)
	assert.Equal(t, "timeout", ErrCode(snap.Results["twitch"].Err))
}

func TestGet_SurvivesCancelledRequest(t *testing.T) {
	ok := func(ctx context.Context) ([]platform.Campaign, error) {
		return []platform.Campaign{{ID: "x"}}, ctx.Err()
	}
	s := New(Source{Platform: "kick", Fetch: ok, Timeout: time.Second})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	snap := s.Get(ctx, false)
	assert.NoError(t, snap.Results["kick"].Err)
}

func accounts(as ...gen.Account) Accounts {
	return func(context.Context) ([]gen.Account, error) { return as, nil }
}

func sessionsBy(m map[string]platform.Session) SessionFor {
	return func(_ context.Context, id string) (platform.Session, bool, error) {
		s, ok := m[id]
		return s, ok, nil
	}
}

func TestTwitchFetcher_PicksAndroidAccount(t *testing.T) {
	var used []string
	cat := func(_ context.Context, s platform.Session) ([]platform.Campaign, error) {
		used = append(used, s.AccountID)
		if s.AccountID == "a2" {
			return nil, twitch.ErrCatalogUnavailable
		}
		return []platform.Campaign{{ID: "c"}}, nil
	}
	f := TwitchFetcher(
		accounts(
			gen.Account{ID: "tv", Platform: "twitch", Enabled: 1},
			gen.Account{ID: "off", Platform: "twitch", Enabled: 0},
			gen.Account{ID: "k", Platform: "kick", Enabled: 1},
			gen.Account{ID: "a2", Platform: "twitch", Enabled: 1},
			gen.Account{ID: "a3", Platform: "twitch", Enabled: 1},
		),
		sessionsBy(map[string]platform.Session{
			"tv":  {AccessToken: "t", ClientID: twitch.ClientTV},
			"off": {AccessToken: "t"},
			"k":   {AccessToken: "t"},
			"a2":  {AccessToken: "t", ClientID: twitch.ClientAndroid},
			"a3":  {AccessToken: "t"},
		}),
		cat,
	)
	camps, err := f(context.Background())
	require.NoError(t, err)
	assert.Len(t, camps, 1)
	assert.Equal(t, []string{"a2", "a3"}, used)
}

func TestTwitchFetcher_NoAndroid(t *testing.T) {
	f := TwitchFetcher(
		accounts(gen.Account{ID: "tv", Platform: "twitch", Enabled: 1}),
		sessionsBy(map[string]platform.Session{"tv": {AccessToken: "t", ClientID: twitch.ClientTV}}),
		func(context.Context, platform.Session) ([]platform.Campaign, error) {
			t.Fatal("catalog must not run for a TV token")
			return nil, nil
		},
	)
	_, err := f(context.Background())
	assert.Equal(t, "needs_android", ErrCode(err))
}

func TestKickFetcher_FallsBackToNoSession(t *testing.T) {
	var used []string
	cat := func(_ context.Context, s platform.Session) ([]platform.Campaign, error) {
		used = append(used, s.AccountID)
		if s.AccountID != "" {
			return nil, errors.New("authed failed")
		}
		return []platform.Campaign{{ID: "c"}}, nil
	}
	f := KickFetcher(
		accounts(gen.Account{ID: "k1", Platform: "kick", Enabled: 1}),
		sessionsBy(map[string]platform.Session{"k1": {}}),
		cat,
	)
	camps, err := f(context.Background())
	require.NoError(t, err)
	assert.Len(t, camps, 1)
	assert.Equal(t, []string{"k1", ""}, used)
}
