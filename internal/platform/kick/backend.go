package kick

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/auth/browser"
	"github.com/aalejandrofer/grubdrops/internal/dockerctl"
	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// Backend implements platform.Backend for Kick over a pure-HTTP utls client
// that mimics a real Chrome TLS/HTTP2 fingerprint. Kick's API 403s any
// CDP-driven browser (chromedp included) but accepts this fingerprint, so the
// chromedp sidecar is no longer used for Kick data (see
// project_kick_breakthrough_utls memory / kick_issues.md). The browser.Client
// is retained only for the legacy interactive-login path.
type Backend struct {
	c   *browser.Client
	api *api

	// sidecars manages on-demand start/stop + per-account client pinning for
	// the IVS watch path. clientByName dials one gRPC client per derived
	// container name (lazy connect, survives container restarts).
	sidecars     *sidecarRegistry
	clientByName map[string]*browser.Client
	clientMu     sync.Mutex
	sidecarPort  int

	// browserWatch, when true AND c != nil, routes StartWatch/Heartbeat/
	// StopWatch to the chromedp sidecar so a real IVS <video> session
	// accrues drop watch-time. Real video playback is the ONLY path that
	// accrues Kick drop watch-time (the pure-HTTP viewer-WS presence does
	// NOT accrue — proven 2026-06-12), so this is mandatory: when false,
	// StartWatch errors rather than silently running a non-accruing watch.
	// All other calls (campaigns/progress/claim/discovery) stay on the
	// utls HTTP transport regardless.
	browserWatch bool

	mu               sync.Mutex
	handleByAcc      map[string]string // accountID -> watch handle
	channelsByAcc    map[string][]string
	campaignChannels map[string][]kickChannel // campaignID -> eligible channels (slug+id)
	categoryChannels map[string][]kickChannel // game/category -> union of participating channels across campaigns
}

var _ platform.Backend = (*Backend)(nil)

// Option configures optional Backend behaviour at construction.
type Option func(*options)

type options struct {
	sidecarImage   string
	sidecarNetwork string
}

// WithSidecarAutoCreate enables auto-create of per-account browser sidecars:
// the miner pulls image and creates+removes labelled containers over the
// docker socket so the default compose needs no hand-defined browser services.
// network (may be "") overrides network self-detection.
func WithSidecarAutoCreate(image, network string) Option {
	return func(o *options) { o.sidecarImage, o.sidecarNetwork = image, network }
}

// New builds the Kick backend. c is the login client (data flows over the utls
// HTTP client; c is kept for the interactive-login path). ctl controls the
// per-account chromedp sidecar containers on demand (nil = degrade to
// always-on). template/port derive each account's container name + gRPC port;
// idleGrace is how long an account may go without watch activity before its
// sidecar is reaped.
func New(c *browser.Client, ctl dockerctl.Controller, template string, port int, idleGrace time.Duration, opts ...Option) *Backend {
	var o options
	for _, fn := range opts {
		fn(&o)
	}
	reg := newSidecarRegistry(ctl, template, port, idleGrace)
	if o.sidecarImage != "" {
		reg.withCreate(o.sidecarImage, o.sidecarNetwork)
	}
	b := &Backend{
		c:                c,
		api:              newAPI(),
		sidecars:         reg,
		clientByName:     map[string]*browser.Client{},
		sidecarPort:      port,
		handleByAcc:      map[string]string{},
		channelsByAcc:    map[string][]string{},
		campaignChannels: map[string][]kickChannel{},
		categoryChannels: map[string][]kickChannel{},
	}
	if ctl != nil {
		go b.sidecars.runReaper(context.Background())
	}
	return b
}

// RegisterSidecar maps an account to its username-derived sidecar. Called at
// startup (per-account build loop) and on login.
func (b *Backend) RegisterSidecar(accountID, username string) {
	b.sidecars.register(accountID, username)
}

// SweepSidecars removes auto-created sidecars whose account is no longer in the
// live roster. Call once after the initial account Reload (the periodic reaper
// covers it thereafter). Safe no-op when there is no docker controller.
func (b *Backend) SweepSidecars(ctx context.Context) {
	b.sidecars.sweepOrphans(ctx)
}

// watchClientForName returns (dialing once) the gRPC client for a container
// name. Lazy connect means a stopped container is fine until first RPC.
func (b *Backend) watchClientForName(name string) (*browser.Client, error) {
	b.clientMu.Lock()
	defer b.clientMu.Unlock()
	if cl, ok := b.clientByName[name]; ok {
		return cl, nil
	}
	target := name
	if b.sidecarPort > 0 {
		target = fmt.Sprintf("%s:%d", name, b.sidecarPort)
	}
	cl, err := browser.Dial(target)
	if err != nil {
		return nil, err
	}
	b.clientByName[name] = cl
	return cl, nil
}

// EnableBrowserWatch switches StartWatch/Heartbeat/StopWatch onto the
// chromedp sidecar (a real, playing IVS <video> per watch — the only path
// that accrues Kick drop watch-time). No-op without a sidecar client: the
// backend logs and leaves browser-watch off, in which case StartWatch
// errors (there is no non-accruing fallback). Call once at construction,
// before the watcher starts.
func (b *Backend) EnableBrowserWatch() {
	if b.c == nil {
		slog.Warn("kick: browser-watch requested but no sidecar client configured; Kick watch will be unavailable")
		return
	}
	b.browserWatch = true
}

// kickBrowserWatch is stored in WatchHandle.Internal for the sidecar
// IVS-video watch path. It carries the opaque sidecar tab handle so
// Heartbeat/StopWatch can target the same browser tab.
type kickBrowserWatch struct {
	handle string
	// client is the specific sidecar this watch's tab lives in, so
	// Heartbeat/StopWatch target the same Chrome that opened it.
	client *browser.Client
	// accountID lets Heartbeat bump the registry's lastActive so the
	// reaper keeps this account's sidecar up while it's actively watching.
	accountID string
}

func (b *Backend) Name() string { return "kick" }

// RegisterChannel stores a SINGLE channel for an account, replacing any
// existing list. Convenience wrapper; new code should call RegisterChannels.
func (b *Backend) RegisterChannel(accountID, channel string) {
	if channel == "" {
		b.RegisterChannels(accountID, nil)
		return
	}
	b.RegisterChannels(accountID, []string{channel})
}

// RegisterChannels stores the full channel list an account wants to
// mine. Replaces any previous list. Duplicate entries are deduplicated;
// empty strings dropped. Pass nil/empty to unregister the account.
func (b *Backend) RegisterChannels(accountID string, channels []string) {
	cleaned := dedupeChannels(channels)
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(cleaned) == 0 {
		delete(b.channelsByAcc, accountID)
		return
	}
	b.channelsByAcc[accountID] = cleaned
}

// Channels returns the registered channel list for an account. Returns
// nil when none registered. Caller must not mutate the returned slice.
func (b *Backend) Channels(accountID string) []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	chs := b.channelsByAcc[accountID]
	if len(chs) == 0 {
		return nil
	}
	out := make([]string, len(chs))
	copy(out, chs)
	return out
}

// AllowedChannelCount returns the number of distinct channels currently
// registered across all accounts. Kick discovery doesn't surface a
// per-campaign allow-list, so the campaignID argument is ignored and
// the dashboard treats the result as the campaign-wide eligible channel
// count.
func (b *Backend) AllowedChannelCount(_ string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	seen := make(map[string]struct{})
	for _, chs := range b.channelsByAcc {
		for _, ch := range chs {
			if ch == "" {
				continue
			}
			seen[ch] = struct{}{}
		}
	}
	return len(seen)
}

func dedupeChannels(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, ch := range in {
		ch = strings.TrimSpace(ch)
		if ch == "" {
			continue
		}
		key := strings.ToLower(ch)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, ch)
	}
	return out
}

func (b *Backend) StartDeviceLogin(_ context.Context) (platform.DeviceChallenge, error) {
	return platform.DeviceChallenge{}, errors.New("kick: use cookie-paste login")
}

func (b *Backend) PollDeviceLogin(_ context.Context, _ platform.DeviceChallenge) (platform.Session, error) {
	return platform.Session{}, errors.New("kick: use cookie-paste login")
}

func (b *Backend) LoginViaBrowser(_ context.Context, rpc platform.BrowserRPC) (platform.Session, error) {
	return rpc.LoginInteractive("kick")
}

func (b *Backend) RefreshSession(_ context.Context, s platform.Session) (platform.Session, error) {
	// Kick cookies don't refresh server-side. Return unchanged — invalid
	// sessions surface as 401s on the next API call.
	return s, nil
}

func (b *Backend) ListActiveCampaigns(ctx context.Context, s platform.Session) ([]platform.Campaign, error) {
	// Drops campaigns over the utls HTTP client (GET /api/v1/drops/campaigns).
	// The per-account whitelist filters the result — never hardcode a game.
	camps, err := b.api.Campaigns(ctx, s)
	if err != nil {
		return nil, fmt.Errorf("kick campaigns: %w", err)
	}
	out := make([]platform.Campaign, 0, len(camps))
	for _, c := range camps {
		if s.GameFilter != nil && c.Game != "" && !s.GameFilter(c.Game) {
			continue
		}
		benefits := make([]platform.DropBenefit, 0, len(c.Rewards))
		for _, ben := range c.Rewards {
			required := ben.RequiredMinutes
			if required <= 0 {
				required = 120 // Kick drops typically require ~2h
			}
			benefits = append(benefits, platform.DropBenefit{
				ID:              ben.ID,
				CampaignID:      c.ID,
				Name:            ben.Name,
				RequiredMinutes: required,
				ImageURL:        ben.ImageURL,
			})
		}
		slugs := make([]string, 0, len(c.Channels))
		for _, ch := range c.Channels {
			slugs = append(slugs, ch.Slug)
		}
		if c.ID != "" && len(c.Channels) > 0 {
			b.mu.Lock()
			b.campaignChannels[c.ID] = c.Channels // slug+id, for the watch handshake
			// Kick drops accrue on ANY participating live channel in the
			// campaign's category, so pool every campaign's channels by game.
			// Open campaigns (channels: []) borrow this pool — that's how the
			// daemon finds a live Rust channel for "Kick Off 2" et al.
			if c.Game != "" {
				b.categoryChannels[c.Game] = mergeChannels(b.categoryChannels[c.Game], c.Channels)
			}
			b.mu.Unlock()
		}
		camp := platform.Campaign{
			ID:              c.ID,
			Platform:        "kick",
			Game:            c.Game,
			Name:            c.Name,
			Benefits:        benefits,
			AllowedChannels: slugs,
		}
		// Kick gives no per-campaign "is the external account linked" signal
		// (unlike Twitch). Inferring it from /drops/progress deadlocks: progress
		// only appears after watch time, watch time was blocked until "linked",
		// "linked" was read from progress — so a freshly-linked campaign stayed
		// "unlinked" forever. So assume linked and mine; just surface the
		// connect_url so the user can link manually if a drop never progresses.
		camp.AccountLinked = true
		camp.AccountLinkChecked = true
		camp.AccountLinkURL = c.ConnectURL
		// Parse RFC3339 start/end so the /drops past|current|upcoming tabs work.
		if t, err := time.Parse(time.RFC3339, c.StartsAt); err == nil {
			camp.StartsAt = t.UTC()
		}
		if t, err := time.Parse(time.RFC3339, c.EndsAt); err == nil {
			camp.EndsAt = t.UTC()
		}
		// Normalise status to active|upcoming|expired (Kick also supports
		// upcoming campaigns). Trust explicit "expired"; derive the rest from
		// the window so the watcher only mines truly-active ones.
		now := time.Now()
		switch {
		case strings.EqualFold(c.Status, "expired"), !camp.EndsAt.IsZero() && camp.EndsAt.Before(now):
			camp.Status = "expired"
		case !camp.StartsAt.IsZero() && camp.StartsAt.After(now):
			camp.Status = "upcoming"
		default:
			camp.Status = "active"
		}
		out = append(out, camp)
	}
	return out, nil
}

func (b *Backend) ListEligibleChannels(ctx context.Context, s platform.Session, c platform.Campaign) ([]platform.Stream, error) {
	// Candidate pool:
	//  - Restricted campaign (has its own channels, e.g. "Team Oilrats"): ONLY
	//    those channels can accrue, so use them.
	//  - Open campaign (channels: []): Kick drops accrue on ANY participating
	//    live channel in the same category, so use the category-wide union we
	//    pooled across all campaigns in ListActiveCampaigns.
	// Every candidate is verified LIVE + actually streaming the campaign's
	// category before we commit (a live channel on a DIFFERENT game accrues
	// nothing; an offline one can't be watched). This replaces the old generic
	// /stream/livestreams feed, which ignored the category slug and returned
	// unrelated games (smite, slots, …) — the bot would watch them for nothing.
	b.mu.Lock()
	pool := append([]kickChannel(nil), b.campaignChannels[c.ID]...)
	openCampaign := len(pool) == 0
	if openCampaign {
		pool = append(pool, b.categoryChannels[c.Game]...)
	}
	b.mu.Unlock()

	// For OPEN campaigns the pool is the whole category union, in arbitrary
	// (campaign-discovery) order. Bias it toward known always-live,
	// drops-enabled, high-participation broadcasters so the watcher lands on
	// a reliable channel (oilrats streams Rust ~24/7 and participates in the
	// open + Team-Oilrats campaigns, so watching it accrues the most at
	// once). probeLive checks liveness anyway, so this is purely an ordering
	// preference, not a hard pin. Restricted campaigns keep their own
	// channel order untouched (only their listed channels can accrue).
	if openCampaign {
		pool = preferReliableChannels(pool)
	}

	if live := b.probeLive(ctx, s, c, pool); len(live) > 0 {
		return live, nil
	}

	// Fallback: channels the operator registered manually. Returned as-is (no
	// liveness probe) — an explicit operator override, and the watch loop will
	// drop a dead one on the next heartbeat.
	b.mu.Lock()
	manual := b.channelsByAcc[s.AccountID]
	b.mu.Unlock()
	out := make([]platform.Stream, 0, len(manual))
	for _, ch := range manual {
		out = append(out, platform.Stream{Channel: ch, DropsEnabled: true})
	}
	return out, nil
}

// probeLive checks each candidate channel's livestream and returns the ones
// that are LIVE and streaming the campaign's category. Caps the number of
// network probes. ChannelLivestream returns the channel id when known (campaign
// channels carry it; bare slugs fall back to the livestream id, which the watch
// handshake also accepts).
func (b *Backend) probeLive(ctx context.Context, s platform.Session, c platform.Campaign, pool []kickChannel) []platform.Stream {
	const maxProbe = 12
	var live []platform.Stream
	for i, ch := range pool {
		if i >= maxProbe {
			break
		}
		ok, lsID, viewers, category, err := b.api.ChannelLivestream(ctx, s, ch.Slug)
		if err != nil {
			slog.Debug("kick channel liveness check failed", "channel", ch.Slug, "err", err)
			continue
		}
		if !ok {
			continue
		}
		if c.Game != "" && category != "" && !strings.EqualFold(strings.TrimSpace(category), strings.TrimSpace(c.Game)) {
			slog.Debug("kick skip channel on wrong category", "channel", ch.Slug, "streaming", category, "want", c.Game)
			continue
		}
		id := ch.ID
		if id == "" {
			id = lsID // bare slug: best-effort, watch handshake accepts the livestream id
		}
		live = append(live, platform.Stream{Channel: ch.Slug, ChannelID: id, ViewerCount: viewers, DropsEnabled: true})
	}
	return live
}

// reliableChannels are broadcasters known to stream their drops category
// almost continuously with high viewer participation. When an OPEN campaign
// (channels: []) lets us watch ANY participating live channel in the
// category, we prefer these so the watcher reliably lands on a steady stream
// instead of churning across short-lived broadcasters. Ranked best-first.
// Currently Rust-focused (the active event); harmless for other categories
// since none of these will be live + on-category there, so probeLive skips
// them and the rest of the pool is used as-is.
var reliableChannels = []string{"oilrats"}

// preferReliableChannels returns the pool reordered so any reliableChannels
// present come first (in reliableChannels rank order), followed by the rest
// in their original order. It does not add or drop channels — probeLive still
// gates on live + correct category.
func preferReliableChannels(pool []kickChannel) []kickChannel {
	if len(pool) < 2 {
		return pool
	}
	rank := make(map[string]int, len(reliableChannels))
	for i, s := range reliableChannels {
		rank[s] = i
	}
	preferred := make([]kickChannel, 0, len(pool))
	rest := make([]kickChannel, 0, len(pool))
	// Collect preferred in rank order.
	bySlug := map[string]kickChannel{}
	for _, ch := range pool {
		if _, ok := rank[strings.ToLower(ch.Slug)]; ok {
			bySlug[strings.ToLower(ch.Slug)] = ch
		} else {
			rest = append(rest, ch)
		}
	}
	for _, s := range reliableChannels {
		if ch, ok := bySlug[s]; ok {
			preferred = append(preferred, ch)
		}
	}
	return append(preferred, rest...)
}

// mergeChannels appends src to dst, deduping by lowercased slug.
func mergeChannels(dst, src []kickChannel) []kickChannel {
	seen := make(map[string]struct{}, len(dst))
	for _, c := range dst {
		seen[strings.ToLower(c.Slug)] = struct{}{}
	}
	for _, c := range src {
		k := strings.ToLower(c.Slug)
		if c.Slug == "" {
			continue
		}
		if _, dup := seen[k]; dup {
			continue
		}
		seen[k] = struct{}{}
		dst = append(dst, c)
	}
	return dst
}

func (b *Backend) InventoryProgress(ctx context.Context, s platform.Session) ([]platform.Progress, error) {
	return b.api.Progress(ctx, s)
}

func (b *Backend) StartWatch(ctx context.Context, s platform.Session, stream platform.Stream) (platform.WatchHandle, error) {
	// Browser-watch path: drive a real, playing IVS <video> in the sidecar
	// — the ONLY path that accrues Kick drop watch-time. The sidecar needs
	// the channel SLUG (it navigates kick.com/<slug>) plus the session
	// cookies, which it injects before navigation.
	if b.browserWatch && b.c != nil {
		name := b.sidecars.nameFor(s.AccountID)
		if name == "" {
			return platform.WatchHandle{}, fmt.Errorf("kick start watch: no sidecar for account %s", s.AccountID)
		}
		cl, err := b.watchClientForName(name)
		if err != nil {
			return platform.WatchHandle{}, fmt.Errorf("kick start watch: dial sidecar %s: %w", name, err)
		}
		// Start the container on demand + wait for readiness.
		if err := b.sidecars.ensureUp(ctx, s.AccountID, func(c context.Context) error {
			_, e := cl.Heartbeat(c, "") // nil error == gRPC server reachable
			return e
		}); err != nil {
			return platform.WatchHandle{}, fmt.Errorf("kick start watch: sidecar up %s: %w", name, err)
		}
		ks, err := decodeSession(s)
		if err != nil {
			return platform.WatchHandle{}, fmt.Errorf("kick start watch: decode session: %w", err)
		}
		handle, err := cl.StartWatch(ctx, toProto(ks), stream.Channel)
		if err != nil {
			return platform.WatchHandle{}, fmt.Errorf("kick start watch (browser) %s: %w", stream.Channel, err)
		}
		b.sidecars.touch(s.AccountID)
		return platform.WatchHandle{Channel: stream.Channel, Internal: kickBrowserWatch{handle: handle, client: cl, accountID: s.AccountID}}, nil
	}

	// No browser sidecar: Kick watch is unavailable. Real IVS video
	// playback is the only path that accrues drop watch-time, so there is
	// no non-browser fallback — fail loudly rather than run a watch that
	// earns nothing.
	return platform.WatchHandle{}, fmt.Errorf("kick start watch %s: browser sidecar required (no accruing watch path without it)", stream.Channel)
}

func (b *Backend) Heartbeat(ctx context.Context, h platform.WatchHandle) error {
	switch w := h.Internal.(type) {
	case kickBrowserWatch:
		alive, err := w.client.Heartbeat(ctx, w.handle)
		if err != nil {
			return fmt.Errorf("kick heartbeat (browser) %q: %w", h.Channel, err)
		}
		if !alive {
			return fmt.Errorf("kick: browser watch not playing for %q", h.Channel)
		}
		b.sidecars.touch(w.accountID)
		return nil
	default:
		return fmt.Errorf("kick: invalid watch handle")
	}
}

func (b *Backend) StopWatch(ctx context.Context, h platform.WatchHandle) error {
	switch w := h.Internal.(type) {
	case kickBrowserWatch:
		if err := w.client.StopWatch(ctx, w.handle); err != nil {
			return fmt.Errorf("kick stop watch (browser) %q: %w", h.Channel, err)
		}
		return nil
	default:
		return nil
	}
}

func (b *Backend) Claim(ctx context.Context, s platform.Session, drop platform.DropBenefit) error {
	// Kick claim needs reward_id + campaign_id. drop.ID is the reward id;
	// drop.CampaignID is the campaign.
	return b.api.Claim(ctx, s, drop.ID, drop.CampaignID)
}

// SweepCompletedClaims claims every Kick reward that has reached 100% and is
// not yet granted. A single Kick watch advances many rewards at once (the open
// "General Drops" tiers + the Team campaign for the channel being watched), but
// the watcher tracks only one currentBenefit — so its own claim flow would miss
// the siblings. This sweep closes that gap: it reads /drops/progress (which
// carries each reward's fraction + claimed flag + parent campaign id) and POSTs
// a claim for any reward at fraction>=1.0 that isn't already claimed.
//
// Kick appears to auto-grant some rewards (a reward observed at progress:1 came
// back claimed:true with no bot claim in its history), but that is not
// guaranteed for every campaign, so we POST regardless and treat a claim error
// on an already-granted reward as benign (logged, not fatal). Already-claimed
// rewards are skipped. Satisfies platform.CompletedSweeper.
func (b *Backend) SweepCompletedClaims(ctx context.Context, s platform.Session) ([]platform.ClaimedReward, error) {
	rewards, err := b.api.progressDetail(ctx, s)
	if err != nil {
		return nil, fmt.Errorf("kick sweep progress: %w", err)
	}
	var claimed []platform.ClaimedReward
	for _, r := range rewards {
		if r.Claimed || r.Fraction < 1.0 || r.RewardID == "" || r.CampaignID == "" {
			continue
		}
		if err := b.api.Claim(ctx, s, r.RewardID, r.CampaignID); err != nil {
			// Kick may have already auto-granted it server-side, in which
			// case the claim POST can 4xx — that's not a real failure, the
			// reward is the user's. Log and move on; the next progress poll
			// will show claimed:true and stop re-attempting.
			slog.Info("kick sweep: claim attempt returned error (likely already granted)",
				"kind", "claim", "account", s.AccountID, "reward", r.RewardID, "name", r.Name, "err", err)
			continue
		}
		slog.Info("kick sweep: claimed completed reward",
			"kind", "claim", "account", s.AccountID, "reward", r.RewardID, "campaign", r.CampaignID, "name", r.Name)
		claimed = append(claimed, platform.ClaimedReward{Game: "Rust", Title: r.Name})
	}
	return claimed, nil
}

var _ platform.CompletedSweeper = (*Backend)(nil)

// FetchImage proxies a Kick CDN asset over the utls transport so the
// browser can render it (files.kick.com 403s direct hotlinks). Returns
// the bytes + Content-Type + upstream status.
func (b *Backend) FetchImage(ctx context.Context, rawURL string) ([]byte, string, int, error) {
	return b.api.FetchImage(ctx, rawURL)
}

// VerifyAuth probes the Kick session by fetching the drops campaigns over
// the authed utls transport. A hard failure (dead cookies / CF block /
// expired session) surfaces as an error. Satisfies platform.AuthChecker.
func (b *Backend) VerifyAuth(ctx context.Context, s platform.Session) error {
	if _, err := b.api.Campaigns(ctx, s); err != nil {
		return fmt.Errorf("kick campaigns probe: %w", err)
	}
	return nil
}

var _ platform.AuthChecker = (*Backend)(nil)
