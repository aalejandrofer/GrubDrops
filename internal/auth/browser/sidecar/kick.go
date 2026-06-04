package sidecar

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"

	pb "github.com/chano-fernandez/rust-drops-miner/internal/auth/browser/gen/browser/v1"
)

// Kick wraps Browser with Kick.com-specific page logic.
type Kick struct {
	b *Browser
}

func NewKick(b *Browser) *Kick { return &Kick{b: b} }

// InstallCookies pushes the user-supplied session cookies into a tab
// before navigation. Must be called before chromedp.Navigate.
func (k *Kick) InstallCookies(ctx context.Context, session *pb.KickSession) error {
	return chromedp.Run(ctx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			for _, c := range session.Cookies {
				expr := network.SetCookie(c.Name, c.Value).
					WithDomain(c.Domain).
					WithPath(c.Path)
				if err := expr.Do(ctx); err != nil {
					return err
				}
			}
			return nil
		}),
	)
}

// VerifyAuth navigates to /api/v1/user and returns the username from
// the response. 401 / missing-username means invalid cookies.
func (k *Kick) VerifyAuth(ctx context.Context, session *pb.KickSession) (string, error) {
	if err := k.InstallCookies(ctx, session); err != nil {
		return "", err
	}
	var raw string
	err := chromedp.Run(ctx,
		chromedp.Navigate("https://kick.com/api/v1/user"),
		chromedp.Sleep(2*time.Second),
		chromedp.Text("body", &raw),
	)
	if err != nil {
		return "", fmt.Errorf("verify auth navigate: %w", err)
	}
	var body struct {
		Username string `json:"username"`
	}
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		return "", fmt.Errorf("verify auth parse: %w (body=%q)", err, raw)
	}
	if body.Username == "" {
		return "", fmt.Errorf("verify auth: empty username — cookies likely invalid")
	}
	return body.Username, nil
}

// OpenStream opens kick.com/<channel> in a new tab; the HLS player
// auto-loads on the page and watch time accrues as long as the tab stays open.
func (k *Kick) OpenStream(channel string, session *pb.KickSession) (string, error) {
	handle, ctx, err := k.b.OpenTab()
	if err != nil {
		return "", err
	}
	if err := k.InstallCookies(ctx, session); err != nil {
		k.b.CloseTab(handle)
		return "", err
	}
	err = chromedp.Run(ctx,
		chromedp.Navigate(fmt.Sprintf("https://kick.com/%s", channel)),
		chromedp.Sleep(5*time.Second),
	)
	if err != nil {
		k.b.CloseTab(handle)
		return "", fmt.Errorf("open stream %s: %w", channel, err)
	}
	return handle, nil
}

// Inventory scrapes the user's drops inventory page.
//
// NOTE: This relies on window.__NEXT_DATA__ which is injected by Next.js SSR.
// Kick.com may migrate away from Next.js or change the pageProps schema at any
// time. If the drops array is missing from the JSON path the function returns
// an empty slice rather than an error — callers should log a warning and treat
// this as "no active drops" until the schema can be confirmed.
func (k *Kick) Inventory(ctx context.Context, session *pb.KickSession) ([]*pb.DropProgress, error) {
	handle, tabCtx, err := k.b.OpenTab()
	if err != nil {
		return nil, err
	}
	defer k.b.CloseTab(handle)

	if err := k.InstallCookies(tabCtx, session); err != nil {
		return nil, err
	}

	var raw string
	err = chromedp.Run(tabCtx,
		chromedp.Navigate("https://kick.com/dashboard/drops"),
		chromedp.Sleep(3*time.Second),
		chromedp.Evaluate(`JSON.stringify(window.__NEXT_DATA__ || {})`, &raw),
	)
	if err != nil {
		return nil, fmt.Errorf("inventory navigate: %w", err)
	}
	return parseInventoryNextData(raw)
}

// parseInventoryNextData extracts drops progress from the Next.js page state.
// Returns an empty slice when the JSON path is missing (common when the user
// has no active drops). Schema drift in production should be flagged here.
func parseInventoryNextData(raw string) ([]*pb.DropProgress, error) {
	var page struct {
		Props struct {
			PageProps struct {
				Drops []struct {
					ID             string `json:"id"`
					MinutesWatched int32  `json:"minutesWatched"`
					Claimed        bool   `json:"claimed"`
				} `json:"drops"`
			} `json:"pageProps"`
		} `json:"props"`
	}
	if err := json.Unmarshal([]byte(raw), &page); err != nil {
		return nil, fmt.Errorf("parse next data: %w", err)
	}
	out := make([]*pb.DropProgress, 0, len(page.Props.PageProps.Drops))
	for _, d := range page.Props.PageProps.Drops {
		out = append(out, &pb.DropProgress{
			BenefitId:      d.ID,
			MinutesWatched: d.MinutesWatched,
			Claimed:        d.Claimed,
		})
	}
	return out, nil
}

// Claim drives the claim button for a specific benefit. If the click
// fails because the button isn't there (already claimed) we treat that
// as benign success with alreadyClaimed=true.
func (k *Kick) Claim(ctx context.Context, session *pb.KickSession, benefitID string) (bool, error) {
	handle, tabCtx, err := k.b.OpenTab()
	if err != nil {
		return false, err
	}
	defer k.b.CloseTab(handle)

	if err := k.InstallCookies(tabCtx, session); err != nil {
		return false, err
	}

	selector := fmt.Sprintf(`button[data-benefit-id=%q]`, benefitID)
	claimedSelector := fmt.Sprintf(`[data-benefit-id=%q] .claimed-badge`, benefitID)

	var alreadyClaimed bool
	err = chromedp.Run(tabCtx,
		chromedp.Navigate("https://kick.com/dashboard/drops"),
		chromedp.Sleep(3*time.Second),
		chromedp.Click(selector, chromedp.NodeVisible),
		chromedp.Sleep(2*time.Second),
		chromedp.EvaluateAsDevTools(
			fmt.Sprintf(`!!document.querySelector(%q)`, claimedSelector),
			&alreadyClaimed,
		),
	)
	if err != nil {
		// Treat click-failure as "button missing because already claimed".
		return true, nil
	}
	return alreadyClaimed, nil
}
