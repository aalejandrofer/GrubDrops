// Command kick-probe loads a stored Kick session for an account, decrypts it
// with GRUB_MASTER_KEY, and dumps the live authed Kick endpoint responses
// (campaigns/progress) plus public discovery — so we can verify the real
// authed shapes and tell "not enrolled" from "no watch time yet". One-shot ops
// tool; run on the host with the prod DB + master key:
//
//	GRUB_MASTER_KEY=… GRUB_DB_PATH=/data/miner.db kick-probe <account_id> [category_slug]
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/aalejandrofer/grubdrops/internal/platform/kick"
	"github.com/aalejandrofer/grubdrops/internal/store"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

func main() {
	if len(os.Args) < 2 {
		die("usage: kick-probe <account_id> [category_slug]  (GRUB_MASTER_KEY, GRUB_DB_PATH env)")
	}
	accountID := os.Args[1]
	category := "rust"
	if len(os.Args) > 2 {
		category = os.Args[2]
	}
	key := os.Getenv("GRUB_MASTER_KEY")
	if key == "" {
		die("GRUB_MASTER_KEY unset")
	}
	dbPath := os.Getenv("GRUB_DB_PATH")
	if dbPath == "" {
		dbPath = "/data/miner.db"
	}

	ctx := context.Background()
	db, err := store.Open(ctx, dbPath)
	if err != nil {
		die("open db: " + err.Error())
	}
	defer db.Close()

	cryptor, err := store.NewCryptor(key)
	if err != nil {
		die("master key invalid: " + err.Error())
	}
	sessions := store.NewSessionStore(db, gen.New(db), cryptor)

	sess, ok, err := sessions.Get(ctx, accountID)
	if err != nil {
		die("load session: " + err.Error())
	}
	if !ok {
		die("no session for account " + accountID)
	}
	sess.AccountID = accountID

	// Optional claim test: `kick-probe <acc> claim <reward_id> <campaign_id>`
	// POSTs a real /drops/claim and dumps the status/body so we can verify the
	// claim endpoint live. Otherwise run the read-only diagnostic dump.
	if category == "claim" && len(os.Args) >= 5 {
		kick.ProbeClaim(ctx, sess, os.Args[3], os.Args[4])
		return
	}

	kick.Probe(ctx, sess, category)
}

func die(m string) { fmt.Fprintln(os.Stderr, "fatal:", m); os.Exit(1) }
