package main

import (
	"context"
	"errors"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/notify"
	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/rewards"
	"github.com/aalejandrofer/grubdrops/internal/runner"
	"github.com/aalejandrofer/grubdrops/internal/store"
)

// errNoRewardSession means the account has no session a runner build loaded.
var errNoRewardSession = errors.New("reward codes: no loaded session")

// rewardTargets lists the accounts whose reward codes to read: every enabled
// Twitch account with a loaded session that can read them and does not need
// a login.
func (ro *roster) rewardTargets(ctx context.Context) ([]rewards.Target, error) {
	accts, err := ro.q.ListEnabledAccounts(ctx)
	if err != nil {
		return nil, err
	}
	states := map[string]runner.State{}
	for _, sn := range ro.pool.Snapshots() {
		states[sn.AccountID] = sn.State
	}
	var out []rewards.Target
	for _, a := range accts {
		if a.Platform != "twitch" || states[a.ID] == runner.StateNeedsLogin {
			continue
		}
		ls, ok := ro.loaded.get(a.ID)
		if !ok {
			continue
		}
		if _, ok := ls.backend.(platform.RewardCodeReader); !ok {
			continue
		}
		out = append(out, rewards.Target{AccountID: a.ID, Read: ro.rewardReader(a.ID)})
	}
	return out, nil
}

// rewardReader reads accountID's reward codes with its session at read time.
func (ro *roster) rewardReader(accountID string) rewards.Reader {
	return func(ctx context.Context, known func(string) (bool, bool)) ([]platform.RewardCode, error) {
		ls, ok := ro.loaded.get(accountID)
		if !ok {
			return nil, errNoRewardSession
		}
		rr, ok := ls.backend.(platform.RewardCodeReader)
		if !ok {
			return nil, errNoRewardSession
		}
		return rr.RewardCodes(ctx, ls.session, known)
	}
}

// newRewardPoller builds the reward code poller on st.
func (ro *roster) newRewardPoller(st *store.RewardCodes, settings *store.Settings, n notify.Notifier, zone func() *time.Location) *rewards.Poller {
	return &rewards.Poller{
		Store:    st,
		Targets:  ro.rewardTargets,
		Notifier: n,
		AccountName: func(_ context.Context, id string) string {
			return ro.accountName(id)
		},
		Lang: func(ctx context.Context) string {
			l, _ := settings.UILanguage(ctx)
			return l
		},
		Zone: zone,
	}
}
