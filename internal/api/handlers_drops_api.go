package api

import (
	"encoding/json"
	"net/http"
	"strings"
)

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

// apiAddWhitelist handles POST /api/drops/whitelist/add — JSON variant of
// addWhitelist. Expects {"account_id":"...","name":"..."} body.
func (d *dropsDeps) apiAddWhitelist(w http.ResponseWriter, r *http.Request) {
	var b struct {
		AccountID string `json:"account_id"`
		Name      string `json:"name"`
	}
	_ = json.NewDecoder(r.Body).Decode(&b)
	if err := d.doAddWhitelist(r.Context(), b.AccountID, strings.TrimSpace(b.Name)); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if d.reload != nil {
		_ = d.reload(r.Context())
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// apiAddChannelWhitelist handles POST /api/drops/whitelist/channel — JSON
// variant of addChannelWhitelist. Expects {"account_id":"...","channels":[...]} body.
func (d *dropsDeps) apiAddChannelWhitelist(w http.ResponseWriter, r *http.Request) {
	var b struct {
		AccountID string   `json:"account_id"`
		Channels  []string `json:"channels"`
	}
	_ = json.NewDecoder(r.Body).Decode(&b)
	added, err := d.doAddChannelWhitelist(r.Context(), b.AccountID, b.Channels)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if added > 0 && d.reload != nil {
		_ = d.reload(r.Context())
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// apiRemoveChannelWhitelist handles POST /api/drops/whitelist/channel/remove —
// JSON variant of removeChannelWhitelist. Expects {"account_id":"...","channels":[...]} body.
func (d *dropsDeps) apiRemoveChannelWhitelist(w http.ResponseWriter, r *http.Request) {
	var b struct {
		AccountID string   `json:"account_id"`
		Channels  []string `json:"channels"`
	}
	_ = json.NewDecoder(r.Body).Decode(&b)
	removed, err := d.doRemoveChannelWhitelist(r.Context(), b.AccountID, b.Channels)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if removed > 0 && d.reload != nil {
		_ = d.reload(r.Context())
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// apiMarkLinked handles POST /api/drops/link — JSON variant of markLinked.
// Expects {"campaign_id":"...","unlink":false} body.
func (d *dropsDeps) apiMarkLinked(w http.ResponseWriter, r *http.Request) {
	var b struct {
		CampaignID string `json:"campaign_id"`
		Unlink     bool   `json:"unlink"`
	}
	_ = json.NewDecoder(r.Body).Decode(&b)
	if err := d.doMarkLinked(r.Context(), strings.TrimSpace(b.CampaignID), b.Unlink); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if d.reload != nil {
		_ = d.reload(r.Context())
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
