package api

import (
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
