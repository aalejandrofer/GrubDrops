package api

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/aalejandrofer/grubdrops/internal/runner"
	"github.com/aalejandrofer/grubdrops/internal/store"
)

type forceChannelJSON struct {
	Platform string   `json:"platform"`
	Channel  string   `json:"channel"`
	Live     *bool    `json:"live"`
	Accounts []string `json:"accounts"`
	Watching []string `json:"watching"`
}

type forceWatchJSON struct {
	Channels []forceChannelJSON `json:"channels"`
}

type forceWatchPut struct {
	Channels *[]struct {
		Platform string   `json:"platform"`
		Channel  string   `json:"channel"`
		Accounts []string `json:"accounts"`
	} `json:"channels"`
}

func (s *server) routesForceWatch(r chi.Router) {
	r.Get("/force-watch", s.getForceWatch)
	r.Put("/force-watch", s.putForceWatch)
}

// forceLive is the cached live state of one channel; nil = not checked yet.
func (s *server) forceLive(platform, login string) *bool {
	if s.d.ForceLive == nil {
		return nil
	}
	return s.d.ForceLive(platform, login)
}

// forceWatchers maps "platform/channel" to the IDs of accounts force
// watching it now, in snapshot order.
func forceWatchers(snaps []runner.Snapshot) map[string][]string {
	out := map[string][]string{}
	for _, sn := range snaps {
		if sn.State != runner.StateForceWatching || sn.Channel == "" {
			continue
		}
		k := sn.Platform + "/" + sn.Channel
		out[k] = append(out[k], sn.AccountID)
	}
	return out
}

func (s *server) snapshots() []runner.Snapshot {
	if s.d.Pool == nil {
		return nil
	}
	return s.d.Pool.Snapshots()
}

// forceView builds the force-watch list with live state and watchers.
func (s *server) forceView(list []store.ForceChannel) forceWatchJSON {
	watch := forceWatchers(s.snapshots())
	out := forceWatchJSON{Channels: make([]forceChannelJSON, 0, len(list))}
	for _, c := range list {
		accts := c.Accounts
		if accts == nil {
			accts = []string{}
		}
		w := watch[c.Platform+"/"+c.Channel]
		if w == nil {
			w = []string{}
		}
		out.Channels = append(out.Channels, forceChannelJSON{
			Platform: c.Platform, Channel: c.Channel,
			Live: s.forceLive(c.Platform, c.Channel), Accounts: accts, Watching: w,
		})
	}
	return out
}

func (s *server) getForceWatch(w http.ResponseWriter, r *http.Request) {
	if s.d.ForceWatch == nil {
		writeErr(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	list, err := s.d.ForceWatch.List(r.Context())
	if err != nil {
		slog.Error("force watch: list failed", "kind", "error", "err", err)
		writeErr(w, http.StatusInternalServerError, "internal")
		return
	}
	writeJSON(w, http.StatusOK, s.forceView(list))
}

// putForceWatch replaces the whole list; its order becomes the rank.
func (s *server) putForceWatch(w http.ResponseWriter, r *http.Request) {
	if s.d.ForceWatch == nil {
		writeErr(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	var body forceWatchPut
	if !readJSON(w, r, &body) {
		return
	}
	if body.Channels == nil {
		writeErr(w, http.StatusBadRequest, "bad_request")
		return
	}
	list := make([]store.ForceChannel, 0, len(*body.Channels))
	for _, c := range *body.Channels {
		list = append(list, store.ForceChannel{
			Platform: strings.ToLower(strings.TrimSpace(c.Platform)),
			Channel:  strings.ToLower(strings.TrimSpace(c.Channel)),
			Accounts: c.Accounts,
		})
	}
	ctx := r.Context()
	if err := s.d.ForceWatch.Set(ctx, list); err != nil {
		if errors.Is(err, store.ErrForceInvalid) {
			writeErr(w, http.StatusBadRequest, "bad_request")
			return
		}
		slog.Error("force watch: save failed", "kind", "error", "err", err)
		writeErr(w, http.StatusInternalServerError, "internal")
		return
	}
	if s.d.ForceChanged != nil {
		s.d.ForceChanged()
	}
	if s.d.Hub != nil {
		s.d.Hub.Publish(Event{Name: EventForceWatch, Data: struct{}{}})
	}
	saved, err := s.d.ForceWatch.List(ctx)
	if err != nil {
		slog.Error("force watch: list failed", "kind", "error", "err", err)
		writeErr(w, http.StatusInternalServerError, "internal")
		return
	}
	writeJSON(w, http.StatusOK, s.forceView(saved))
}

type forceLiveLineJSON struct {
	Platform string   `json:"platform"`
	Channel  string   `json:"channel"`
	Watchers []string `json:"watchers"`
}

type forceOverviewJSON struct {
	Live  []forceLiveLineJSON `json:"live"`
	More  int                 `json:"more"`
	Total int                 `json:"total"`
}

// forceOverviewMax is how many live channels the Home tile lists.
const forceOverviewMax = 2

// forceOverview is the Home tile block: live channels, watched ones first and
// then panel order, at most forceOverviewMax. nil when the list is empty or
// cannot be read. A channel an account is watching counts as live. usable
// reports whether any channel has an enabled account, so the list gives an
// account something to watch.
func (s *server) forceOverview(r *http.Request, snaps []runner.Snapshot, names map[string]string, paused map[string]bool) (*forceOverviewJSON, bool) {
	if s.d.ForceWatch == nil {
		return nil, false
	}
	list, err := s.d.ForceWatch.List(r.Context())
	if err != nil {
		slog.Warn("overview: force watch list failed", "kind", "error", "err", err)
		return nil, false
	}
	if len(list) == 0 {
		return nil, false
	}
	watch := forceWatchers(snaps)
	var watched, idle []forceLiveLineJSON
	usable := false
	for _, c := range list {
		for _, id := range c.Accounts {
			if _, ok := names[id]; ok && !paused[id] {
				usable = true
			}
		}
		ids := watch[c.Platform+"/"+c.Channel]
		live := s.forceLive(c.Platform, c.Channel)
		if len(ids) == 0 && (live == nil || !*live) {
			continue
		}
		line := forceLiveLineJSON{Platform: c.Platform, Channel: c.Channel, Watchers: []string{}}
		for _, id := range ids {
			if n, ok := names[id]; ok {
				line.Watchers = append(line.Watchers, n)
			}
		}
		if len(line.Watchers) > 0 {
			watched = append(watched, line)
		} else {
			idle = append(idle, line)
		}
	}
	all := append(watched, idle...)
	shown := all[:min(len(all), forceOverviewMax)]
	return &forceOverviewJSON{Live: append([]forceLiveLineJSON{}, shown...), More: len(all) - len(shown), Total: len(list)}, usable
}
