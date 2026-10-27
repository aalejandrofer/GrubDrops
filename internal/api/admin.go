package api

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/aalejandrofer/grubdrops/internal/auth"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

func (s *server) routesAdmin(r chi.Router) {
	r.Post("/settings/password", s.changePassword)
	r.Post("/settings/snapshot", s.postSnapshot)
}

// changePassword checks the current password, then sets a new one. Every
// other session ends; this one stays signed in on a fresh token.
func (s *server) changePassword(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Current string `json:"current"`
		New     string `json:"new"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	ctx := r.Context()
	admin, err := s.d.Q.GetAdmin(ctx)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal")
		return
	}
	if auth.VerifyPassword(admin.PasswordHash, in.Current) != nil {
		writeErr(w, http.StatusBadRequest, "wrong_password")
		return
	}
	if len(in.New) < minPasswordLen {
		writeErr(w, http.StatusBadRequest, "password_short")
		return
	}
	hash, err := auth.HashPassword(in.New)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request")
		return
	}
	if err := s.d.Q.UpsertAdmin(ctx, gen.UpsertAdminParams{PasswordHash: hash, CreatedAt: admin.CreatedAt}); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal")
		return
	}
	if err := s.d.Session.RenewToken(ctx); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal")
		return
	}
	if s.d.DB != nil {
		if err := deleteAllSessions(s.d.DB); err != nil {
			slog.Warn("password change: ending other sessions failed", "kind", "error", "err", err)
		}
	}
	s.d.Session.Put(ctx, "admin_authed", true)
	w.WriteHeader(http.StatusNoContent)
}

// writeSnapshot writes a consistent copy of the live database to dest with
// VACUUM INTO. A plain file copy while the miner runs can catch a torn WAL
// state. dest must not exist yet: VACUUM INTO refuses to overwrite.
func writeSnapshot(ctx context.Context, db *sql.DB, dest string) error {
	// dest comes from os.MkdirTemp plus a fixed name, never from the request.
	if _, err := db.ExecContext(ctx, "VACUUM INTO "+quoteSQLiteString(dest)); err != nil {
		return fmt.Errorf("vacuum into: %w", err)
	}
	return nil
}

// quoteSQLiteString renders s as a SQLite string literal. VACUUM INTO takes a
// literal path, not a bound parameter.
func quoteSQLiteString(s string) string {
	out := make([]rune, 0, len(s)+2)
	out = append(out, '\'')
	for _, r := range s {
		if r == '\'' {
			out = append(out, '\'')
		}
		out = append(out, r)
	}
	return string(append(out, '\''))
}

// postSnapshot streams a consistent copy of the database as a download. It is
// a POST so CSRF covers it: it reads, but sends every stored secret out.
//
// The store runs on one connection, so VACUUM INTO briefly stalls other DB
// work. Fine for a manual action on a database of a few megabytes; revisit
// before running it on a timer.
func (s *server) postSnapshot(w http.ResponseWriter, r *http.Request) {
	if s.d.DB == nil {
		writeErr(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	dir, err := os.MkdirTemp("", "grubdrops-snapshot-")
	if err != nil {
		slog.Warn("snapshot: temp dir failed", "kind", "error", "err", err)
		writeErr(w, http.StatusInternalServerError, "internal")
		return
	}
	// Removing the directory also drops any -wal or -shm file next to the copy.
	defer func() { _ = os.RemoveAll(dir) }()

	name := "grubdrops-" + s.overviewNow().In(s.overviewZone()).Format("20060102-1504") + ".db"
	dest := filepath.Join(dir, name)
	if err := writeSnapshot(r.Context(), s.d.DB, dest); err != nil {
		slog.Warn("snapshot failed", "kind", "error", "err", err)
		writeErr(w, http.StatusInternalServerError, "internal")
		return
	}
	f, err := os.Open(dest)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal")
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal")
		return
	}
	w.Header().Set("Content-Type", "application/x-sqlite3")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Content-Length", strconv.FormatInt(fi.Size(), 10))
	if _, err := io.Copy(w, f); err != nil {
		slog.Debug("snapshot: stream interrupted", "err", err)
	}
}
