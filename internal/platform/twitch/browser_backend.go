package twitch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	pb "github.com/aalejandrofer/dropsminer/internal/auth/browser/gen/browser/v1"
	"github.com/aalejandrofer/dropsminer/internal/platform"
)

// BrowserBackend routes Twitch GraphQL through the chromedp sidecar so
// Twitch's anti-bot integrity check sees a real browser context. The
// per-account sidecar tab is the trust anchor; the miner side just
// builds and parses gql envelopes.
type BrowserBackend struct {
	sender    TwitchGQLSender
	auth      TwitchSidecarAuthenticator
	rewards   TwitchRewardClaimer // nil-safe: ClaimRewards returns ErrNotSupported when nil
	authFlow  *authFlow
	clientsMu sync.Mutex
	clients   map[string]*twitchAccount

	authedMu sync.Mutex
	authed   map[string]bool // account_id -> sidecar tab has been authenticated this process lifetime

	mu                      sync.Mutex
	allowedLoginsByCampaign map[string][]string
}

// TwitchSidecarAuthenticator is the surface BrowserBackend needs to
// install cookies into the per-account sidecar tab before any gql
// call. Implemented by *browser.Client (same instance as TwitchGQLSender).
type TwitchSidecarAuthenticator interface {
	TwitchAuthenticate(ctx context.Context, accountID string, s *pb.TwitchSession) (*pb.TwitchAuthenticateResponse, error)
}

// TwitchRewardClaimer is the surface BrowserBackend needs to invoke
// the sidecar's TwitchClaimRewards RPC. The two parallel slices map
// 1:1 (games[i] is the game of titles[i]). Implemented by
// *browser.Client.
type TwitchRewardClaimer interface {
	TwitchClaimRewards(ctx context.Context, accountID string, allowedGames []string) (games []string, titles []string, soft []string, err error)
}

type twitchAccount struct {
	c     *client
	disc  *discovery
	chans *channels
	watch *watch
	claim *claimer
}

var _ platform.Backend = (*BrowserBackend)(nil)

// NewBrowserBackend builds a Backend whose gql traffic is proxied
// through the sidecar. `client` must satisfy both TwitchGQLSender and
// TwitchSidecarAuthenticator — typically *browser.Client.
func NewBrowserBackend(client interface {
	TwitchGQLSender
	TwitchSidecarAuthenticator
}) *BrowserBackend {
	bb := &BrowserBackend{
		sender:                  client,
		auth:                    client,
		authFlow:                newAuthFlow(),
		clients:                 map[string]*twitchAccount{},
		authed:                  map[string]bool{},
		allowedLoginsByCampaign: map[string][]string{},
	}
	if r, ok := client.(TwitchRewardClaimer); ok {
		bb.rewards = r
	}
	return bb
}

// ensureAuthenticated pushes the account's persisted cookies into the
// sidecar tab if we haven't done so already this process lifetime.
//
// Session cookies live under s.Cookies["twitch"] as a JSON blob written
// by the paste handler. Schema: {"cookies":[{name,value,domain,path}],
// "username","user_id"}.
func (b *BrowserBackend) ensureAuthenticated(ctx context.Context, s platform.Session) error {
	if s.AccountID == "" {
		return errors.New("session has no AccountID")
	}
	b.authedMu.Lock()
	already := b.authed[s.AccountID]
	b.authedMu.Unlock()
	if already {
		return nil
	}

	blob := s.Cookies["twitch"]
	if blob == "" {
		return fmt.Errorf("twitch session has no cookie blob for account %s", s.AccountID)
	}
	var stored struct {
		Cookies []struct {
			Name   string `json:"name"`
			Value  string `json:"value"`
			Domain string `json:"domain"`
			Path   string `json:"path"`
		} `json:"cookies"`
	}
	if err := json.Unmarshal([]byte(blob), &stored); err != nil {
		return fmt.Errorf("decode twitch cookie blob: %w", err)
	}
	pbCookies := make([]*pb.Cookie, 0, len(stored.Cookies))
	for _, c := range stored.Cookies {
		pbCookies = append(pbCookies, &pb.Cookie{Name: c.Name, Value: c.Value, Domain: c.Domain, Path: c.Path})
	}
	resp, err := b.auth.TwitchAuthenticate(ctx, s.AccountID, &pb.TwitchSession{Cookies: pbCookies})
	if err != nil {
		return fmt.Errorf("sidecar twitch authenticate: %w", err)
	}
	slog.Info("twitch sidecar tab ready", "account", s.AccountID, "login", resp.Username, "user_id", resp.UserId)
	b.authedMu.Lock()
	b.authed[s.AccountID] = true
	b.authedMu.Unlock()
	return nil
}

func (b *BrowserBackend) Name() string { return "twitch" }

func (b *BrowserBackend) StartDeviceLogin(ctx context.Context) (platform.DeviceChallenge, error) {
	// Device-code login still works against id.twitch.tv (not gated by
	// integrity). Browser-routed accounts can still use it to obtain
	// an OAuth access token, but most users will paste cookies instead.
	return b.authFlow.start(ctx)
}

func (b *BrowserBackend) PollDeviceLogin(ctx context.Context, ch platform.DeviceChallenge) (platform.Session, error) {
	internal, ok := ch.Internal.(deviceInternal)
	if !ok {
		return platform.Session{}, errors.New("invalid challenge internal")
	}
	return b.authFlow.poll(ctx, internal)
}

func (b *BrowserBackend) LoginViaBrowser(_ context.Context, _ platform.BrowserRPC) (platform.Session, error) {
	return platform.Session{}, errors.New("LoginViaBrowser unused; use cookie-paste form")
}

func (b *BrowserBackend) RefreshSession(ctx context.Context, s platform.Session) (platform.Session, error) {
	if s.RefreshToken == "" {
		// Cookie-based sessions have no refresh token; just return as-is.
		// Watcher will hit a 401 from /gql which surfaces re-auth.
		return s, nil
	}
	return b.authFlow.refresh(ctx, s)
}

// accountFor returns the per-account subsystem bundle. Account-keyed
// because the sidecar tab is per-account; the client must include
// X-Account headers (encoded into the gRPC accountID field) so the
// sidecar routes the gql call to the right tab.
//
// Account ID is sourced from platform.Session via session.AccountID,
// which is plumbed through Watcher (see scheduler/main wiring).
func (b *BrowserBackend) accountFor(accountID string) *twitchAccount {
	b.clientsMu.Lock()
	defer b.clientsMu.Unlock()
	if a, ok := b.clients[accountID]; ok {
		return a
	}
	c := newBrowserClient(b.sender, accountID)
	a := &twitchAccount{
		c:     c,
		disc:  &discovery{c: c},
		chans: &channels{c: c},
		watch: &watch{c: c},
		claim: &claimer{c: c},
	}
	b.clients[accountID] = a
	return a
}

func (b *BrowserBackend) ListActiveCampaigns(ctx context.Context, s platform.Session) ([]platform.Campaign, error) {
	if err := b.ensureAuthenticated(ctx, s); err != nil {
		return nil, err
	}
	a := b.accountFor(s.AccountID)
	camps, err := a.disc.listActive(ctx, s)
	if err != nil {
		return nil, err
	}
	allowed := a.disc.drainAllowed()
	b.mu.Lock()
	for cid, logins := range allowed {
		b.allowedLoginsByCampaign[cid] = logins
	}
	b.mu.Unlock()
	return camps, nil
}

func (b *BrowserBackend) ListEligibleChannels(ctx context.Context, s platform.Session, c platform.Campaign) ([]platform.Stream, error) {
	if err := b.ensureAuthenticated(ctx, s); err != nil {
		return nil, err
	}
	a := b.accountFor(s.AccountID)
	b.mu.Lock()
	allowed := b.allowedLoginsByCampaign[c.ID]
	b.mu.Unlock()
	if len(allowed) > 0 {
		return a.chans.listEligible(ctx, s, c, allowed)
	}
	// Fall back to game directory when allow.channels is empty —
	// same logic as Backend.ListEligibleChannels. Most public drop
	// campaigns (Minecraft etc) have no channel restriction.
	return a.chans.listForGameDirectory(ctx, s, slugify(c.Game))
}

func (b *BrowserBackend) InventoryProgress(ctx context.Context, s platform.Session) ([]platform.Progress, error) {
	if err := b.ensureAuthenticated(ctx, s); err != nil {
		return nil, err
	}
	return b.accountFor(s.AccountID).disc.inventory(ctx, s)
}

func (b *BrowserBackend) StartWatch(ctx context.Context, s platform.Session, stream platform.Stream) (platform.WatchHandle, error) {
	if err := b.ensureAuthenticated(ctx, s); err != nil {
		return platform.WatchHandle{}, err
	}
	h, err := b.accountFor(s.AccountID).watch.start(ctx, s, stream)
	if err != nil {
		return h, err
	}
	h.AccountID = s.AccountID
	return h, nil
}

func (b *BrowserBackend) Heartbeat(ctx context.Context, h platform.WatchHandle) error {
	if h.AccountID == "" {
		return errors.New("missing AccountID on WatchHandle")
	}
	return b.accountFor(h.AccountID).watch.heartbeat(ctx, h)
}

func (b *BrowserBackend) StopWatch(ctx context.Context, h platform.WatchHandle) error {
	if h.AccountID == "" {
		return nil
	}
	return b.accountFor(h.AccountID).watch.stop(ctx, h)
}

func (b *BrowserBackend) Claim(ctx context.Context, s platform.Session, drop platform.DropBenefit) error {
	if err := b.ensureAuthenticated(ctx, s); err != nil {
		return err
	}
	return b.accountFor(s.AccountID).claim.claim(ctx, s, drop)
}

// ClaimRewards satisfies platform.RewardClaimer. Delegates to the
// sidecar TwitchClaimRewards RPC; returns an empty list when the
// sidecar doesn't surface the RPC (older binary).
func (b *BrowserBackend) ClaimRewards(ctx context.Context, s platform.Session, allowedGames []string) ([]platform.ClaimedReward, error) {
	if b.rewards == nil {
		return nil, nil
	}
	if err := b.ensureAuthenticated(ctx, s); err != nil {
		return nil, err
	}
	games, titles, soft, err := b.rewards.TwitchClaimRewards(ctx, s.AccountID, allowedGames)
	if err != nil {
		return nil, err
	}
	for _, e := range soft {
		slog.Warn("twitch reward claim soft error", "account", s.AccountID, "err", e)
	}
	out := make([]platform.ClaimedReward, 0, len(games))
	for i := range games {
		out = append(out, platform.ClaimedReward{Game: games[i], Title: titles[i]})
	}
	return out, nil
}

// AllowedChannelCount returns the number of channels in the cached
// allow-list for campaignID. Mirrors twitch.Backend.AllowedChannelCount;
// the dashboard uses it to populate the "channels" column on each
// Active Campaigns row regardless of which Twitch backend is wired up.
func (b *BrowserBackend) AllowedChannelCount(campaignID string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.allowedLoginsByCampaign[campaignID])
}
