package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// apiAccountDetail serves the per-account detail modal data as JSON for the
// SPA, reusing the same projection the HTML modal renders.
func (d dashboardDeps) apiAccountDetail(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "missing id")
		return
	}
	detail, ok := d.accountDetailData(r, id)
	if !ok {
		writeAPIError(w, http.StatusNotFound, "not_found", "account not found")
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

// apiToggle flips a single account's enabled flag and returns {"ok":true}.
// JSON counterpart to the legacy redirect handler toggleEnabled.
func (d accountsDeps) apiToggle(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, err := d.doToggleEnabled(r.Context(), id); err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found", "account not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// apiReload triggers a targeted watcher reload for a single account and returns
// {"ok":true}. JSON counterpart to the legacy redirect handler reloadOne.
func (d accountsDeps) apiReload(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := d.doReloadOne(r.Context(), id); err != nil {
		if errors.Is(err, errReloadUnavailable) {
			writeAPIError(w, http.StatusServiceUnavailable, "unavailable", "reload unavailable")
			return
		}
		writeAPIError(w, http.StatusNotFound, "not_found", "account not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// apiForceWatch sets the per-account force-watch flag from a JSON body
// {"enabled":bool} and returns {"ok":true}. JSON counterpart to forceWatchToggle.
func (d accountsDeps) apiForceWatch(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var body struct {
		Enabled bool `json:"enabled"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if err := d.doForceWatch(r.Context(), id, body.Enabled); err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found", "account not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
