package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

// AccountGames is one account's own ranked game list. It is used only while
// the account's own_games flag is on; otherwise the account mines the global
// chosen list.
type AccountGames struct {
	DB *sql.DB
	Q  *gen.Queries
}

func NewAccountGames(db *sql.DB, q *gen.Queries) *AccountGames { return &AccountGames{DB: db, Q: q} }

// List returns the account's own games in rank order, whether or not the
// flag is on.
func (a *AccountGames) List(ctx context.Context, accountID string) ([]ChosenGame, error) {
	rows, err := a.Q.ListAccountGames(ctx, accountID)
	if err != nil {
		return nil, err
	}
	out := make([]ChosenGame, 0, len(rows))
	for _, r := range rows {
		out = append(out, ChosenGame{ID: r.ID, Name: r.Name, Slug: r.Slug})
	}
	return out, nil
}

// Set writes the flag and, when gameIDs is not nil, replaces the list, in
// one transaction. A nil gameIDs keeps the stored list, so turning the flag
// off and on again brings it back. A repeated game ID keeps its first
// position; an unknown one fails on the foreign key and changes nothing.
func (a *AccountGames) Set(ctx context.Context, accountID string, own bool, gameIDs []string) error {
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	q := a.Q.WithTx(tx)
	flag := int64(0)
	if own {
		flag = 1
	}
	if err := q.SetAccountOwnGames(ctx, gen.SetAccountOwnGamesParams{OwnGames: flag, UpdatedAt: time.Now().Unix(), ID: accountID}); err != nil {
		return err
	}
	if gameIDs != nil {
		if err := q.ClearAccountGames(ctx, accountID); err != nil {
			return err
		}
		seen := make(map[string]bool, len(gameIDs))
		for _, id := range gameIDs {
			if seen[id] {
				continue
			}
			if err := q.AddAccountGame(ctx, gen.AddAccountGameParams{AccountID: accountID, GameID: id, Rank: int64(len(seen))}); err != nil {
				return err
			}
			seen[id] = true
		}
	}
	return tx.Commit()
}
