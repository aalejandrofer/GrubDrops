package api

import (
	"context"
	"database/sql"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/aalejandrofer/dropsminer/internal/platform"
	"github.com/aalejandrofer/dropsminer/internal/store"
	"github.com/aalejandrofer/dropsminer/internal/store/gen"
)

type dropsDeps struct {
	q        *gen.Queries
	t        Renderer
	reload   func(context.Context) error
	sessions *store.SessionStore
	registry *platform.Registry
}

// lazyFetchBenefits backfills a campaign's benefits the first time its
// items panel is opened. Discovery only fetches details for whitelisted
// campaigns, so non-whitelisted ones have no benefits persisted ("No items
// recorded"). Fetch on demand via the backend's CampaignDetailer, persist,
// and report whether anything was stored. Best-effort: any failure (no
// session, no detailer, network) returns false and the panel shows empty.
func (d *dropsDeps) lazyFetchBenefits(ctx context.Context, campaignID, plat string) bool {
	if d.registry == nil || d.sessions == nil {
		return false
	}
	b, ok := d.registry.Get(plat)
	if !ok {
		return false
	}
	detailer, ok := b.(platform.CampaignDetailer)
	if !ok {
		return false
	}
	sess, ok := d.sessionForPlatform(ctx, plat)
	if !ok {
		return false
	}
	benefits, err := detailer.CampaignDetails(ctx, sess, campaignID)
	if err != nil || len(benefits) == 0 {
		if err != nil {
			slog.Debug("drops: lazy benefit fetch failed", "campaign", campaignID, "err", err)
		}
		return false
	}
	for _, ben := range benefits {
		if ben.ID == "" {
			continue
		}
		if err := d.q.UpsertBenefit(ctx, gen.UpsertBenefitParams{
			ID:              ben.ID,
			CampaignID:      campaignID,
			Name:            ben.Name,
			RequiredMinutes: int64(ben.RequiredMinutes),
			ImageUrl:        ben.ImageURL,
		}); err != nil {
			slog.Warn("drops: persist lazy benefit failed", "campaign", campaignID, "err", err)
		}
	}
	return true
}

// sessionForPlatform returns the session of the first enabled account on
// the given platform (any account's token can read public campaign
// details).
func (d *dropsDeps) sessionForPlatform(ctx context.Context, plat string) (platform.Session, bool) {
	accs, err := d.q.ListEnabledAccounts(ctx)
	if err != nil {
		return platform.Session{}, false
	}
	for _, a := range accs {
		if a.Platform != plat {
			continue
		}
		if sess, ok, err := d.sessions.Get(ctx, a.ID); err == nil && ok {
			sess.AccountID = a.ID
			return sess, true
		}
	}
	return platform.Session{}, false
}

// linkOverrides returns the set of campaign ids the user manually marked
// "I've linked it" (kv keys store.LinkOverridePrefix, value "1"). Best
// effort — a query error yields an empty set (gate stays on).
func (d *dropsDeps) linkOverrides(ctx context.Context) map[string]bool {
	set := map[string]bool{}
	rows, err := d.q.ListKVByPrefix(ctx, sql.NullString{String: store.LinkOverridePrefix, Valid: true})
	if err != nil {
		return set
	}
	for _, kv := range rows {
		if string(kv.Value) == "1" {
			set[strings.TrimPrefix(kv.Key, store.LinkOverridePrefix)] = true
		}
	}
	return set
}

// dropTab is one of "past" | "current" | "upcoming". The /drops page
// renders a tab strip and only one table at a time. HTMX swaps just the
// table body when the user clicks a tab.
type dropTab string

const (
	tabPast     dropTab = "past"
	tabCurrent  dropTab = "current"
	tabUpcoming dropTab = "upcoming"
)

// dropsRow is the unified row type for all three tabs. The "When" column
// has different meanings per tab:
//   - past:     ended at (or claimed_at for rows that came from a claim)
//   - current:  ends at
//   - upcoming: starts at
//
// BenefitName is only populated for past-claim rows; for campaign-only
// rows we show the campaign name in the Drop column and leave BenefitName
// empty.
type dropsRow struct {
	CampaignID   string // empty for claim-source rows that lack a campaign
	When         string
	Platform     string
	Game         string
	CampaignName string
	BenefitName  string
	AccountName  string
	Kind         string // "drop" | "reward"
	// sortKey is a Unix timestamp used for "ending soonest" sorting
	// across all tabs + the Discoverable list. Past uses ends_at,
	// current uses ends_at, upcoming uses starts_at — i.e. always
	// "next thing to happen for this row".
	sortKey int64
	// rankKey is the whitelist priority of this row's game (lower =
	// higher priority), matching the order the watcher mines in. Used
	// to sort the whitelisted Current list by priority. math.MaxInt
	// for games with no explicit rank so they fall to the bottom.
	rankKey int

	// Collection status (filled per rendered row by attachCollection).
	// ActionOnly = campaign has benefits but none are watch-time (nothing
	// the miner can auto-collect → cross). Collectors = accounts that have
	// claimed at least one benefit, with Full = claimed every watch-time one.
	ActionOnly bool
	Collectors []collectMark

	// Linked is true when the account can earn this campaign (external
	// account connected, or none required). False = whitelisted but the
	// required account isn't linked → shown in a separate "not linked"
	// table and NOT mined. LinkURL is where to connect it (may be empty).
	Linked  bool
	LinkURL string
}

// collectMark is one account's collection state for a campaign, shown as a
// chip in the row's right column ("✓ @login"). Full = claimed every
// watch-time benefit (green tick); otherwise partial (yellow tick).
type collectMark struct {
	Login    string
	Platform string
	Full     bool
}

// dropsPage is what the template sees: the active tab, the three counts
// (so the tab strip can show totals), and the rows to render in the
// table body. UnlistedRows are campaigns discovered (any tab) whose game
// is NOT on any whitelist — rendered in a parallel "discoverable but
// not whitelisted" table below the main one.
type dropsPage struct {
	Tab          dropTab
	PastCount    int
	CurrentCount int
	UpcomingCount int
	Rows         []dropsRow
	UnlinkedRows []dropsRow      // whitelisted but the account isn't linked (Current tab only)
	UnlistedRows []dropsRow      // campaigns whose Game is not on any account whitelist
	Accounts     []dropsAccount  // for the "add to whitelist" dropdown on unlisted rows
	CSRFToken    string          // mirrors templateData.CSRFToken for inline form
}

type dropsAccount struct {
	ID    string
	Label string // "@login (platform)"
}

// allowedGamesUnion returns the effective whitelist union across
// every account, keyed by lowercased name AND slug. For each account:
// per-account rows when present, otherwise the global priority list
// (matching the watcher's loadAccountWhitelist resolution). The
// global list is therefore picked up whenever any account leaves
// its per-account override empty.
//
// Returns (map, true) whenever any row was contributed;
// (nil, false) when there are no accounts AND no global games.
func allowedGamesUnion(ctx context.Context, q *gen.Queries) (map[string]struct{}, bool) {
	accs, err := q.ListAllAccounts(ctx)
	if err != nil {
		return nil, false
	}
	out := map[string]struct{}{}
	anyRow := false
	var globalLoaded bool
	var globalRows []gen.ListGlobalGamesRow
	loadGlobal := func() {
		if globalLoaded {
			return
		}
		globalLoaded = true
		globalRows, _ = q.ListGlobalGames(ctx)
	}
	for _, a := range accs {
		rows, err := q.ListAccountGames(ctx, a.ID)
		if err != nil {
			continue
		}
		if len(rows) == 0 {
			loadGlobal()
			for _, r := range globalRows {
				anyRow = true
				out[strings.ToLower(r.Name)] = struct{}{}
				out[strings.ToLower(r.Slug)] = struct{}{}
			}
			continue
		}
		for _, r := range rows {
			anyRow = true
			out[strings.ToLower(r.Name)] = struct{}{}
			out[strings.ToLower(r.Slug)] = struct{}{}
		}
	}
	// If there are no accounts at all, still include global games so
	// the /drops page reflects what the watcher would mine once
	// accounts are added.
	if len(accs) == 0 {
		loadGlobal()
		for _, r := range globalRows {
			anyRow = true
			out[strings.ToLower(r.Name)] = struct{}{}
			out[strings.ToLower(r.Slug)] = struct{}{}
		}
	}
	if !anyRow {
		return nil, false
	}
	return out, true
}

// gameRankUnion returns the whitelist priority rank for each game,
// keyed by lowercased name AND slug, mirroring allowedGamesUnion's
// resolution (per-account rows, else the global priority list). When a
// game appears for multiple accounts the MIN rank wins (highest
// priority). Lower rank = higher priority. Games absent from the map
// have no explicit priority. Used to order the /drops whitelisted list
// the same way the watcher mines.
func gameRankUnion(ctx context.Context, q *gen.Queries) map[string]int {
	out := map[string]int{}
	put := func(key string, rank int) {
		key = strings.ToLower(key)
		if cur, ok := out[key]; !ok || rank < cur {
			out[key] = rank
		}
	}
	accs, _ := q.ListAllAccounts(ctx)
	var globalLoaded bool
	var globalRows []gen.ListGlobalGamesRow
	loadGlobal := func() {
		if globalLoaded {
			return
		}
		globalLoaded = true
		globalRows, _ = q.ListGlobalGames(ctx)
	}
	applyGlobal := func() {
		loadGlobal()
		for _, r := range globalRows {
			put(r.Name, int(r.Rank))
			put(r.Slug, int(r.Rank))
		}
	}
	for _, a := range accs {
		rows, err := q.ListAccountGames(ctx, a.ID)
		if err != nil {
			continue
		}
		if len(rows) == 0 {
			applyGlobal()
			continue
		}
		for _, r := range rows {
			put(r.Name, int(r.Rank))
			put(r.Slug, int(r.Rank))
		}
	}
	if len(accs) == 0 {
		applyGlobal()
	}
	return out
}

// rankFor returns the priority rank for a game, or math.MaxInt when the
// game has no explicit whitelist rank (sorts last).
func rankFor(ranks map[string]int, game string) int {
	if r, ok := ranks[strings.ToLower(game)]; ok {
		return r
	}
	return math.MaxInt
}

// passesWhitelist returns true if game is on the whitelist union.
// When no whitelist is configured at all (no account opted in to any
// game), every row falls into the Discoverable tab so the operator
// has a place to start picking games from.
func passesWhitelist(allow map[string]struct{}, hasWhitelist bool, game string) bool {
	if !hasWhitelist {
		return false
	}
	_, ok := allow[strings.ToLower(game)]
	return ok
}

func (d *dropsDeps) list(w http.ResponseWriter, r *http.Request) {
	tab := dropTab(r.URL.Query().Get("tab"))
	switch tab {
	case tabPast, tabCurrent, tabUpcoming:
	default:
		tab = tabCurrent
	}

	allow, hasWhitelist := allowedGamesUnion(r.Context(), d.q)
	now := time.Now().Unix()
	const limit = 200

	pastRows, currentRows, upcomingRows, unlistedRows, err := d.collectAll(r.Context(), allow, hasWhitelist, now, limit, tab)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Populate accounts dropdown for the inline "add to whitelist"
	// affordance on unlisted rows. Best-effort: empty dropdown disables
	// the button server-side.
	var accountsForPick []dropsAccount
	if accs, err := d.q.ListAllAccounts(r.Context()); err == nil {
		for _, a := range accs {
			accountsForPick = append(accountsForPick, dropsAccount{
				ID:    a.ID,
				Label: "@" + a.Login + " (" + a.Platform + ")",
			})
		}
	}

	page := dropsPage{
		Tab:           tab,
		PastCount:     len(pastRows),
		CurrentCount:  len(currentRows),
		UpcomingCount: len(upcomingRows),
		UnlistedRows:  unlistedRows,
		Accounts:      accountsForPick,
		CSRFToken:     csrfToken(r),
	}
	switch tab {
	case tabPast:
		page.Rows = pastRows
	case tabCurrent:
		// Split the whitelisted Current list: linked → mineable (Rows),
		// not-linked → a separate table so the operator sees what they'd
		// mine once the account is connected. The watcher already skips
		// the not-linked ones. A manual "I've linked it" override promotes
		// a row into the mineable list (matches the watcher's ForceLinked).
		overrides := d.linkOverrides(r.Context())
		for _, row := range currentRows {
			if row.Linked || overrides[row.CampaignID] {
				page.Rows = append(page.Rows, row)
			} else {
				page.UnlinkedRows = append(page.UnlinkedRows, row)
			}
		}
	case tabUpcoming:
		page.Rows = upcomingRows
	}

	// Collection chips for the not-linked rows too (so they show context).
	for i := range page.UnlinkedRows {
		d.attachCollection(r.Context(), &page.UnlinkedRows[i])
	}

	// Collection status (cross / per-account ticks) for the rows actually
	// rendered in this tab — keeps the per-row benefit+claim queries bounded.
	for i := range page.Rows {
		d.attachCollection(r.Context(), &page.Rows[i])
	}

	// HTMX partial — used when the user clicks a tab. We just swap the
	// table body so the page chrome stays put.
	if r.Header.Get("HX-Request") == "true" {
		renderPartial(w, d.t, "drops_table", page)
		return
	}
	render(w, d.t, "drops.html", templateData{
		AuthedAdmin: true, CSRFToken: csrfToken(r), Active: "drops",
		Page: page,
	})
}

// collectAll runs all three queries (past, current, upcoming) and returns
// rows filtered by the whitelist. PAST also unions in claim history so
// that drops the operator has already claimed appear even if the campaign
// row was evicted. The unlisted slice mirrors whichever tab the caller
// asked for (`tab` arg) so Discoverable always matches the active tab —
// not a confusing cross-tab union.
func (d *dropsDeps) collectAll(
	ctx context.Context,
	allow map[string]struct{}, hasWhitelist bool,
	now int64, limit int64, tab dropTab,
) (past, current, upcoming, unlisted []dropsRow, err error) {
	// Priority ranks so the whitelisted Current list mirrors the
	// watcher's mining order rather than ending-soonest.
	ranks := gameRankUnion(ctx, d.q)
	// Past: campaigns ended before now.
	pastCamps, err := d.q.ListPastCampaigns(ctx, gen.ListPastCampaignsParams{EndsAt: now, Limit: limit})
	if err != nil {
		return nil, nil, nil, nil, err
	}
	var unlistedPast, unlistedCurrent, unlistedUpcoming []dropsRow
	past = make([]dropsRow, 0, len(pastCamps))
	for _, c := range pastCamps {
		row := dropsRow{
			CampaignID:   c.ID,
			When:         time.Unix(c.EndsAt, 0).UTC().Format("2006-01-02 15:04"),
			Platform:     c.Platform,
			Game:         c.Game,
			CampaignName: c.Name,
			Kind:         c.Kind,
			sortKey:      c.EndsAt,
		}
		if passesWhitelist(allow, hasWhitelist, c.Game) {
			past = append(past, row)
		} else {
			unlistedPast = append(unlistedPast, row)
		}
	}

	// (Claim-history is unioned into PAST below, AFTER current+upcoming are
	// known — a claim on a still-running campaign must NOT appear in Past
	// too, since that campaign already shows in Current. See the relocated
	// block after the upcoming loop.)

	// Current: starts_at <= now < ends_at.
	currentCamps, err := d.q.ListCurrentCampaigns(ctx, gen.ListCurrentCampaignsParams{
		StartsAt: now, EndsAt: now, Limit: limit,
	})
	if err != nil {
		return nil, nil, nil, nil, err
	}
	current = make([]dropsRow, 0, len(currentCamps))
	for _, c := range currentCamps {
		row := dropsRow{
			CampaignID:   c.ID,
			When:         time.Unix(c.EndsAt, 0).UTC().Format("2006-01-02 15:04"),
			Platform:     c.Platform,
			Game:         c.Game,
			CampaignName: c.Name,
			Kind:         c.Kind,
			sortKey:      c.EndsAt,
			rankKey:      rankFor(ranks, c.Game),
			Linked:       c.AccountLinked != 0,
			LinkURL:      c.AccountLinkUrl,
		}
		if passesWhitelist(allow, hasWhitelist, c.Game) {
			current = append(current, row)
		} else {
			unlistedCurrent = append(unlistedCurrent, row)
		}
	}

	// Upcoming: starts_at > now.
	upcomingCamps, err := d.q.ListUpcomingCampaigns(ctx, gen.ListUpcomingCampaignsParams{
		StartsAt: now, Limit: limit,
	})
	if err != nil {
		return nil, nil, nil, nil, err
	}
	upcoming = make([]dropsRow, 0, len(upcomingCamps))
	for _, c := range upcomingCamps {
		row := dropsRow{
			CampaignID:   c.ID,
			When:         time.Unix(c.StartsAt, 0).UTC().Format("2006-01-02 15:04"),
			Platform:     c.Platform,
			Game:         c.Game,
			CampaignName: c.Name,
			Kind:         c.Kind,
			sortKey:      c.StartsAt,
		}
		if passesWhitelist(allow, hasWhitelist, c.Game) {
			upcoming = append(upcoming, row)
		} else {
			unlistedUpcoming = append(unlistedUpcoming, row)
		}
	}

	// Past — union in claim history so claimed drops stay visible even after
	// their campaign row is evicted. BUT skip claims whose campaign is still
	// current or upcoming: that campaign already appears in the Current/Upcoming
	// tab, and listing the claim under Past too made the same drop show in two
	// tabs at once. Mutually-exclusive tabs: a claim only lands in Past once its
	// campaign is no longer live.
	liveCampIDs := make(map[string]bool, len(currentCamps)+len(upcomingCamps))
	for _, c := range currentCamps {
		liveCampIDs[c.ID] = true
	}
	for _, c := range upcomingCamps {
		liveCampIDs[c.ID] = true
	}
	claims, err := d.q.ListRecentClaims(ctx, limit)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	for _, row := range claims {
		// Skip stale/empty claim rows (no game AND no campaign name) that
		// would otherwise render as "REWARD · — · —".
		if row.Game == "" && row.CampaignName == "" {
			continue
		}
		if !passesWhitelist(allow, hasWhitelist, row.Game) {
			continue
		}
		if liveCampIDs[row.CampaignID] {
			continue // still current/upcoming — shown there, not in Past
		}
		past = append(past, dropsRow{
			CampaignID:   row.CampaignID,
			When:         time.Unix(row.ClaimedAt, 0).UTC().Format("2006-01-02 15:04"),
			Platform:     row.Platform,
			Game:         row.Game,
			CampaignName: row.CampaignName,
			BenefitName:  row.BenefitName,
			AccountName:  row.AccountName,
			sortKey:      row.ClaimedAt,
		})
	}

	// Pick the per-tab unlisted bucket so Discoverable always matches
	// the active tab. Cross-tab merging was confusing — Past tab would
	// show non-past Discoverable rows.
	switch tab {
	case tabPast:
		unlisted = unlistedPast
	case tabUpcoming:
		unlisted = unlistedUpcoming
	default:
		unlisted = unlistedCurrent
	}

	// Dedupe unlisted by (Platform, Game, CampaignName).
	if len(unlisted) > 1 {
		seen := make(map[string]bool, len(unlisted))
		out := unlisted[:0]
		for _, r := range unlisted {
			k := r.Platform + "|" + r.Game + "|" + r.CampaignName
			if seen[k] {
				continue
			}
			seen[k] = true
			out = append(out, r)
		}
		unlisted = out
	}

	// Ending-soonest sort across every list. sortKey carries ends_at for
	// past/current rows and starts_at for upcoming — i.e. the next thing
	// that will happen for this row. Zero keys sort last so rows
	// missing a timestamp don't jump to the head.
	sortBySoonest := func(xs []dropsRow) {
		sort.SliceStable(xs, func(i, j int) bool {
			ai, aj := xs[i].sortKey, xs[j].sortKey
			if ai == 0 {
				return false
			}
			if aj == 0 {
				return true
			}
			return ai < aj
		})
	}
	// The whitelisted Current list is ordered by whitelist PRIORITY
	// (the order the watcher mines), ending-soonest as the tiebreak.
	// Past/upcoming/unlisted stay ending-soonest.
	sort.SliceStable(current, func(i, j int) bool {
		ri, rj := current[i].rankKey, current[j].rankKey
		if ri != rj {
			return ri < rj
		}
		ai, aj := current[i].sortKey, current[j].sortKey
		if ai == 0 {
			return false
		}
		if aj == 0 {
			return true
		}
		return ai < aj
	})
	sortBySoonest(past)
	sortBySoonest(upcoming)
	sortBySoonest(unlisted)

	return past, current, upcoming, unlisted, nil
}

// items returns the benefits + summary for a single campaign, rendered
// as the HTML partial that hx-get loads into a row's expanded section.
type campaignDetailRow struct {
	ID              string
	Platform        string
	Game            string
	CampaignName    string
	Kind            string
	When            string
	Status          string
	Benefits        []campaignBenefitRow
}

type campaignBenefitRow struct {
	Name            string
	RequiredMinutes int64
	ImageURL        string
	// Collected lists the accounts that have already claimed this benefit
	// (from the claims table) — rendered as per-account marks on the item.
	Collected []collectedMark
}

// collectedMark is one account that claimed a benefit, carrying the
// platform so the mark can be colored (purple=Twitch, green=Kick).
type collectedMark struct {
	Login    string
	Platform string
}

// addWhitelist takes (account_id, name) from the inline form on the
// /drops Discoverable table and reuses the same slug-and-upsert flow
// as the per-account whitelist editor. Redirects back to /drops with
// the current tab preserved.
func (d *dropsDeps) addWhitelist(w http.ResponseWriter, r *http.Request) {
	accID := r.FormValue("account_id")
	name := strings.TrimSpace(r.FormValue("name"))
	if accID == "" || name == "" {
		http.Redirect(w, r, "/drops", http.StatusSeeOther)
		return
	}
	// "__global__" adds to the global priority list (applies to every account
	// that has no per-account override) rather than a single account.
	global := accID == "__global__"
	if !global {
		if _, err := d.q.GetAccount(r.Context(), accID); err != nil {
			http.NotFound(w, r)
			return
		}
	}
	slug := slugifyGame(name)
	if slug == "" {
		http.Redirect(w, r, "/drops", http.StatusSeeOther)
		return
	}
	gameID := "g_" + slug
	if err := d.q.UpsertGame(r.Context(), gen.UpsertGameParams{
		ID: gameID, Name: name, Slug: slug, Priority: 0,
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if global {
		existing, _ := d.q.ListGlobalGames(r.Context())
		rank := int64(len(existing))
		for _, e := range existing {
			if e.ID == gameID {
				rank = e.Rank
				break
			}
		}
		if err := d.q.AddGlobalGame(r.Context(), gen.AddGlobalGameParams{
			GameID: gameID, Rank: rank,
		}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/drops", http.StatusSeeOther)
		return
	}
	existing, _ := d.q.ListAccountGames(r.Context(), accID)
	rank := int64(len(existing))
	for _, e := range existing {
		if e.ID == gameID {
			rank = e.Rank
			break
		}
	}
	if err := d.q.AddAccountGame(r.Context(), gen.AddAccountGameParams{
		AccountID: accID, GameID: gameID, Rank: rank,
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/drops", http.StatusSeeOther)
}

// markLinked handles the manual "I've linked it" toggle on a not-linked
// row. Sets (or clears, when unlink=1) the kv override that the watcher's
// ForceLinked reads, then reloads the scheduler so the campaign starts
// (or stops) being mined immediately. The live /drops/progress check is
// what ultimately confirms the assertion.
func (d *dropsDeps) markLinked(w http.ResponseWriter, r *http.Request) {
	campaignID := strings.TrimSpace(r.FormValue("campaign_id"))
	if campaignID == "" {
		http.Redirect(w, r, "/drops", http.StatusSeeOther)
		return
	}
	key := store.LinkOverridePrefix + campaignID
	if r.FormValue("unlink") == "1" {
		_ = d.q.DeleteKV(r.Context(), key)
	} else {
		if err := d.q.UpsertSettingString(r.Context(), gen.UpsertSettingStringParams{
			Key: key, Value: []byte("1"),
		}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	// Reload so ForceLinked picks up the change without waiting for the
	// next discovery cycle. Non-fatal — the change is persisted regardless.
	if d.reload != nil {
		if err := d.reload(r.Context()); err != nil {
			slog.Warn("scheduler reload after link override failed", "campaign", campaignID, "err", err)
		}
	}
	http.Redirect(w, r, "/drops", http.StatusSeeOther)
}

// attachCollection fills a row's collection status from the campaign's
// benefits + claims. ActionOnly when no benefit is watch-time (cross);
// otherwise a chip per account that has claimed, Full=claimed every
// watch-time benefit. Cheap (2 queries); only called for rendered rows.
func (d *dropsDeps) attachCollection(ctx context.Context, row *dropsRow) {
	if row.CampaignID == "" {
		return
	}
	bens, err := d.q.ListBenefitsForCampaign(ctx, row.CampaignID)
	if err != nil || len(bens) == 0 {
		return
	}
	watch := make(map[string]bool)
	for _, b := range bens {
		if b.RequiredMinutes > 0 {
			watch[b.ID] = true
		}
	}
	if len(watch) == 0 {
		row.ActionOnly = true // only action-required drops — nothing to auto-collect
		return
	}
	claims, err := d.q.ListClaimsForCampaign(ctx, row.CampaignID)
	if err != nil || len(claims) == 0 {
		return
	}
	type acct struct {
		platform string
		got      map[string]bool
	}
	byLogin := make(map[string]*acct)
	for _, c := range claims {
		a := byLogin[c.Login]
		if a == nil {
			a = &acct{platform: c.Platform, got: make(map[string]bool)}
			byLogin[c.Login] = a
		}
		if watch[c.BenefitID] {
			a.got[c.BenefitID] = true
		}
	}
	logins := make([]string, 0, len(byLogin))
	for l := range byLogin {
		logins = append(logins, l)
	}
	sort.Strings(logins)
	for _, l := range logins {
		a := byLogin[l]
		row.Collectors = append(row.Collectors, collectMark{
			Login:    l,
			Platform: a.platform,
			Full:     len(a.got) == len(watch),
		})
	}
}

func (d *dropsDeps) items(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	camp, err := d.q.GetCampaign(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	bens, _ := d.q.ListBenefitsForCampaign(r.Context(), id)
	// Backfill: non-whitelisted campaigns have no benefits persisted
	// (discovery skips their detail fetch). Fetch on demand the first time
	// the panel opens, then re-query.
	if len(bens) == 0 {
		if d.lazyFetchBenefits(r.Context(), id, camp.Platform) {
			bens, _ = d.q.ListBenefitsForCampaign(r.Context(), id)
		}
	}
	detail := campaignDetailRow{
		ID:           camp.ID,
		Platform:     camp.Platform,
		Game:         camp.Game,
		CampaignName: camp.Name,
		Kind:         camp.Kind,
		Status:       camp.Status,
	}
	if camp.EndsAt > 0 {
		detail.When = time.Unix(camp.EndsAt, 0).UTC().Format("2006-01-02 15:04 UTC")
	}
	// Per-benefit COLLECTED marks: which accounts already claimed each benefit.
	collectedByBenefit := map[string][]collectedMark{}
	if claims, err := d.q.ListClaimsForCampaign(r.Context(), id); err == nil {
		for _, c := range claims {
			collectedByBenefit[c.BenefitID] = append(collectedByBenefit[c.BenefitID], collectedMark{
				Login:    c.Login,
				Platform: c.Platform,
			})
		}
	}
	for _, b := range bens {
		img := b.ImageUrl
		// Kick CDN images 403 direct hotlinks (Cloudflare); route them
		// through our utls-backed proxy so the browser can render them.
		if img != "" && detail.Platform == "kick" {
			img = "/img/kick?u=" + url.QueryEscape(img)
		}
		detail.Benefits = append(detail.Benefits, campaignBenefitRow{
			Name:            b.Name,
			RequiredMinutes: b.RequiredMinutes,
			ImageURL:        img,
			Collected:       collectedByBenefit[b.ID],
		})
	}
	renderPartial(w, d.t, "drops_campaign_items", detail)
}
