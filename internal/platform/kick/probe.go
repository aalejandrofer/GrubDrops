package kick

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// ProbeClaim POSTs a real /drops/claim for the given reward+campaign and dumps
// the raw status + body, so we can verify the claim endpoint/payload live
// (Kick appears to auto-grant at 100%, so claiming an already-granted reward is
// the safe way to confirm the endpoint exists + the body parses without
// changing inventory). One-shot ops tool.
func ProbeClaim(ctx context.Context, sess platform.Session, rewardID, campaignID string) {
	d := newHTTPDoer(nil)
	payload := []byte(fmt.Sprintf(`{"reward_id":%q,"campaign_id":%q}`, rewardID, campaignID))
	fmt.Printf("POST %s/api/v1/drops/claim  body=%s\n", dropsBase, payload)
	body, status, err := d.do(ctx, sess, http.MethodPost, dropsBase+"/api/v1/drops/claim", payload)
	if err != nil {
		fmt.Printf("ERR: %v\n", err)
		return
	}
	fmt.Printf("status: %d  body: %s\n", status, string(body))
}

// Probe is a one-shot diagnostic that hits the live Kick endpoints with a real
// authed session and dumps raw status + body for each, so we can verify the
// authed response shapes (campaigns/progress field names, the connect_url, the
// list of eligible channels) and tell "not enrolled" (403 on /drops/progress)
// apart from "empty 200". Wired only into cmd/kick-probe; not used by the
// daemon. Output goes to stdout.
func Probe(ctx context.Context, sess platform.Session, categorySlug string) {
	d := newHTTPDoer(nil)
	a := newAPI()

	dump := func(label, base, path string) {
		fmt.Printf("\n===== %s\nGET %s%s\n", label, base, path)
		body, status, err := d.do(ctx, sess, http.MethodGet, base+path, nil)
		if err != nil {
			fmt.Printf("ERR: %v\n", err)
			return
		}
		fmt.Printf("status: %d  bytes: %d\n", status, len(body))
		// Full body for the small/decisive ones; cap the campaigns dump so a
		// huge payload stays legible while still showing the shape.
		max := 8000
		if len(body) > max {
			fmt.Printf("body[:%d]:\n%s\n…(truncated)\n", max, string(body[:max]))
		} else {
			fmt.Printf("body:\n%s\n", string(body))
		}
	}

	fmt.Fprintf(os.Stderr, "probing kick with session (account=%s, cookies=%d)\n", sess.AccountID, len(sess.Cookies))

	// 1) Authed: the drops campaigns shape (real field names, channels[], connect_url).
	dump("DROPS CAMPAIGNS (authed)", dropsBase, "/api/v1/drops/campaigns")
	// 2) Authed: progress — 403 means not enrolled/linked; 200 empty means enrolled-no-time.
	dump("DROPS PROGRESS (authed)", dropsBase, "/api/v1/drops/progress")

	// 2b) PARSED view: exactly what the DAEMON sees per campaign — the
	// channel slugs it can choose from, plus the parsed progress rows. This
	// is the ground-truth for "why did it pick channel X".
	fmt.Printf("\n===== PARSED CAMPAIGNS (what the daemon sees)\n")
	camps, cerr := a.Campaigns(ctx, sess)
	if cerr != nil {
		fmt.Printf("Campaigns() ERR: %v\n", cerr)
	} else {
		for _, c := range camps {
			slugs := make([]string, 0, len(c.Channels))
			for _, ch := range c.Channels {
				slugs = append(slugs, fmt.Sprintf("%s(id=%s)", ch.Slug, ch.ID))
			}
			fmt.Printf("- campaign %q  game=%q status=%q channels=%v rewards=%d\n",
				c.Name, c.Game, c.Status, slugs, len(c.Rewards))
			for _, r := range c.Rewards {
				fmt.Printf("    reward id=%s name=%q required=%d\n", r.ID, r.Name, r.RequiredMinutes)
			}
		}
	}

	fmt.Printf("\n===== PARSED PROGRESS (what the daemon sees)\n")
	prog, perr := a.Progress(ctx, sess)
	if perr != nil {
		fmt.Printf("Progress() ERR: %v\n", perr)
	} else if len(prog) == 0 {
		fmt.Printf("(empty — no progress rows parsed)\n")
	} else {
		for _, p := range prog {
			fmt.Printf("- benefit=%s minutes=%d claimed=%v\n", p.BenefitID, p.MinutesWatched, p.Claimed)
		}
	}

	// 3) Public: the generic category directory feed (tier-2 discovery on the
	// OLD code; this is where junk like pr8isegod/l-busch-l came from).
	if categorySlug != "" {
		dump("CATEGORY LIVESTREAMS (public, generic directory feed)", discoveryBase, "/stream/livestreams/"+categorySlug)
	}

	// 4) Liveness of each campaign channel + a couple known participating
	// channels (oilrats, the official "kick") — exactly the ChannelLivestream
	// probe ListEligibleChannels runs. Shows which campaign channels are live
	// + their live category, so we can see why selection fell through.
	probeSet := map[string]struct{}{"oilrats": {}, "kick": {}}
	for _, c := range camps {
		for _, ch := range c.Channels {
			if ch.Slug != "" {
				probeSet[ch.Slug] = struct{}{}
			}
		}
	}
	fmt.Printf("\n===== CHANNEL LIVENESS (campaign channels + oilrats/kick)\n")
	for slug := range probeSet {
		live, lsID, viewers, category, lerr := a.ChannelLivestream(ctx, sess, slug)
		if lerr != nil {
			fmt.Printf("- %-16s ERR: %v\n", slug, lerr)
			continue
		}
		fmt.Printf("- %-16s live=%-5v category=%q viewers=%d livestreamID=%s\n", slug, live, category, viewers, lsID)
	}
}
