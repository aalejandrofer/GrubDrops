package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

// ErrForceInvalid marks a force-watch list that fails validation.
var ErrForceInvalid = errors.New("force watch: invalid list")

var (
	twitchChannelRe = regexp.MustCompile(`^[a-z0-9_]{2,25}$`)
	kickChannelRe   = regexp.MustCompile(`^[a-z0-9_-]{2,25}$`)
)

// ForceChannel is one global force-watch channel and the accounts assigned to
// watch it.
type ForceChannel struct {
	Platform string   // "twitch" | "kick"
	Channel  string   // lowercase login/slug
	Accounts []string // account IDs, sorted
}

// ForceWatch stores the global ordered force-watch list.
type ForceWatch struct {
	DB *sql.DB
	Q  *gen.Queries
}

func NewForceWatch(db *sql.DB, q *gen.Queries) *ForceWatch { return &ForceWatch{DB: db, Q: q} }

// List returns the channels in rank order with their sorted account IDs.
func (f *ForceWatch) List(ctx context.Context) ([]ForceChannel, error) {
	chans, err := f.Q.ListGlobalForceChannels(ctx)
	if err != nil {
		return nil, err
	}
	assigns, err := f.Q.ListGlobalForceChannelAccounts(ctx)
	if err != nil {
		return nil, err
	}
	byID := make(map[int64][]string, len(chans))
	for _, a := range assigns {
		byID[a.ChannelID] = append(byID[a.ChannelID], a.AccountID)
	}
	out := make([]ForceChannel, 0, len(chans))
	for _, c := range chans {
		accts := byID[c.ID]
		sort.Strings(accts)
		out = append(out, ForceChannel{Platform: c.Platform, Channel: c.Channel, Accounts: accts})
	}
	return out, nil
}

// ForAccount returns the account's channels in rank order.
func (f *ForceWatch) ForAccount(ctx context.Context, accountID string) ([]string, error) {
	rows, err := f.Q.ListGlobalForceChannelsForAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []string{}
	}
	return rows, nil
}

func forceInvalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrForceInvalid, fmt.Sprintf(format, args...))
}

// Set replaces the whole list in one transaction. List order becomes rank.
// A bad name or platform, a duplicate channel or an account on another
// platform fails with ErrForceInvalid and changes nothing.
func (f *ForceWatch) Set(ctx context.Context, list []ForceChannel) error {
	tx, err := f.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	q := f.Q.WithTx(tx)

	// Validate the whole list first so a bad one does no writes.
	seen := make(map[[2]string]bool, len(list))
	platforms := map[string]string{}
	for _, c := range list {
		switch c.Platform {
		case "twitch":
			if !twitchChannelRe.MatchString(c.Channel) {
				return forceInvalid("channel name %q", c.Channel)
			}
		case "kick":
			if !kickChannelRe.MatchString(c.Channel) {
				return forceInvalid("channel name %q", c.Channel)
			}
		default:
			return forceInvalid("platform %q", c.Platform)
		}
		key := [2]string{c.Platform, c.Channel}
		if seen[key] {
			return forceInvalid("duplicate channel %s/%s", c.Platform, c.Channel)
		}
		seen[key] = true
		for _, acct := range c.Accounts {
			p, ok := platforms[acct]
			if !ok {
				a, err := q.GetAccount(ctx, acct)
				if errors.Is(err, sql.ErrNoRows) {
					return forceInvalid("unknown account %q", acct)
				}
				if err != nil {
					return err
				}
				p = a.Platform
				platforms[acct] = p
			}
			if p != c.Platform {
				return forceInvalid("account %q is %s, channel %q is %s", acct, p, c.Channel, c.Platform)
			}
		}
	}

	if err := q.DeleteAllGlobalForceChannels(ctx); err != nil {
		return err
	}
	now := time.Now().Unix()
	for i, c := range list {
		id, err := q.InsertGlobalForceChannel(ctx, gen.InsertGlobalForceChannelParams{
			Platform: c.Platform, Channel: c.Channel, Rank: int64(i), CreatedAt: now,
		})
		if err != nil {
			return err
		}
		assigned := map[string]bool{}
		for _, acct := range c.Accounts {
			if assigned[acct] {
				continue
			}
			assigned[acct] = true
			if err := q.InsertGlobalForceChannelAccount(ctx, gen.InsertGlobalForceChannelAccountParams{ChannelID: id, AccountID: acct}); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}
