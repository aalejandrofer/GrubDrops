package api

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/points"
	"github.com/aalejandrofer/grubdrops/internal/store"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

// Balance statuses in GET /api/points.
const (
	// pointsOK: the last read succeeded.
	pointsOK = "ok"
	// pointsPending: not read yet.
	pointsPending = "pending"
	// pointsError: the last read failed; balance is the last good one.
	pointsError = "error"
	// pointsUnavailable: no balance exists, a Kick account or a Twitch
	// channel without channel points.
	pointsUnavailable = "unavailable"
	// pointsOff: not read while the account is paused or its points mode
	// is off; balance is the last one read.
	pointsOff = "off"
)

type pointsBalanceJSON struct {
	AccountID string  `json:"account_id"`
	Status    string  `json:"status"`
	Balance   *int64  `json:"balance"`
	FetchedAt *string `json:"fetched_at"`
	CheckedAt *string `json:"checked_at"`
	// Stale: the balance is older than two refresh intervals.
	Stale bool `json:"stale"`
}

type pointsChannelJSON struct {
	Platform string              `json:"platform"`
	Channel  string              `json:"channel"`
	Live     *bool               `json:"live"`
	Balances []pointsBalanceJSON `json:"balances"`
}

type pointsAccountJSON struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Platform   string `json:"platform"`
	Enabled    bool   `json:"enabled"`
	PointsMode string `json:"points_mode"`
	// BalancesAvailable is false on platforms without balances: Kick.
	BalancesAvailable bool `json:"balances_available"`
	// Total sums the account's known balances on its channels; nil = none.
	Total *int64 `json:"total"`
}

type pointsJSON struct {
	RefreshEverySec int                 `json:"refresh_every_sec"`
	Accounts        []pointsAccountJSON `json:"accounts"`
	Channels        []pointsChannelJSON `json:"channels"`
}

func (s *server) routesPoints(r chi.Router) {
	r.Get("/points", s.getPoints)
	r.Post("/points/refresh", s.postPointsRefresh)
}

// postPointsRefresh starts a read of every balance and returns 202; the
// new balances arrive over SSE. Inside the cooldown it returns 429 with
// retry_after_sec and a Retry-After header.
func (s *server) postPointsRefresh(w http.ResponseWriter, _ *http.Request) {
	if s.d.Points == nil || s.d.PointsRefresh == nil {
		writeErr(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	ok, wait := s.d.PointsRefresh()
	if !ok {
		sec := int((wait + time.Second - 1) / time.Second)
		w.Header().Set("Retry-After", strconv.Itoa(sec))
		writeJSON(w, http.StatusTooManyRequests, map[string]any{"error": "cooldown", "retry_after_sec": sec})
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (s *server) pointsEvery() time.Duration {
	if s.d.PointsEvery > 0 {
		return s.d.PointsEvery
	}
	return points.DefaultEvery
}

// pointsBalance is the API view of one account's cached balance on c.
func (s *server) pointsBalance(a gen.Account, c store.ForceChannel, row store.PointsBalance, had bool, now time.Time) pointsBalanceJSON {
	out := pointsBalanceJSON{AccountID: a.ID}
	if a.Platform != "twitch" || c.Platform != "twitch" {
		out.Status = pointsUnavailable
		return out
	}
	reading := a.Enabled != 0 && platform.PointsMode(a.PointsMode).On()
	if had {
		out.Balance = row.Balance
		out.FetchedAt = rfc3339(row.FetchedAt)
		out.CheckedAt = rfc3339(row.CheckedAt)
		out.Stale = row.Balance != nil && now.Sub(row.FetchedAt) > 2*s.pointsEvery()
	}
	switch {
	case had && row.Error == store.PointsErrUnavailable:
		out.Status = pointsUnavailable
	case !reading:
		out.Status = pointsOff
	case !had:
		out.Status = pointsPending
	case row.Error == store.PointsErrFailed:
		out.Status = pointsError
	default:
		out.Status = pointsOK
	}
	return out
}

// Read states of a points sum on the Home Points watch tile.
const (
	sumOK      = "ok"
	sumPending = "pending"
	sumFailed  = "failed"
	sumNone    = "none"
)

// pointsSum adds up cached balances for the Home Points watch tile. Its read
// state follows these rules, first match wins:
//   - failed: at least one balance was read (status ok or error) and the
//     newest attempt failed for every one of them. The last good values
//     still count in the sum.
//   - ok: some balance is known.
//   - pending: no balance yet and at least one waits for its first read.
//   - none: no balance applies: Kick, no channel points, or only paused or
//     Drops only accounts with nothing cached.
//
// One failing channel among good ones stays ok; the Points page shows which.
type pointsSum struct {
	total                 int64
	known                 bool
	read, failed, pending int
	at                    time.Time // newest successful read among known balances
}

// add folds in one balance from pointsBalance; row is its cache row.
func (p *pointsSum) add(b pointsBalanceJSON, row store.PointsBalance) {
	if b.Status == pointsUnavailable {
		return
	}
	if b.Balance != nil {
		p.total += *b.Balance
		p.known = true
		if row.FetchedAt.After(p.at) {
			p.at = row.FetchedAt
		}
	}
	switch b.Status {
	case pointsOK:
		p.read++
	case pointsError:
		p.read++
		p.failed++
	case pointsPending:
		p.pending++
	}
}

// merge folds in another sum.
func (p *pointsSum) merge(o pointsSum) {
	p.total += o.total
	p.known = p.known || o.known
	p.read += o.read
	p.failed += o.failed
	p.pending += o.pending
	if o.at.After(p.at) {
		p.at = o.at
	}
}

// state is the read state; see pointsSum.
func (p pointsSum) state() string {
	switch {
	case p.read > 0 && p.failed == p.read:
		return sumFailed
	case p.known:
		return sumOK
	case p.pending > 0:
		return sumPending
	default:
		return sumNone
	}
}

// balance is the sum, nil when no balance is known.
func (p pointsSum) balance() *int64 {
	if !p.known {
		return nil
	}
	v := p.total
	return &v
}

// forcePoints sums the cached balances of each Force watch channel, keyed
// "platform/channel", and of all of them. With no cache, or one that cannot
// be read, every sum is empty: read state none.
func (s *server) forcePoints(ctx context.Context, list []store.ForceChannel, accts []gen.Account) (map[string]pointsSum, pointsSum) {
	per := map[string]pointsSum{}
	var all pointsSum
	if s.d.Points == nil {
		return per, all
	}
	rows, err := s.d.Points.List(ctx)
	if err != nil {
		slog.Warn("overview: read points balances failed", "kind", "error", "err", err)
		return per, all
	}
	cached := make(map[[2]string]store.PointsBalance, len(rows))
	for _, b := range rows {
		cached[[2]string{b.AccountID, b.Channel}] = b
	}
	byID := make(map[string]gen.Account, len(accts))
	for _, a := range accts {
		byID[a.ID] = a
	}
	now := s.overviewNow()
	for _, c := range list {
		var sum pointsSum
		for _, id := range c.Accounts {
			a, ok := byID[id]
			if !ok {
				continue
			}
			row, had := cached[[2]string{id, c.Channel}]
			sum.add(s.pointsBalance(a, c, row, had, now), row)
		}
		per[c.Platform+"/"+c.Channel] = sum
		all.merge(sum)
	}
	return per, all
}

// getPoints lists every Force watch channel with each assigned account's
// cached channel-points balance, plus every account's points mode.
func (s *server) getPoints(w http.ResponseWriter, r *http.Request) {
	if s.d.Points == nil || s.d.ForceWatch == nil {
		writeErr(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	ctx := r.Context()
	accts, err := s.d.Q.ListAllAccounts(ctx)
	if err != nil {
		slog.Error("points: list accounts failed", "kind", "error", "err", err)
		writeErr(w, http.StatusInternalServerError, "internal")
		return
	}
	list, err := s.d.ForceWatch.List(ctx)
	if err != nil {
		slog.Error("points: force watch list failed", "kind", "error", "err", err)
		writeErr(w, http.StatusInternalServerError, "internal")
		return
	}
	rows, err := s.d.Points.List(ctx)
	if err != nil {
		slog.Error("points: read balances failed", "kind", "error", "err", err)
		writeErr(w, http.StatusInternalServerError, "internal")
		return
	}
	cached := make(map[[2]string]store.PointsBalance, len(rows))
	for _, b := range rows {
		cached[[2]string{b.AccountID, b.Channel}] = b
	}
	byID := make(map[string]gen.Account, len(accts))
	for _, a := range accts {
		byID[a.ID] = a
	}
	now := s.overviewNow()
	totals := map[string]int64{}
	known := map[string]bool{}
	out := pointsJSON{
		RefreshEverySec: int(s.pointsEvery() / time.Second),
		Accounts:        make([]pointsAccountJSON, 0, len(accts)),
		Channels:        make([]pointsChannelJSON, 0, len(list)),
	}
	for _, c := range list {
		ch := pointsChannelJSON{Platform: c.Platform, Channel: c.Channel,
			Live: s.forceLive(c.Platform, c.Channel), Balances: []pointsBalanceJSON{}}
		for _, id := range c.Accounts {
			a, ok := byID[id]
			if !ok {
				continue
			}
			row, had := cached[[2]string{id, c.Channel}]
			b := s.pointsBalance(a, c, row, had, now)
			if b.Balance != nil && b.Status != pointsUnavailable {
				totals[id] += *b.Balance
				known[id] = true
			}
			ch.Balances = append(ch.Balances, b)
		}
		out.Channels = append(out.Channels, ch)
	}
	for _, a := range accts {
		aj := pointsAccountJSON{
			ID: a.ID, Name: store.AccountName(a), Platform: a.Platform,
			Enabled: a.Enabled != 0, PointsMode: a.PointsMode,
			BalancesAvailable: a.Platform == "twitch",
		}
		if known[a.ID] {
			t := totals[a.ID]
			aj.Total = &t
		}
		out.Accounts = append(out.Accounts, aj)
	}
	writeJSON(w, http.StatusOK, out)
}
