package main

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/api"
	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/points"
	"github.com/aalejandrofer/grubdrops/internal/runner"
	"github.com/aalejandrofer/grubdrops/internal/store"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

// errNoPointsSession means the account has no session a runner build loaded.
var errNoPointsSession = errors.New("points balances: no loaded session")

// pointsTargets lists the balances to keep fresh: every enabled Twitch
// account whose points mode is on, has a loaded session that can read
// balances and does not need a login, times each Force watch channel
// assigned to it.
func (ro *roster) pointsTargets(ctx context.Context) ([]points.Target, error) {
	accts, err := ro.q.ListEnabledAccounts(ctx)
	if err != nil {
		return nil, err
	}
	states := map[string]runner.State{}
	for _, sn := range ro.pool.Snapshots() {
		states[sn.AccountID] = sn.State
	}
	var out []points.Target
	for _, a := range accts {
		if a.Platform != "twitch" || !platform.PointsMode(a.PointsMode).On() || states[a.ID] == runner.StateNeedsLogin {
			continue
		}
		ls, ok := ro.loaded.get(a.ID)
		if !ok {
			continue
		}
		if _, ok := ls.backend.(platform.PointsBalancer); !ok {
			continue
		}
		for _, ch := range ro.forceFor(a.ID, a.Platform) {
			out = append(out, points.Target{AccountID: a.ID, Channel: ch})
		}
	}
	return out, nil
}

// fetchPointsBalance reads one balance with the account's loaded session.
func (ro *roster) fetchPointsBalance(ctx context.Context, accountID, channel string) (int, error) {
	ls, ok := ro.loaded.get(accountID)
	if !ok {
		return 0, errNoPointsSession
	}
	pb, ok := ls.backend.(platform.PointsBalancer)
	if !ok {
		return 0, errNoPointsSession
	}
	return pb.ChannelPointsBalance(ctx, ls.session, channel)
}

// publishPoints tells the UI a cached balance changed.
func (ro *roster) publishPoints() {
	ro.hub.Publish(api.Event{Name: api.EventPoints, Key: "points", Data: struct{}{}})
}

// newPointsRefresher builds the hourly balance refresher on st.
func (ro *roster) newPointsRefresher(st *store.PointsBalances) *points.Refresher {
	return &points.Refresher{
		Store:    st,
		Targets:  ro.pointsTargets,
		Fetch:    ro.fetchPointsBalance,
		OnChange: ro.publishPoints,
	}
}

// pointsBalanceHook is the runner's OnPointsBalance for account a: a balance
// the claim loop learned on one of a's Force watch channels goes into the
// cache, which defers that pair's hourly read. Other channels are skipped.
func (ro *roster) pointsBalanceHook(a gen.Account) func(ctx context.Context, channel string, balance int) {
	if a.Platform != "twitch" {
		return nil
	}
	st := store.NewPointsBalances(ro.q)
	return func(ctx context.Context, channel string, balance int) {
		if !slices.Contains(ro.forceFor(a.ID, a.Platform), channel) {
			return
		}
		changed, err := points.Record(ctx, st, a.ID, channel, balance, time.Now())
		if err != nil {
			if ctx.Err() == nil {
				slog.Warn("points balances: save claim balance failed", "kind", "error",
					"account", a.ID, "channel", channel, "err", err)
			}
			return
		}
		if changed {
			ro.publishPoints()
		}
	}
}
