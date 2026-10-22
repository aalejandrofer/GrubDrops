package api

import (
	"context"
	"net/http"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"github.com/aalejandrofer/grubdrops/internal/scheduler"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
	"github.com/aalejandrofer/grubdrops/internal/timeutil"
)

// PointsFollowRaidsKey is the per-account KV flag for joining raids; unset
// means on, "0" means off.
func PointsFollowRaidsKey(accountID string) string { return "points_follow_raids:" + accountID }

// PointsFollowRaids reports whether the account joins raids (default true).
func PointsFollowRaids(ctx context.Context, q *gen.Queries, accountID string) bool {
	v, err := q.GetSettingString(ctx, PointsFollowRaidsKey(accountID))
	return err != nil || string(v) != "0"
}

// PointsOnlyKey is the per-account KV flag for points-only mode: the account
// never mines drops and only watches its selected channel-points channels.
func PointsOnlyKey(accountID string) string { return "points_only:" + accountID }

// PointsOnly reports whether the account is in points-only mode (default off).
func PointsOnly(ctx context.Context, q *gen.Queries, accountID string) bool {
	v, err := q.GetSettingString(ctx, PointsOnlyKey(accountID))
	return err == nil && string(v) == "1"
}

// pointsDeps owns /points: one place to pick the channels each Twitch/Kick
// account watches for channel points. The channel and on/off mutations reuse
// the per-account force-watch endpoints on accountsDeps.
type pointsDeps struct {
	q   *gen.Queries
	sch *scheduler.Scheduler
	t   Renderer
	sm  *scs.SessionManager
	loc *timeutil.Zone
}

type pointsAccount struct {
	ID          string
	Name        string
	Platform    string
	Enabled     bool     // channel-points mining toggle
	PointsOnly  bool     // true: never mines drops, only the selected channels
	Channels    []string // priority order; the first live one is watched
	Watching    string   // channel currently force-watched, "" when not
	AsleepUntil string   // local HH:MM the current sleep window ends, "" when awake
	Sleep       SleepWindow
	FollowRaids bool
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
	loc := time.Local
	if d.loc != nil {
		loc = d.loc.Location()
	}

	var rows []pointsAccount
	accs, _ := d.q.ListEnabledAccounts(ctx)
	for _, a := range accs {
		if a.Platform != "twitch" && a.Platform != "kick" {
			continue
		}
		row := pointsAccount{
			ID: a.ID, Name: a.DisplayName, Platform: a.Platform,
			Watching:    watching[a.ID],
			Sleep:       PointsSleepWindow(ctx, d.q, a.ID),
			FollowRaids: PointsFollowRaids(ctx, d.q, a.ID),
			PointsOnly:  PointsOnly(ctx, d.q, a.ID),
		}
		if end, asleep := row.Sleep.Until(time.Now().In(loc), a.ID); asleep {
			row.AsleepUntil = end.Format("15:04")
		}
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

// pointsSettings handles POST /accounts/:id/points-settings — saves the
// account's mode (drops + points vs points only), sleep window and (Twitch
// only) whether it follows raids. The sleep window and raids are read live; a
// mode change restarts just this account's watcher.
func (d accountsDeps) pointsSettings(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	acc, err := d.q.GetAccount(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	save := func(key, val string) bool {
		if err := d.q.UpsertSettingString(r.Context(), gen.UpsertSettingStringParams{Key: key, Value: []byte(val)}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return false
		}
		return true
	}

	sleep := PointsSleepWindow(r.Context(), d.q, id) // keeps the old times if the new ones are invalid
	sleep.Enabled = r.FormValue("sleep") == "1"
	start, ok1 := parseClock(r.FormValue("sleep_start"))
	end, ok2 := parseClock(r.FormValue("sleep_end"))
	if ok1 && ok2 && start != end {
		sleep.Start, sleep.End = start, end
	}
	if !save(PointsSleepKey(id), sleep.encode()) {
		return
	}

	wasOnly := PointsOnly(r.Context(), d.q, id)
	only := r.FormValue("mode") == "points"
	mode := "0"
	if only {
		mode = "1"
	}
	if !save(PointsOnlyKey(id), mode) {
		return
	}
	if only != wasOnly {
		d.reloadForce(id)
	}
	if acc.Platform == "twitch" {
		raids := "0"
		if r.FormValue("follow_raids") == "1" {
			raids = "1"
		}
		if !save(PointsFollowRaidsKey(id), raids) {
			return
		}
	}
	d.sm.Put(r.Context(), "flash", "flash.points_saved")
	http.Redirect(w, r, "/points", http.StatusSeeOther)
}
