package api

import (
	"encoding/json"
	"net/http"
)

func (d *settingsDeps) apiSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, d.settingsViewData(r))
}

func (d *settingsDeps) apiGlobalGamesOrder(w http.ResponseWriter, r *http.Request) {
	var b struct {
		GameIDs []string `json:"game_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid body")
		return
	}
	if err := d.doSetGlobalGamesOrder(r.Context(), b.GameIDs); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if d.onUpdate != nil {
		d.onUpdate()
	}
	d.applyReload(r.Context())
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (d *settingsDeps) apiGlobalGamesAdd(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Name string `json:"name"`
	}
	_ = json.NewDecoder(r.Body).Decode(&b)
	if err := d.doAddGlobalGame(r.Context(), b.Name); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if d.onUpdate != nil {
		d.onUpdate()
	}
	d.applyReload(r.Context())
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (d *settingsDeps) apiGeneral(w http.ResponseWriter, r *http.Request) {
	var b struct {
		LogRetentionDays     int    `json:"log_retention_days"`
		LogLevel             string `json:"log_level"`
		TickIntervalSec      int    `json:"tick_interval_sec"`
		DiscoveryIntervalMin int    `json:"discovery_interval_min"`
	}
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid body")
		return
	}
	changed, err := d.doSaveGeneral(r.Context(), b.LogRetentionDays, b.LogLevel, b.TickIntervalSec, b.DiscoveryIntervalMin)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if d.onUpdate != nil {
		d.onUpdate()
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true, "intervals_changed": changed})
}

func (d *settingsDeps) apiPriorityMode(w http.ResponseWriter, r *http.Request) {
	var b struct {
		PriorityMode string `json:"priority_mode"`
	}
	_ = json.NewDecoder(r.Body).Decode(&b)
	if err := d.doSetPriorityMode(r.Context(), b.PriorityMode); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if d.onUpdate != nil {
		d.onUpdate()
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
