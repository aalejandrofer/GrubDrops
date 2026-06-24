package api

import "net/http"

// apiDrops serves the /drops page model as JSON for the SPA, reusing the
// same per-tab assembly the HTML page renders.
func (d *dropsDeps) apiDrops(w http.ResponseWriter, r *http.Request) {
	page, err := d.dropsPageData(r)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, page)
}
