// Package reconcile folds platform per-campaign progress into drop_state.
// It never decides what to watch.
package reconcile

import (
	"context"
	"fmt"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/pipeline/dropstate"
	"github.com/aalejandrofer/grubdrops/internal/platform"
)

type CampaignPersister interface {
	PersistCampaigns(ctx context.Context, camps []platform.Campaign) error
}

type Config struct {
	AccountID    string
	Platform     string
	Backend      platform.Backend
	Progress     platform.DropProgressSource
	Session      platform.Session
	AllowGame    func(game string) bool
	AllowChannel func(channels []string) bool
	ForceLinked  func(campaignID string) bool
	Persister    CampaignPersister
}

type Result struct {
	Campaigns []platform.Campaign
	Rows      []dropstate.Row
	Progress  map[string]platform.DropProgress
}

// InScope keeps campaigns the account mines: whitelisted game, or a
// campaign whose channels match the account's priority streamers
// (null-game drops). One-click reward campaigns have no watch time.
func InScope(camps []platform.Campaign, allowGame func(string) bool, allowChannel func([]string) bool) []platform.Campaign {
	var out []platform.Campaign
	for _, c := range camps {
		if c.Kind == "reward" {
			continue
		}
		ok := allowGame == nil || allowGame(c.Game)
		if !ok && allowChannel != nil && allowChannel(c.AllowedChannels) {
			ok = true
		}
		if ok {
			out = append(out, c)
		}
	}
	return out
}

func isActive(c platform.Campaign, now time.Time) bool {
	if c.Status != "" && c.Status != "active" {
		return false
	}
	return c.EndsAt.IsZero() || now.Before(c.EndsAt)
}

// Run lists campaigns, persists the in-scope ones, reads their progress and
// returns every drop row it touched. On any platform error it returns the
// error and no rows, so a failed sync never changes state.
func Run(ctx context.Context, cfg Config, prev map[string]dropstate.Row, now time.Time) (Result, error) {
	camps, err := cfg.Backend.ListActiveCampaigns(ctx, cfg.Session)
	if err != nil {
		return Result{}, fmt.Errorf("list campaigns: %w", err)
	}
	scope := InScope(camps, cfg.AllowGame, cfg.AllowChannel)
	if cfg.Persister != nil && len(scope) > 0 {
		if err := cfg.Persister.PersistCampaigns(ctx, scope); err != nil {
			return Result{}, fmt.Errorf("persist campaigns: %w", err)
		}
	}
	var active []platform.Campaign
	for _, c := range scope {
		if isActive(c, now) {
			active = append(active, c)
		}
	}
	byDrop := map[string]platform.DropProgress{}
	if len(active) > 0 {
		obs, err := cfg.Progress.DropProgress(ctx, cfg.Session, active)
		if err != nil {
			return Result{}, fmt.Errorf("drop progress: %w", err)
		}
		for _, o := range obs {
			byDrop[o.DropID] = o
		}
	}

	res := Result{Campaigns: active, Progress: byDrop}
	seen := map[string]bool{}
	for _, c := range scope {
		if c.Status == "upcoming" {
			continue
		}
		expired := !isActive(c, now)
		linked := c.AccountLinked || !c.AccountLinkChecked || (cfg.ForceLinked != nil && cfg.ForceLinked(c.ID))
		for _, b := range c.Benefits {
			if b.ID == "" || seen[b.ID] {
				continue
			}
			seen[b.ID] = true
			p, ok := prev[b.ID]
			if !ok || p.IsZero() {
				p = dropstate.Row{AccountID: cfg.AccountID, DropID: b.ID, CampaignID: c.ID, Platform: cfg.Platform, Source: dropstate.FromPlatform}
			}
			var next dropstate.Row
			if expired {
				if p.IsZero() {
					continue // never tracked, nothing to expire
				}
				next = dropstate.Expire(p, now)
			} else {
				o, known := byDrop[b.ID]
				req := b.RequiredMinutes
				if known {
					req = o.Required
				}
				next = dropstate.Apply(p, dropstate.Observation{
					Known: known && o.Known, Unmineable: o.Unmineable, Claimed: o.Claimed,
					Minutes: o.Minutes, Required: req,
				}, now)
				if !linked {
					next = dropstate.BlockLink(next, now)
				}
			}
			res.Rows = append(res.Rows, next)
		}
	}
	return res, nil
}
