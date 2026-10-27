package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

// LinkNoticePrefix keys a sent "link your account to claim" notice in kv.
const LinkNoticePrefix = "link_notice:"

// LinkNotices remembers which link notices were sent, under
// LinkNoticePrefix + benefitID + ":" + accountID, so a runner rebuild or a
// restart does not send the same one again.
type LinkNotices struct{ Q *gen.Queries }

func NewLinkNotices(q *gen.Queries) *LinkNotices { return &LinkNotices{Q: q} }

// FirstLinkNotice records the notice for the account's benefit and reports
// whether none was recorded before.
func (n *LinkNotices) FirstLinkNotice(accountID, benefitID string) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), skipTimeout)
	defer cancel()
	key := LinkNoticePrefix + benefitID + ":" + accountID
	if _, err := n.Q.GetSettingString(ctx, key); err == nil {
		return false, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	return true, n.Q.UpsertSettingString(ctx, gen.UpsertSettingStringParams{Key: key, Value: []byte("1")})
}
