// Package authcheck runs a lightweight, periodic liveness probe of every
// enabled account's auth (Twitch token / Kick cookies) so the operator
// learns an account needs re-authentication BEFORE it silently stops
// mining. Results are persisted in kv (key authcheck:<accountID>) and
// surfaced on the Accounts page; a manual "check now" button calls
// CheckAll on demand.
package authcheck

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

// Prefix namespaces per-account auth-check results in the kv table.
const Prefix = "authcheck:"

// Result is the persisted outcome of one account's auth probe.
type Result struct {
	OK        bool   `json:"ok"`
	CheckedAt int64  `json:"checked_at"`
	Msg       string `json:"msg"`
}

// sessionGetter is the slice of *store.SessionStore the checker needs.
type sessionGetter interface {
	Get(ctx context.Context, accountID string) (platform.Session, bool, error)
}

type Checker struct {
	q        *gen.Queries
	sessions sessionGetter
	registry *platform.Registry
	log      *slog.Logger
}

func New(q *gen.Queries, sessions sessionGetter, reg *platform.Registry) *Checker {
	return &Checker{q: q, sessions: sessions, registry: reg, log: slog.Default().With("component", "authcheck")}
}

// Run probes once immediately, then every interval until ctx is cancelled.
func (c *Checker) Run(ctx context.Context, interval time.Duration) {
	if c == nil {
		return
	}
	if interval <= 0 {
		interval = 12 * time.Hour
	}
	c.CheckAll(ctx)
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c.CheckAll(ctx)
		}
	}
}

// CheckAll probes every enabled account and persists each result.
func (c *Checker) CheckAll(ctx context.Context) {
	if c == nil {
		return
	}
	accs, err := c.q.ListEnabledAccounts(ctx)
	if err != nil {
		c.log.Warn("authcheck: list accounts failed", "err", err)
		return
	}
	for _, a := range accs {
		c.checkOne(ctx, a.ID, a.Platform)
	}
}

func (c *Checker) checkOne(ctx context.Context, accountID, plat string) {
	res := Result{CheckedAt: time.Now().Unix()}
	b, ok := c.registry.Get(plat)
	if !ok {
		res.Msg = "no backend for platform"
		c.persist(ctx, accountID, res)
		return
	}
	checker, ok := b.(platform.AuthChecker)
	if !ok {
		// Platform has no probe — treat as healthy (nothing to verify).
		res.OK, res.Msg = true, "no auth check for platform"
		c.persist(ctx, accountID, res)
		return
	}
	sess, found, err := c.sessions.Get(ctx, accountID)
	if err != nil || !found {
		res.Msg = "no session — never authenticated"
		c.persist(ctx, accountID, res)
		return
	}
	sess.AccountID = accountID
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := checker.VerifyAuth(cctx, sess); err != nil {
		res.OK, res.Msg = false, truncate(err.Error(), 200)
	} else {
		res.OK, res.Msg = true, "ok"
	}
	c.persist(ctx, accountID, res)
	c.log.Info("authcheck", "kind", "auth", "account", accountID, "ok", res.OK, "msg", res.Msg)
}

func (c *Checker) persist(ctx context.Context, accountID string, res Result) {
	b, _ := json.Marshal(res)
	if err := c.q.UpsertSettingString(ctx, gen.UpsertSettingStringParams{Key: Prefix + accountID, Value: b}); err != nil {
		c.log.Warn("authcheck: persist failed", "account", accountID, "err", err)
	}
}

// Load returns the stored auth-check result for an account (ok=false when
// none has been recorded yet).
func Load(ctx context.Context, q *gen.Queries, accountID string) (Result, bool) {
	v, err := q.GetSettingString(ctx, Prefix+accountID)
	if err != nil || len(v) == 0 {
		return Result{}, false
	}
	var r Result
	if json.Unmarshal(v, &r) != nil {
		return Result{}, false
	}
	return r, true
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
