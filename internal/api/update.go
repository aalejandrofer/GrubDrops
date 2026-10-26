package api

import (
	"context"
	"net/http"
	"time"
)

// updateCommand is what the UI shows for applying an update.
const updateCommand = "docker compose pull && docker compose up -d"

func (s *server) updateBody(ctx context.Context) map[string]any {
	var (
		latest, notesURL string
		notes            = []string{}
		checkedAt        *string
	)
	available := false
	if s.d.Update != nil {
		var at time.Time
		var n []string
		latest, notesURL, n, at = s.d.Update.Release()
		if len(n) > 0 {
			notes = n
		}
		checkedAt = rfc3339(at)
		available, _ = s.d.Update.Status(s.d.Version)
	}
	auto := true
	if s.d.Settings != nil {
		if v, err := s.d.Settings.UpdateAuto(ctx); err == nil {
			auto = v
		}
	}
	return map[string]any{
		"current":          s.d.Version,
		"latest":           latest,
		"update_available": available,
		"checked_at":       checkedAt,
		"notes":            notes,
		"notes_url":        notesURL,
		"command":          updateCommand,
		"auto":             auto,
	}
}

func (s *server) getUpdate(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.updateBody(r.Context()))
}

// postUpdateCheck fetches the latest release now. A failed fetch keeps the
// cached answer, so the body is returned either way.
func (s *server) postUpdateCheck(w http.ResponseWriter, r *http.Request) {
	if s.d.Update != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		_ = s.d.Update.RunOnce(ctx)
	}
	writeJSON(w, http.StatusOK, s.updateBody(r.Context()))
}
