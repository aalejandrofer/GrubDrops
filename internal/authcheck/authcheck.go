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
	// retryDelay is the pause before the single VerifyAuth retry. A blip
	// (idle-timeout, transient 403/timeout) clears within seconds, so one
	// short-delayed retry avoids marking a valid session expired while still
	// failing a genuinely dead one. Defaults to 15s in New.
	retryDelay time.Duration
}

func New(q *gen.Queries, sessions sessionGetter, reg *platform.Registry) *Checker {
	return &Checker{q: q, sessions: sessions, registry: reg, log: slog.Default().With("component", "authcheck"), retryDelay: 15 * time.Second}
}

// verifyWithRetry probes the session and, on failure, retries exactly once
// after retryDelay. This debounces transient failures (a single idle-timeout,
// Cloudflare 403, or network blip) so a still-valid session is not wrongly
// reported as expired — the bug where a Kick account flipped to needs_auth
// after being left idle and only a manual cookie re-import cleared it. A
// genuinely expired session fails both attempts and is still reported.
func (c *Checker) verifyWithRetry(ctx context.Context, checker platform.AuthChecker, sess platform.Session) error {
	err := checker.VerifyAuth(ctx, sess)
	if err == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return err
	case <-time.After(c.retryDelay):
	}
	return checker.VerifyAuth(ctx, sess)
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
	cctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	if err := c.verifyWithRetry(cctx, checker, sess); err != nil {
		res.OK, res.Msg = false, truncate(err.Error(), 200)
	} else {
		res.OK, res.Msg = true, "ok"
	}
	c.persist(ctx, accountID, res)
	c.log.Info("authcheck", "kind", "auth", "account", accountID, "ok", res.OK, "msg", res.Msg)

	// Backfill / refresh the account avatar while we have a verified
	// session in hand. Avatars change rarely, so the ~12h sweep cadence is
	// plenty — keeping the fetch here (and on login) keeps it off the hot
	// per-tick path. Best-effort: a failure never affects the auth result.
	if res.OK {
		c.refreshAvatar(cctx, accountID, b, sess)
	}
}

// refreshAvatar fetches the account's profile picture (if the backend
// supports it) and persists it. Best-effort and idempotent — only writes
// when the fetched URL differs from what's stored, to avoid churning
// updated_at on every sweep.
func (c *Checker) refreshAvatar(ctx context.Context, accountID string, b platform.Backend, sess platform.Session) {
	fetcher, ok := b.(platform.AvatarFetcher)
	if !ok {
		return
	}
	url, err := fetcher.FetchAvatar(ctx, sess)
	if err != nil {
		c.log.Debug("authcheck: avatar fetch failed", "account", accountID, "err", err)
		return
	}
	if url == "" {
		return
	}
	acc, err := c.q.GetAccount(ctx, accountID)
	if err == nil && acc.AvatarUrl == url {
		return // unchanged
	}
	if err := c.q.UpdateAccountAvatar(ctx, gen.UpdateAccountAvatarParams{
		AvatarUrl: url,
		UpdatedAt: time.Now().Unix(),
		ID:        accountID,
	}); err != nil {
		c.log.Warn("authcheck: persist avatar failed", "account", accountID, "err", err)
		return
	}
	c.log.Info("authcheck: avatar updated", "account", accountID)
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
