package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/scheduler"
	"github.com/aalejandrofer/grubdrops/internal/store"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
	"github.com/aalejandrofer/grubdrops/internal/timeutil"
)

// The safety settings (sleep window, raid following) are shared: one value
// for every account on both platforms, set on the Points > Safety tab.

// pointsFollowRaidsKey is the KV flag for joining raids; unset means on, "0"
// means off. Raids only exist on Twitch.
const pointsFollowRaidsKey = "points_follow_raids"

// PointsFollowRaids reports whether raids are followed (default true).
func PointsFollowRaids(ctx context.Context, q *gen.Queries) bool {
	v, err := q.GetSettingString(ctx, pointsFollowRaidsKey)
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

// pointsTabs are the tabs of the Points page; the first is the default.
var pointsTabs = []string{"twitch", "kick", "safety"}

// pointsRedirect sends the user back to the Points tab for a platform.
func pointsRedirect(w http.ResponseWriter, r *http.Request, platform string) {
	http.Redirect(w, r, "/points?tab="+platform, http.StatusSeeOther)
}

// PointsStreamsKey is the per-account KV key for concurrent Twitch channels.
func PointsStreamsKey(accountID string) string { return "points_streams:" + accountID }

// PointsStreams returns the configured number of simultaneous channels.
// Twitch accounts default to two channels and have no app-imposed maximum.
func PointsStreams(ctx context.Context, q *gen.Queries, accountID string) int {
	if v, err := q.GetSettingString(ctx, PointsStreamsKey(accountID)); err == nil {
		if n, err := strconv.Atoi(string(v)); err == nil && n > 0 {
			return n
		}
	}
	return 2
}

// pointsDeps owns /points. Channel and on/off edits reuse the per-account
// force-watch endpoints on accountsDeps.
type pointsDeps struct {
	q        *gen.Queries
	sch      *scheduler.Scheduler
	t        Renderer
	sm       *scs.SessionManager
	loc      *timeutil.Zone
	points   *channelPointsCache
	sessions *store.SessionStore
	registry *platform.Registry
	rootCtx  context.Context
}

type pointChannel struct {
	Name            string
	Points          string
	PointsAvailable bool
}

type pointsAccount struct {
	ID          string
	Name        string
	Platform    string
	Enabled     bool // channel-points mining toggle
	PointsOnly  bool // true: never mines drops, only the selected channels
	Channels    []pointChannel
	Watching    string // channel currently force-watched, "" when not
	AsleepUntil string // local HH:MM the current sleep window ends, "" when awake
	Streams     int    // channels watched simultaneously
}

type pointsPage struct {
	Tab         string
	Accounts    []pointsAccount // accounts of the open platform tab
	Sleep       SleepWindow
	FollowRaids bool
}

func (d *pointsDeps) get(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tab := r.URL.Query().Get("tab")
	if tab != "kick" && tab != "safety" {
		tab = "twitch"
	}
	page := pointsPage{
		Tab:         tab,
		Sleep:       PointsSleepWindow(ctx, d.q),
		FollowRaids: PointsFollowRaids(ctx, d.q),
	}

	if tab != "safety" {
		watching := map[string]string{}
		if d.sch != nil {
			for _, s := range d.sch.WatcherSnapshots() {
				if s.State == "force_watch" {
					watching[s.AccountID] = s.Channel
					if len(s.ExtraChannels) > 0 {
						watching[s.AccountID] += ", " + strings.Join(s.ExtraChannels, ", ")
					}
				}
			}
		}
		loc := time.Local
		if d.loc != nil {
			loc = d.loc.Location()
		}
		var reader platform.ChannelPointsBalanceReader
		if tab == "twitch" && d.registry != nil {
			if b, ok := d.registry.Get("twitch"); ok {
				reader, _ = b.(platform.ChannelPointsBalanceReader)
			}
		}
		accs, _ := d.q.ListEnabledAccounts(ctx)
		for _, a := range accs {
			if a.Platform != tab {
				continue
			}
			row := pointsAccount{
				ID: a.ID, Name: a.DisplayName, Platform: a.Platform,
				Watching: watching[a.ID], PointsOnly: PointsOnly(ctx, d.q, a.ID),
				Streams: PointsStreams(ctx, d.q, a.ID),
			}
			if end, asleep := page.Sleep.Until(time.Now().In(loc), a.ID); asleep {
				row.AsleepUntil = end.Format("15:04")
			}
			if v, err := d.q.GetSettingString(ctx, ForceWatchEnabledKey(a.ID)); err == nil && string(v) == "1" {
				row.Enabled = true
			}
			if chs, err := d.q.ListForceChannels(ctx, a.ID); err == nil {
				for _, c := range chs {
					ch := pointChannel{Name: c.Channel}
					if reader != nil {
						if entry, ok := cachedChannelPoints(d.rootCtx, d.points, d.sessions, reader, a.ID, c.Channel); ok {
							ch.Points = strconv.Itoa(entry.Balance)
							ch.PointsAvailable = true
						}
					}
					row.Channels = append(row.Channels, ch)
				}
			}
			page.Accounts = append(page.Accounts, row)
		}
	}

	render(w, r, d.t, "points.html", templateData{
		AuthedAdmin: true, CSRFToken: csrfToken(r),
		Flash: d.sm.PopString(ctx, "flash"),
		Page:  page, Active: "points",
	})
}

// saveSafety handles POST /points/safety — saves the shared sleep window and
// raid-following flag. Both are read live, so no watcher restart is needed.
func (d *pointsDeps) saveSafety(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	save := func(key, val string) bool {
		if err := d.q.UpsertSettingString(ctx, gen.UpsertSettingStringParams{Key: key, Value: []byte(val)}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return false
		}
		return true
	}

	sleep := PointsSleepWindow(ctx, d.q) // keeps the old times if the new ones are invalid
	sleep.Enabled = r.FormValue("sleep") == "1"
	start, ok1 := parseClock(r.FormValue("sleep_start"))
	end, ok2 := parseClock(r.FormValue("sleep_end"))
	if ok1 && ok2 && start != end {
		sleep.Start, sleep.End = start, end
	}
	if !save(pointsSleepKey, sleep.encode()) {
		return
	}
	raids := "0"
	if r.FormValue("follow_raids") == "1" {
		raids = "1"
	}
	if !save(pointsFollowRaidsKey, raids) {
		return
	}
	d.sm.Put(ctx, "flash", "flash.points_saved")
	pointsRedirect(w, r, "safety")
}

// pointsMode handles POST /accounts/:id/points-mode — switches the account
// between Drops + points and Points only and updates Twitch stream count.
func (d accountsDeps) pointsMode(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	acc, err := d.q.GetAccount(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	wasOnly := PointsOnly(r.Context(), d.q, id)
	only := r.FormValue("mode") == "points"
	wasStreams, streams := 0, 0
	if acc.Platform == "twitch" {
		wasStreams = PointsStreams(r.Context(), d.q, id)
		streams = wasStreams
		if raw := r.FormValue("streams"); raw != "" {
			requested, err := strconv.Atoi(raw)
			if err != nil || requested < 1 {
				d.sm.Put(r.Context(), "flash", "flash.points_streams_invalid")
				pointsRedirect(w, r, acc.Platform)
				return
			}
			streams = requested
		}
	}
	save := func(key, val string) bool {
		if err := d.q.UpsertSettingString(r.Context(), gen.UpsertSettingStringParams{Key: key, Value: []byte(val)}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return false
		}
		return true
	}
	mode := "0"
	if only {
		mode = "1"
	}
	if !save(PointsOnlyKey(id), mode) {
		return
	}
	changed := only != wasOnly
	if acc.Platform == "twitch" {
		if !save(PointsStreamsKey(id), strconv.Itoa(streams)) {
			return
		}
		changed = changed || streams != wasStreams
	}
	if changed {
		d.reloadForce(id)
	}
	d.sm.Put(r.Context(), "flash", "flash.points_saved")
	pointsRedirect(w, r, acc.Platform)
}
