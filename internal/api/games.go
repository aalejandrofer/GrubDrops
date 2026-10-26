package api

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/aalejandrofer/grubdrops/internal/collect"
	"github.com/aalejandrofer/grubdrops/internal/gameslug"
	"github.com/aalejandrofer/grubdrops/internal/plan"
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

type gameJSON struct {
	ID        string             `json:"id"`
	Name      string             `json:"name"`
	Slug      string             `json:"slug"`
	Priority  int                `json:"priority"`
	BoxArtURL string             `json:"box_art_url"`
	State     string             `json:"state"`
	Campaigns []gameCampaignJSON `json:"campaigns"`
}

type gamesJSON struct {
	Games      []gameJSON `json:"games"`
	LastScanAt *string    `json:"last_scan_at"`
	NextScanAt *string    `json:"next_scan_at"`
}

type gameHitJSON struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Chosen   bool   `json:"chosen"`
	Priority *int   `json:"priority"`
}

// boxArtURL is the Twitch directory box art for a game. The ID form works for
// newer games where the name form redirects to a placeholder.
func boxArtURL(name, twitchID string) string {
	if twitchID != "" {
		return "https://static-cdn.jtvnw.net/ttv-boxart/" + url.PathEscape(twitchID) + "-144x192.jpg"
	}
	return "https://static-cdn.jtvnw.net/ttv-boxart/" + url.PathEscape(name) + "-144x192.jpg"
}

func (s *server) routesGames(r chi.Router) {
	r.Get("/games", s.handleGamesList)
	r.Post("/games", s.handleGamesAdd)
	r.Put("/games", s.handleGamesReorder)
	r.Get("/games/search", s.handleGamesSearch)
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
	if s.d.Pool != nil {
		snaps := s.d.Pool.Snapshots()
		if s.d.Q != nil {
			if accts, err := s.d.Q.ListAllAccounts(r.Context()); err == nil {
				next = earliestNextScan(snaps, pausedAccounts(accts))
			}
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
			Campaigns: []gameCampaignJSON{},
		}
		if i < len(statuses) {
			for _, cs := range statuses[i].Campaigns {
				cj := gameCampaignJSON{
					ID: cs.ID, Platform: cs.Platform, Name: cs.Name, Drops: cs.Drops,
					EndsAt: rfc3339(cs.EndsAt),
				}
				g.Campaigns = append(g.Campaigns, cj)
			}
			if statuses[i].LastScanAt.After(last) {
				last = statuses[i].LastScanAt
			}
		}
		switch {
		case mining[i]:
			g.State = "mining"
		case len(g.Campaigns) > 0:
			g.State = "waiting"
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

func (s *server) handleGamesAdd(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	name := strings.TrimSpace(in.Name)
	id := gameslug.ID(name)
	slug := gameslug.Slug(name)
	if name == "" || slug == "" {
		writeErr(w, http.StatusBadRequest, "bad_request")
		return
	}
	// Keep the stored name and slug of an existing row; insert only when missing.
	// An existing row with the same slug but another id (seeded by a migration)
	// wins, since slug is unique.
	all, err := s.d.Q.ListAllGames(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal")
		return
	}
	exists := false
	for _, g := range all {
		if g.ID == id || g.Slug == slug {
			id = g.ID
			exists = true
			break
		}
	}
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
		if err := s.d.Q.UpsertGame(r.Context(), gen.UpsertGameParams{ID: id, Name: name, Slug: slug}); err != nil {
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
		hit := gameHitJSON{ID: g.ID, Name: g.Name}
		if p, ok := prio[g.ID]; ok {
			hit.Chosen, hit.Priority = true, &p
		}
		out = append(out, hit)
	}
	writeJSON(w, http.StatusOK, out)
}
