package api

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/aalejandrofer/grubdrops/internal/store"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

// ageKey is a throwaway age identity for encrypting stored sessions in tests.
const ageKey = "AGE-SECRET-KEY-1DZCAXYWJM6M42NSX5GR4QWZZ2JXEYKJ9ZKWYFYSNU997775JJ6XSY85FK9"

// newKickLoginDeps spins up a migrated sqlite store + a kick account and
// returns deps wired for the Kick login handlers.
func newKickLoginDeps(t *testing.T, accID string) (*loginKickDeps, *store.SessionStore) {
	t.Helper()
	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	q := gen.New(db)
	if accID != "" {
		now := time.Now().Unix()
		if _, err := q.CreateAccount(context.Background(), gen.CreateAccountParams{
			ID: accID, Platform: "kick", DisplayName: "TTik3r",
			Status: "idle", FingerprintJson: "{}", Enabled: 1,
			CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("seed account: %v", err)
		}
	}
	cr, err := store.NewCryptor(ageKey)
	if err != nil {
		t.Fatalf("cryptor: %v", err)
	}
	ss := store.NewSessionStore(db, q, cr)
	return &loginKickDeps{q: q, sessions: ss}, ss
}

// parseKickChannels must accept the various separator styles operators
// paste into the form. The web template advertises "comma/space-
// separated"; the helper CLI joins with commas. Both must round-trip.
func TestParseKickChannels(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"empty", "", nil},
		{"single", "alice", []string{"alice"}},
		{"csv", "alice,bob,carol", []string{"alice", "bob", "carol"}},
		{"spaces", "alice bob carol", []string{"alice", "bob", "carol"}},
		{"mixed", "alice, bob; carol\tdave", []string{"alice", "bob", "carol", "dave"}},
		{"dedupe", "Alice,alice,ALICE,bob", []string{"Alice", "bob"}},
		{"trim", "  alice  ,  bob  ", []string{"alice", "bob"}},
		{"empty parts", ",,alice,,,bob,,", []string{"alice", "bob"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, parseKickChannels(tc.in))
		})
	}
}
