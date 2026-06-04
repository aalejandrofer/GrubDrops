package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/aalejandrofer/rust-drops-miner/internal/platform"
	"github.com/aalejandrofer/rust-drops-miner/internal/store/gen"
)

// SessionStore persists encrypted platform.Session blobs in the sessions
// table.
type SessionStore struct {
	db *sql.DB
	q  *gen.Queries
	c  *Cryptor
}

func NewSessionStore(db *sql.DB, q *gen.Queries, c *Cryptor) *SessionStore {
	return &SessionStore{db: db, q: q, c: c}
}

func (s *SessionStore) Put(ctx context.Context, accountID string, sess platform.Session) error {
	raw, err := json.Marshal(sess)
	if err != nil {
		return err
	}
	ct, err := s.c.Encrypt(raw)
	if err != nil {
		return err
	}
	return s.q.UpsertSession(ctx, gen.UpsertSessionParams{
		AccountID:  accountID,
		Ciphertext: ct,
		ExpiresAt:  sess.ExpiresAt.Unix(),
	})
}

func (s *SessionStore) Get(ctx context.Context, accountID string) (platform.Session, bool, error) {
	row, err := s.q.GetSession(ctx, accountID)
	if errors.Is(err, sql.ErrNoRows) {
		return platform.Session{}, false, nil
	}
	if err != nil {
		return platform.Session{}, false, err
	}
	raw, err := s.c.Decrypt(row.Ciphertext)
	if err != nil {
		return platform.Session{}, false, err
	}
	var sess platform.Session
	if err := json.Unmarshal(raw, &sess); err != nil {
		return platform.Session{}, false, err
	}
	return sess, true, nil
}
