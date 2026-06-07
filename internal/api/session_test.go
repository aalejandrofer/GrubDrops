package api

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/store"
)

func TestKVStore_CommitFindDelete(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.db")
	db, err := store.Open(context.Background(), path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	s := NewKVSessionStore(db)

	require.NoError(t, s.Commit("tok1", []byte("payload"), time.Now().Add(time.Hour)))

	b, exists, err := s.Find("tok1")
	require.NoError(t, err)
	require.True(t, exists)
	assert.Equal(t, []byte("payload"), b)

	require.NoError(t, s.Delete("tok1"))

	_, exists, err = s.Find("tok1")
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestKVStore_ExpiredFindReturnsFalse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.db")
	db, err := store.Open(context.Background(), path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	s := NewKVSessionStore(db)
	require.NoError(t, s.Commit("tok2", []byte("p"), time.Now().Add(-1*time.Minute)))

	_, exists, err := s.Find("tok2")
	require.NoError(t, err)
	assert.False(t, exists)
}
