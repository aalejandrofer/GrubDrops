package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/aalejandrofer/grubdrops/internal/collect"
	"github.com/aalejandrofer/grubdrops/internal/gameslug"
	"github.com/aalejandrofer/grubdrops/internal/plan"
	"github.com/aalejandrofer/grubdrops/internal/platform/twitch"
	"github.com/aalejandrofer/grubdrops/internal/store"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

const maxGameSearch = 10

type gameCampaignJSON struct {
	ID       string  `json:"id"`
	Platform string  `json:"platform"`
	Name     string  `json:"name"`
	Drops    int     `json:"drops"`
	EndsAt   *string `json:"ends_at"`
}

// needsLinkJSON is an account that must link an external account before the
// game's drops can be claimed. URL is http(s) or "".
type needsLinkJSON struct {
	AccountID   string `json:"account_id"`
	AccountName string `json:"account_name"`
	Platform    string `json:"platform"`
	URL         string `json:"url"`
}

type gameJSON struct {
	ID        string             `json:"id"`
	Name      string             `json:"name"`
	Slug      string             `json:"slug"`
	Priority  int                `json:"priority"`
	BoxArtURL string             `json:"box_art_url"`
	State     string             `json:"state"`
	Campaigns []gameCampaignJSON `json:"campaigns"`
	NeedsLink []needsLinkJSON    `json:"needs_link"`
}

type gamesJSON struct {
	Games      []gameJSON `json:"games"`
	LastScanAt *string    `json:"last_scan_at"`
	NextScanAt *string    `json:"next_scan_at"`
}

type gameHitJSON struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	BoxArtURL string `json:"box_art_url"`
	Chosen    bool   `json:"chosen"`
	Priority  *int   `json:"priority"`
}

// boxArtURL is the Twitch directory box art for a game: the IGDB id form
// when the Twitch game id is known, which works for newer games where the
// name form redirects to a placeholder.
func boxArtURL(name, twitchID string) string {
	if u := twitch.BoxArtURL("", twitchID); u != "" {
		return u
	}
	return twitch.BoxArtByName(name)
}

func (s *server) routesGames(r chi.Router) {
	r.Get("/games", s.handleGamesList)
	r.Post("/games", s.handleGamesAdd)
	r.Put("/games", s.handleGamesReorder)
	r.Get("/games/search", s.handleGamesSearch)
	r.Post("/games/scan", s.handleGamesScan)
}

func (s *server) gamesChanged() {
	if s.d.GamesChanged != nil {
		s.d.GamesChanged()
	}
}

// gamesView builds the chosen games in priority order with their live state.
func (s *server) gamesView(r *http.Request) (gamesJSON, error) {
	chosen, err := s.d.Chosen.List(r.Context())
	if err != nil {
		return gamesJSON{}, err
	}
	games := make([]plan.Game, len(chosen))
	for i, c := range chosen {
		games[i] = plan.Game{Name: c.Name, Slug: c.Slug}
	}
	var statuses []collect.GameStatus
	if s.d.Board != nil {
		statuses = s.d.Board.Games(games)
	}
	mining := map[int]bool{}
	var next time.Time
	var accts []gen.Account
	acctsOK := false
	if s.d.Q != nil {
		if a, err := s.d.Q.ListAllAccounts(r.Context()); err == nil {
			accts, acctsOK = a, true
		}
	}
	names := make(map[string]string, len(accts))
	for _, a := range accts {
		names[a.ID] = store.AccountName(a)
	}
	if s.d.Pool != nil {
		snaps := s.d.Pool.Snapshots()
		if acctsOK {
			next = earliestNextScan(snaps, pausedAccounts(accts))
		}
		for _, snap := range snaps {
			if snap.Current == nil {
				continue
			}
			if i, ok := plan.Match(games, snap.Current.Campaign.Game); ok {
				mining[i] = true
			}
		}
	}
	out := gamesJSON{Games: make([]gameJSON, len(chosen))}
	var last time.Time
	for i, c := range chosen {
		g := gameJSON{
			ID: c.ID, Name: c.Name, Slug: c.Slug, Priority: i + 1,
			BoxArtURL: boxArtURL(c.Name, c.TwitchGameID), State: "no_campaign",
			Campaigns: []gameCampaignJSON{}, NeedsLink: []needsLinkJSON{},
		}
		if i < len(statuses) {
			for _, cs := range statuses[i].Campaigns {
				cj := gameCampaignJSON{
					ID: cs.ID, Platform: cs.Platform, Name: cs.Name, Drops: cs.Drops,
					EndsAt: rfc3339(cs.EndsAt),
				}
				g.Campaigns = append(g.Campaigns, cj)
			}
			// Accounts no longer in the store are dropped.
			for _, n := range statuses[i].NeedsLink {
				name, ok := names[n.AccountID]
				if !ok {
					continue
				}
				g.NeedsLink = append(g.NeedsLink, needsLinkJSON{
					AccountID: n.AccountID, AccountName: name, Platform: n.Platform,
					URL: collect.SafeLinkURL(n.URL),
				})
			}
			if statuses[i].LastScanAt.After(last) {
				last = statuses[i].LastScanAt
			}
		}
		switch {
		case mining[i]:
			g.State = "mining"
		case i < len(statuses) && statuses[i].Queued:
			g.State = "waiting"
		case len(g.Campaigns) > 0:
			// Every drop is claimed or skipped on every account.
			g.State = "done"
		}
		out.Games[i] = g
	}
	out.LastScanAt = rfc3339(last)
	out.NextScanAt = rfc3339(next)
	return out, nil
}

func (s *server) handleGamesList(w http.ResponseWriter, r *http.Request) {
	v, err := s.gamesView(r)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal")
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// gameRowID is the games-table id for a typed name; false when the name has
// no usable slug.
func gameRowID(name string) (string, bool) {
	name = strings.TrimSpace(name)
	if name == "" || gameslug.Slug(name) == "" {
		return "", false
	}
	return gameslug.ID(name), true
}

// matchGameRow finds the stored row for a typed name. An existing row with
// the same slug but another id (seeded by a migration) wins, since slug is
// unique. It returns that row's id, or id unchanged when none matches.
func matchGameRow(all []gen.Game, id, name string) (string, bool) {
	slug := gameslug.Slug(strings.TrimSpace(name))
	for _, g := range all {
		if g.ID == id || g.Slug == slug {
			return g.ID, true
		}
	}
	return id, false
}

// insertGame adds a games row for a typed name. Stored rows keep their name
// and slug, so callers insert only when matchGameRow found none.
func (s *server) insertGame(ctx context.Context, id, name string) error {
	name = strings.TrimSpace(name)
	return s.d.Q.UpsertGame(ctx, gen.UpsertGameParams{ID: id, Name: name, Slug: gameslug.Slug(name)})
}

func (s *server) handleGamesAdd(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if plat, slug, isLink := parseCategoryLink(in.Name); isLink {
		name, ok := s.categoryName(w, r, plat, slug)
		if !ok {
			return
		}
		in.Name = name
	}
	id, ok := gameRowID(in.Name)
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad_request")
		return
	}
	all, err := s.d.Q.ListAllGames(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal")
		return
	}
	id, exists := matchGameRow(all, id, in.Name)
	chosen, err := s.d.Chosen.List(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal")
		return
	}
	ids := make([]string, 0, len(chosen)+1)
	for i, c := range chosen {
		if c.ID == id {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "conflict", "priority": i + 1})
			return
		}
		ids = append(ids, c.ID)
	}
	if !exists {
		if err := s.insertGame(r.Context(), id, in.Name); err != nil {
			writeErr(w, http.StatusInternalServerError, "internal")
			return
		}
	}
	if err := s.d.Chosen.Set(r.Context(), append(ids, id)); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal")
		return
	}
	v, err := s.gamesView(r)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal")
		return
	}
	s.gamesChanged()
	writeJSON(w, http.StatusCreated, v.Games[len(v.Games)-1])
}

func (s *server) handleGamesReorder(w http.ResponseWriter, r *http.Request) {
	var in struct {
		IDs []string `json:"ids"`
	}
	if !readJSON(w, r, &in) || !s.validGameIDs(w, r, in.IDs) {
		return
	}
	if err := s.d.Chosen.Set(r.Context(), in.IDs); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal")
		return
	}
	s.gamesChanged()
	w.WriteHeader(http.StatusNoContent)
}

// handleGamesScan asks every runner to rescan campaigns now.
func (s *server) handleGamesScan(w http.ResponseWriter, r *http.Request) {
	s.d.Pool.RescanAll()
	w.WriteHeader(http.StatusAccepted)
}

// validGameIDs writes 400 and returns false when ids is missing or names a
// game that is not in the games table.
func (s *server) validGameIDs(w http.ResponseWriter, r *http.Request, ids []string) bool {
	if ids == nil {
		writeErr(w, http.StatusBadRequest, "bad_request")
		return false
	}
	all, err := s.d.Q.ListAllGames(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal")
		return false
	}
	known := make(map[string]bool, len(all))
	for _, g := range all {
		known[g.ID] = true
	}
	for _, id := range ids {
		if !known[id] {
			writeErr(w, http.StatusBadRequest, "bad_request")
			return false
		}
	}
	return true
}

func (s *server) handleGamesSearch(w http.ResponseWriter, r *http.Request) {
	out := []gameHitJSON{}
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	if q == "" {
		writeJSON(w, http.StatusOK, out)
		return
	}
	all, err := s.d.Q.ListAllGames(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal")
		return
	}
	chosen, err := s.d.Chosen.List(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal")
		return
	}
	prio := make(map[string]int, len(chosen))
	for i, c := range chosen {
		prio[c.ID] = i + 1
	}
	for _, g := range all {
		if len(out) == maxGameSearch {
			break
		}
		if !strings.Contains(strings.ToLower(g.Name), q) {
			continue
		}
		hit := gameHitJSON{ID: g.ID, Name: g.Name, BoxArtURL: boxArtURL(g.Name, g.TwitchGameID)}
		if p, ok := prio[g.ID]; ok {
			hit.Chosen, hit.Priority = true, &p
		}
		out = append(out, hit)
	}
	writeJSON(w, http.StatusOK, out)
}
