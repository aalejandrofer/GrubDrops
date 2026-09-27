//go:build live

package store

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

// TestLive_DumpSession prints one account's decrypted session as JSON.
// Run against a COPY of the DB file: Open runs migrations. Output holds
// live tokens; do not paste it anywhere.
//
//	GRUB_DB_PATH=/tmp/miner-copy.db GRUB_MASTER_KEY=... GRUB_LIVE_ACCOUNT=acc_x \
//	  go test -tags live -run TestLive_DumpSession -v ./internal/store/
func TestLive_DumpSession(t *testing.T) {
	path, key, acct := os.Getenv("GRUB_DB_PATH"), os.Getenv("GRUB_MASTER_KEY"), os.Getenv("GRUB_LIVE_ACCOUNT")
	if path == "" || key == "" || acct == "" {
		t.Skip("set GRUB_DB_PATH, GRUB_MASTER_KEY, GRUB_LIVE_ACCOUNT")
	}
	ctx := context.Background()
	db, err := Open(ctx, path)
	require.NoError(t, err)
	defer db.Close()
	c, err := NewCryptor(key)
	require.NoError(t, err)
	s, ok, err := NewSessionStore(db, gen.New(db), c).Get(ctx, acct)
	require.NoError(t, err)
	require.True(t, ok, "no session for account")
	b, err := json.Marshal(s)
	require.NoError(t, err)
	t.Logf("%s", b)
}
