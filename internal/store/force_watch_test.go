package store

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

func seedPlatformAccount(t *testing.T, q *gen.Queries, id, platform string) {
	t.Helper()
	now := time.Now().Unix()
	_, err := q.CreateAccount(context.Background(), gen.CreateAccountParams{
		ID: id, Platform: platform, DisplayName: id, Status: "idle",
		FingerprintJson: "{}", Enabled: 1, CreatedAt: now, UpdatedAt: now,
	})
	require.NoError(t, err)
}

func forceFixture(t *testing.T) (*ForceWatch, *gen.Queries, *sql.DB) {
	t.Helper()
	db, q := openV2(t)
	seedPlatformAccount(t, q, "a1", "twitch")
	seedPlatformAccount(t, q, "a2", "kick")
	seedPlatformAccount(t, q, "a3", "twitch")
	seedPlatformAccount(t, q, "a4", "twitch")
	return NewForceWatch(db, q), q, db
}

var forceTwoChannels = []ForceChannel{
	{Platform: "twitch", Channel: "keshaeuw", Accounts: []string{"a1", "a3"}},
	{Platform: "kick", Channel: "xqc", Accounts: []string{"a2"}},
}

func TestForceWatch_SetListRoundTrip(t *testing.T) {
	f, _, _ := forceFixture(t)
	ctx := context.Background()
	require.NoError(t, f.Set(ctx, forceTwoChannels))
	got, err := f.List(ctx)
	require.NoError(t, err)
	require.Equal(t, forceTwoChannels, got)
}

func TestForceWatch_SetReplaces(t *testing.T) {
	f, _, _ := forceFixture(t)
	ctx := context.Background()
	require.NoError(t, f.Set(ctx, forceTwoChannels))
	one := []ForceChannel{{Platform: "kick", Channel: "xqc", Accounts: []string{"a2"}}}
	require.NoError(t, f.Set(ctx, one))
	got, err := f.List(ctx)
	require.NoError(t, err)
	require.Equal(t, one, got)
}

func TestForceWatch_SetRejects(t *testing.T) {
	f, _, _ := forceFixture(t)
	ctx := context.Background()
	require.NoError(t, f.Set(ctx, forceTwoChannels))

	cases := map[string][]ForceChannel{
		"platform mismatch": {{Platform: "kick", Channel: "xqc", Accounts: []string{"a1"}}},
		"duplicate":         {{Platform: "twitch", Channel: "a_b", Accounts: nil}, {Platform: "twitch", Channel: "a_b", Accounts: nil}},
		"bad name":          {{Platform: "twitch", Channel: "Bad Name", Accounts: nil}},
		"twitch hyphen":     {{Platform: "twitch", Channel: "a-b", Accounts: nil}},
		"bad platform":      {{Platform: "youtube", Channel: "xqc", Accounts: nil}},
	}
	for name, list := range cases {
		err := f.Set(ctx, list)
		require.ErrorIs(t, err, ErrForceInvalid, name)
		got, lerr := f.List(ctx)
		require.NoError(t, lerr)
		require.Equal(t, forceTwoChannels, got, name)
	}

	err := f.Set(ctx, []ForceChannel{{Platform: "twitch", Channel: "xqc", Accounts: []string{"nope"}}})
	require.ErrorIs(t, err, ErrForceInvalid, "unknown account")
	got, lerr := f.List(ctx)
	require.NoError(t, lerr)
	require.Equal(t, forceTwoChannels, got)

	// Kick slugs may contain hyphens.
	require.NoError(t, f.Set(ctx, []ForceChannel{{Platform: "kick", Channel: "a-b", Accounts: []string{"a2"}}}))
}

func TestForceWatch_ForAccount(t *testing.T) {
	f, _, _ := forceFixture(t)
	ctx := context.Background()
	require.NoError(t, f.Set(ctx, forceTwoChannels))
	got, err := f.ForAccount(ctx, "a3")
	require.NoError(t, err)
	require.Equal(t, []string{"keshaeuw"}, got)
	got, err = f.ForAccount(ctx, "a4")
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Empty(t, got)
}

func TestForceWatch_CascadeOnAccountDelete(t *testing.T) {
	f, q, _ := forceFixture(t)
	ctx := context.Background()
	require.NoError(t, f.Set(ctx, forceTwoChannels))
	require.NoError(t, q.DeleteAccount(ctx, "a1"))
	got, err := f.List(ctx)
	require.NoError(t, err)
	require.Equal(t, []ForceChannel{
		{Platform: "twitch", Channel: "keshaeuw", Accounts: []string{"a3"}},
		{Platform: "kick", Channel: "xqc", Accounts: []string{"a2"}},
	}, got)
}

func TestMigration0023_CarriesOver1x(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "m.db")
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)", path)
	db, err := sql.Open("sqlite", dsn)
	require.NoError(t, err)
	defer db.Close()
	db.SetMaxOpenConns(1)

	goose.SetBaseFS(migrationsFS)
	require.NoError(t, goose.SetDialect("sqlite3"))
	require.NoError(t, goose.UpToContext(ctx, db, "migrations", 22))

	exec := func(q string, args ...any) {
		_, e := db.ExecContext(ctx, q, args...)
		require.NoError(t, e)
	}
	now := time.Now().Unix()
	for _, a := range [][2]string{{"tw1", "twitch"}, {"tw2", "twitch"}, {"kk1", "kick"}} {
		exec(`INSERT INTO accounts (id,platform,display_name,status,fingerprint_json,enabled,created_at,updated_at) VALUES (?,?,?,'idle','{}',1,?,?)`, a[0], a[1], a[0], now, now)
	}
	exec(`INSERT INTO kv (key,value) VALUES ('force_watch:tw1','1'),('force_watch:kk1','1')`)
	exec(`INSERT INTO account_force_channels (account_id,channel,rank,created_at) VALUES ('tw1','keshaeuw',0,100),('tw1','caedrel',1,101),('tw2','xqc',0,102),('kk1','xqc',0,200)`)
	// Invalid 1.x names are skipped: URL-ish, prefixed, too short, too long,
	// uppercase, and a hyphen on Twitch. A hyphen on Kick is valid.
	exec(`INSERT INTO account_force_channels (account_id,channel,rank,created_at) VALUES
		('tw1','https://twitch.tv/xqc',2,103),('tw1','#foo',3,104),('tw1','x',4,105),
		('tw1','abcdefghijklmnopqrstuvwxyz',5,106),('tw1','Shroud',6,107),('tw1','my-chan',7,108),
		('kk1','kick-chan',1,201),('kk1','k',2,202)`)

	require.NoError(t, goose.UpContext(ctx, db, "migrations"))

	got, err := NewForceWatch(db, gen.New(db)).List(ctx)
	require.NoError(t, err)
	require.Equal(t, []ForceChannel{
		{Platform: "twitch", Channel: "keshaeuw", Accounts: []string{"tw1"}},
		{Platform: "kick", Channel: "xqc", Accounts: []string{"kk1"}},
		{Platform: "twitch", Channel: "caedrel", Accounts: []string{"tw1"}},
		{Platform: "kick", Channel: "kick-chan", Accounts: []string{"kk1"}},
	}, got)

	// 1.x data stays.
	var n int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM account_force_channels`).Scan(&n))
	require.Equal(t, 12, n)
}
