package api

import (
	"database/sql"
	"encoding/binary"
	"errors"
	"time"

	"github.com/alexedwards/scs/v2"
)

// kvSessionStore stores session blobs in the existing kv table.
// Keys are prefixed with "session:" to namespace from other kv uses.
// Value layout: [8 bytes big-endian unix nano expiry][N bytes session payload].
type kvSessionStore struct {
	db *sql.DB
}

// compile-time guarantee that kvSessionStore implements scs.Store.
var _ scs.Store = (*kvSessionStore)(nil)

func NewKVSessionStore(db *sql.DB) scs.Store {
	return &kvSessionStore{db: db}
}

const sessionPrefix = "session:"

func (s *kvSessionStore) Find(token string) ([]byte, bool, error) {
	var raw []byte
	err := s.db.QueryRow(`SELECT value FROM kv WHERE key = ?`, sessionPrefix+token).Scan(&raw)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, false, nil
	case err != nil:
		return nil, false, err
	}
	if len(raw) < 8 {
		return nil, false, nil
	}
	exp := time.Unix(0, int64(binary.BigEndian.Uint64(raw[:8])))
	if time.Now().After(exp) {
		_, _ = s.db.Exec(`DELETE FROM kv WHERE key = ?`, sessionPrefix+token)
		return nil, false, nil
	}
	return raw[8:], true, nil
}

func (s *kvSessionStore) Commit(token string, b []byte, expiry time.Time) error {
	buf := make([]byte, 8+len(b))
	binary.BigEndian.PutUint64(buf[:8], uint64(expiry.UnixNano()))
	copy(buf[8:], b)
	_, err := s.db.Exec(
		`INSERT INTO kv (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		sessionPrefix+token, buf,
	)
	return err
}

func (s *kvSessionStore) Delete(token string) error {
	_, err := s.db.Exec(`DELETE FROM kv WHERE key = ?`, sessionPrefix+token)
	return err
}

func (s *kvSessionStore) All() (map[string][]byte, error) {
	rows, err := s.db.Query(`SELECT key, value FROM kv WHERE key LIKE ?`, sessionPrefix+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]byte{}
	for rows.Next() {
		var k string
		var v []byte
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		if len(v) < 8 {
			continue
		}
		out[k[len(sessionPrefix):]] = v[8:]
	}
	return out, rows.Err()
}
