package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// apiNewAccount decodes {"platform","display_name"} and creates a new account,
// returning {"ok":true,"id":"<new-id>"} on success.
func (d accountsDeps) apiNewAccount(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Platform    string `json:"platform"`
		DisplayName string `json:"display_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid body")
		return
	}
	id, err := d.doCreateAccount(r.Context(), b.Platform, b.DisplayName)
	if err != nil {
		if errors.Is(err, errAccountFieldsRequired) {
			writeAPIError(w, http.StatusBadRequest, "bad_request", "platform and display name are required")
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": id})
}

// apiAccountDetailPage serves the per-account detail data as JSON for the SPA
// account detail page. Returns 404 JSON if the account is not found.
// Includes WebhookURL (admin-editable) but never ProxyUrl or FingerprintJson.
func (d accountsDeps) apiAccountDetailPage(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	detail, ok := d.accountDetailData(r, id)
	if !ok {
		writeAPIError(w, http.StatusNotFound, "not_found", "account not found")
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

// apiUpdateAccount decodes {"display_name","webhook_url","enabled"} and
// applies the update via doUpdateAccount, returning {"ok":true} on success.
func (d accountsDeps) apiUpdateAccount(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var body struct {
		DisplayName string `json:"display_name"`
		WebhookURL  string `json:"webhook_url"`
		Enabled     bool   `json:"enabled"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if err := d.doUpdateAccount(r.Context(), id, body.DisplayName, body.WebhookURL, body.Enabled); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	// Targeted reload under the long-lived root context (same reasoning as
	// the legacy update handler: request context cancels on response).
	if d.reloadAccount != nil {
		ctx := d.reloadCtx(r.Context())
		d.reloadAccount(ctx, id)
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// apiDeleteAccount deletes the account (mirroring the legacy delete handler)
// and fires a scheduler reload, returning {"ok":true} on success.
func (d accountsDeps) apiDeleteAccount(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := d.purgeAccount(r.Context(), id); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	d.applyReload(r.Context())
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

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

// apiCampaignDetail serves the campaign-detail modal data as JSON, reusing
// the same projection the HTML modal renders.
func (d dashboardDeps) apiCampaignDetail(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "missing id")
		return
	}
	if d.sch == nil {
		writeAPIError(w, http.StatusNotFound, "not_found", "no discovery scheduler")
		return
	}
	detail, ok := d.campaignDetailData(r, id)
	if !ok {
		writeAPIError(w, http.StatusNotFound, "not_found", "campaign not found")
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

// apiAccountGames decodes {"game_ids": []} and rewrites the per-account game
// whitelist. Returns 400 on JSON decode error (anti-wipe guard).
func (d accountsDeps) apiAccountGames(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var b struct {
		GameIDs []string `json:"game_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid body")
		return
	}
	if err := d.doSetAccountGames(r.Context(), id, b.GameIDs); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	// No auto-reload: whitelist edits take effect on the next manual Apply.
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// apiAccountGameAdd decodes {"name": "..."} and appends the game to the
// per-account whitelist.
func (d accountsDeps) apiAccountGameAdd(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var b struct {
		Name string `json:"name"`
	}
	_ = json.NewDecoder(r.Body).Decode(&b)
	if err := d.doAddAccountGame(r.Context(), id, b.Name); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	// No auto-reload: whitelist edits take effect on the next manual Apply.
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// apiAccountGamesUseGlobal clears the per-account game whitelist so the watcher
// falls back to the global priority list.
func (d accountsDeps) apiAccountGamesUseGlobal(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := d.doUseGlobalGames(r.Context(), id); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	// No auto-reload: whitelist edits take effect on the next manual Apply.
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// apiAccountChannelAdd decodes {"channel": "..."} and opts the account into
// a channel whitelist entry.
func (d accountsDeps) apiAccountChannelAdd(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var b struct {
		Channel string `json:"channel"`
	}
	_ = json.NewDecoder(r.Body).Decode(&b)
	if err := d.doAddChannel(r.Context(), id, b.Channel); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	d.applyReload(r.Context())
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// apiAccountChannelRemove decodes {"channel": "..."} and removes the channel
// from the per-account whitelist.
func (d accountsDeps) apiAccountChannelRemove(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var b struct {
		Channel string `json:"channel"`
	}
	_ = json.NewDecoder(r.Body).Decode(&b)
	if err := d.doRemoveChannel(r.Context(), id, b.Channel); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	d.applyReload(r.Context())
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// apiAccountForceChannels decodes {"channels": []} and rewrites the
// per-account force-watch channel list. Returns 400 on JSON decode error
// (anti-wipe guard).
func (d accountsDeps) apiAccountForceChannels(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var b struct {
		Channels []string `json:"channels"`
	}
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid body")
		return
	}
	if err := d.doSetForceChannels(r.Context(), id, b.Channels); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	d.applyReload(r.Context())
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// apiAccountForceChannelAdd decodes {"channel": "..."} and adds it to the
// per-account force-watch channel list.
func (d accountsDeps) apiAccountForceChannelAdd(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var b struct {
		Channel string `json:"channel"`
	}
	_ = json.NewDecoder(r.Body).Decode(&b)
	if err := d.doAddForceChannel(r.Context(), id, b.Channel); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	d.applyReload(r.Context())
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// apiAccountForceChannelRemove decodes {"channel": "..."} and removes it from
// the per-account force-watch channel list.
func (d accountsDeps) apiAccountForceChannelRemove(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var b struct {
		Channel string `json:"channel"`
	}
	_ = json.NewDecoder(r.Body).Decode(&b)
	if err := d.doRemoveForceChannel(r.Context(), id, b.Channel); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	d.applyReload(r.Context())
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
