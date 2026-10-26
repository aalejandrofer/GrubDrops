package store

import (
	"context"
	"database/sql"

	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

type ChosenGame struct{ ID, Name, Slug, TwitchGameID string }

// ChosenGames is the ranked list of games the user wants to farm.
type ChosenGames struct {
	DB *sql.DB
	Q  *gen.Queries
}

func NewChosenGames(db *sql.DB, q *gen.Queries) *ChosenGames { return &ChosenGames{DB: db, Q: q} }

// List returns the chosen games in rank order.
func (c *ChosenGames) List(ctx context.Context) ([]ChosenGame, error) {
	rows, err := c.Q.ListChosenGames(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ChosenGame, 0, len(rows))
	for _, r := range rows {
		out = append(out, ChosenGame{ID: r.ID, Name: r.Name, Slug: r.Slug, TwitchGameID: r.TwitchGameID})
	}
	return out, nil
}

// Set replaces the whole list in one transaction. A repeated game ID keeps
// its first position. An unknown game ID fails on the foreign key and leaves
// the old list in place.
func (c *ChosenGames) Set(ctx context.Context, gameIDs []string) error {
	tx, err := c.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	q := c.Q.WithTx(tx)
	if err := q.ClearChosenGames(ctx); err != nil {
		return err
	}
	seen := make(map[string]bool, len(gameIDs))
	for _, id := range gameIDs {
		if seen[id] {
			continue
		}
		if err := q.AddChosenGame(ctx, gen.AddChosenGameParams{GameID: id, Rank: int64(len(seen))}); err != nil {
			return err
		}
		seen[id] = true
	}
	return tx.Commit()
}
