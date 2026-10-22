package api

import (
	"net/http"

	"github.com/alexedwards/scs/v2"

	"github.com/aalejandrofer/grubdrops/internal/scheduler"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

// pointsDeps owns /points: one place to pick the channels each Twitch/Kick
// account watches for channel points. The mutations reuse the per-account
// force-watch endpoints on accountsDeps.
type pointsDeps struct {
	q   *gen.Queries
	sch *scheduler.Scheduler
	t   Renderer
	sm  *scs.SessionManager
}

type pointsAccount struct {
	ID       string
	Name     string
	Platform string
	Enabled  bool     // channel-points mining toggle
	Channels []string // priority order; the first live one is watched
	Watching string   // channel currently force-watched, "" when not
}

func (d *pointsDeps) get(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	watching := map[string]string{}
	if d.sch != nil {
		for _, s := range d.sch.WatcherSnapshots() {
			if s.State == "force_watch" {
				watching[s.AccountID] = s.Channel
			}
		}
	}

	var rows []pointsAccount
	accs, _ := d.q.ListEnabledAccounts(ctx)
	for _, a := range accs {
		if a.Platform != "twitch" && a.Platform != "kick" {
			continue
		}
		row := pointsAccount{ID: a.ID, Name: a.DisplayName, Platform: a.Platform, Watching: watching[a.ID]}
		if v, err := d.q.GetSettingString(ctx, ForceWatchEnabledKey(a.ID)); err == nil && string(v) == "1" {
			row.Enabled = true
		}
		if chs, err := d.q.ListForceChannels(ctx, a.ID); err == nil {
			for _, c := range chs {
				row.Channels = append(row.Channels, c.Channel)
			}
		}
		rows = append(rows, row)
	}

	render(w, r, d.t, "points.html", templateData{
		AuthedAdmin: true, CSRFToken: csrfToken(r),
		Flash: d.sm.PopString(ctx, "flash"),
		Page:  rows, Active: "points",
	})
}
