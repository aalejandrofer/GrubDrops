package twitch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// Reward codes: Twitch "reward campaigns" (quests such as Minecraft capes)
// hand out a code the user redeems by hand. The drops claim flow never sees
// them. Two read-only sources are combined:
//
//   - Inventory with fetchRewardCampaigns: true lists completed reward
//     campaigns and their rewards (id, name, game, redemptionURL).
//   - RewardCodeModal, the query behind twitch.tv's own inventory "Redeem"
//     dialog, returns one reward's code and its expiry.
//   - OnsiteNotifications_ListNotifications carries the "You received a
//     reward for : NAME! Click here to redeem: CODE." text and the link the
//     user should follow.
//
// Hashes captured 2026-11-01 from DmitryScaletta/twitch-gql-queries
// (operations-all.generated.ts, commit 60233005, 2026-10-30). RewardCodeModal
// matches Alorf/TwitchDropsBot and Raindancer118/twitchlurker, which reads it
// with a TV-client token.
var (
	// OpRewardCodeModal reads one reward's code. Variables: rewardCampaignID,
	// rewardID. Twitch answers "server error" with a null value for rewards
	// without a code, such as badges.
	OpRewardCodeModal = Operation{
		Name: "RewardCodeModal",
		Hash: "b48d2bd6e80366684092d14c97ced7a86497246c4dab0f3d877c7a308eefdd7f",
	}

	// OpOnsiteNotifications lists the account's onsite notifications, newest
	// first.
	OpOnsiteNotifications = Operation{
		Name: "OnsiteNotifications_ListNotifications",
		Hash: "7e2703e78ad59719530ee6daf380ab32eae0adb5752c89a21fbaa0224ea5c0c0",
	}
)

// rewardNotificationsLimit is how many recent notifications one read covers.
const rewardNotificationsLimit = 30

// rewardLookupRetry is how long a reward whose code read failed waits before
// another try. Badges and emotes always fail, so this keeps them cheap.
const rewardLookupRetry = 24 * time.Hour

// rewardLookupSpacing separates two code reads in one pass.
var rewardLookupSpacing = time.Second

// errGQLRefused marks a gql read Twitch refused for this token kind.
var errGQLRefused = errors.New("twitch gql: refused for this login")

// rewardNotifyText matches the reward notification body:
// "You received a reward for : Blue Creeper Cape! Click here to redeem: XXXX-XXXX-XXXX."
var rewardNotifyText = regexp.MustCompile(`(?is)reward\s+for\s*:?\s*(.+?)\s*!\s*.*?redeem\s*:?\s*([A-Za-z0-9]{3,}(?:-[A-Za-z0-9]{3,}){1,7})`)

// rewardCodeShape matches a bare hyphenated code such as XXXX-XXXX-XXXX or
// the 5x5 Mojang form, for bodies that phrase the text differently.
var rewardCodeShape = regexp.MustCompile(`\b[A-Z0-9]{4,6}(?:-[A-Z0-9]{4,6}){2,5}\b`)

// rewardNotice is one reward code read from a notification.
type rewardNotice struct {
	ID, Name, Code, URL string
	At                  time.Time
}

// parseRewardNotice reads the reward name and code from a notification body.
// ok is false when the body carries no code.
func parseRewardNotice(body string) (name, code string, ok bool) {
	text := strings.NewReplacer("**", "", "__", "", "\n", " ").Replace(body)
	if m := rewardNotifyText.FindStringSubmatch(text); m != nil {
		return strings.TrimSpace(m[1]), strings.ToUpper(m[2]), true
	}
	if c := rewardCodeShape.FindString(text); c != "" {
		return "", c, true
	}
	return "", "", false
}

// redeemLink picks the first absolute http or https URL among urls.
func redeemLink(urls ...string) string {
	for _, raw := range urls {
		u, err := url.Parse(strings.TrimSpace(raw))
		if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
			continue
		}
		return u.String()
	}
	return ""
}

// notificationsData is the OnsiteNotifications_ListNotifications reply.
type notificationsData struct {
	CurrentUser *struct {
		Notifications *struct {
			Edges []struct {
				Node *struct {
					ID        string `json:"id"`
					Type      string `json:"type"`
					Body      string `json:"body"`
					CreatedAt string `json:"createdAt"`
					Actions   []struct {
						Type string `json:"type"`
						URL  string `json:"url"`
					} `json:"actions"`
				} `json:"node"`
			} `json:"edges"`
		} `json:"notifications"`
	} `json:"currentUser"`
}

// rewardNotices parses the notifications that carry a reward code, one per
// code.
func rewardNotices(d notificationsData) []rewardNotice {
	if d.CurrentUser == nil || d.CurrentUser.Notifications == nil {
		return nil
	}
	seen := map[string]bool{}
	var out []rewardNotice
	for _, e := range d.CurrentUser.Notifications.Edges {
		n := e.Node
		if n == nil || n.ID == "" {
			continue
		}
		name, code, ok := parseRewardNotice(n.Body)
		if !ok || seen[code] {
			continue
		}
		seen[code] = true
		urls := make([]string, 0, len(n.Actions))
		for _, a := range n.Actions {
			urls = append(urls, a.URL)
		}
		at, _ := time.Parse(time.RFC3339, n.CreatedAt)
		out = append(out, rewardNotice{ID: n.ID, Name: name, Code: code, URL: redeemLink(urls...), At: at})
	}
	return out
}

// rewardInventoryData is the reward-campaign part of the Inventory reply.
type rewardInventoryData struct {
	CurrentUser *struct {
		Inventory *struct {
			CompletedRewardCampaigns []struct {
				ID          string `json:"id"`
				Name        string `json:"name"`
				ExternalURL string `json:"externalURL"`
				Game        *struct {
					DisplayName string `json:"displayName"`
					Name        string `json:"name"`
				} `json:"game"`
				Rewards []struct {
					ID            string `json:"id"`
					Name          string `json:"name"`
					RedemptionURL string `json:"redemptionURL"`
				} `json:"rewards"`
			} `json:"completedRewardCampaigns"`
		} `json:"inventory"`
	} `json:"currentUser"`
}

// inventoryReward is one reward of a completed reward campaign.
type inventoryReward struct {
	ID, CampaignID, Name, Game, URL string
}

// mayHaveCode reports whether a code read is worth a request: game-bound
// rewards carry codes, and so do rewards redeemed off Twitch. Game-less
// rewards pointing at Twitch are badges or emotes.
func (r inventoryReward) mayHaveCode() bool {
	if r.Game != "" {
		return true
	}
	u, err := url.Parse(r.URL)
	if err != nil || u.Host == "" {
		return false
	}
	h := strings.ToLower(u.Hostname())
	return h != "twitch.tv" && !strings.HasSuffix(h, ".twitch.tv")
}

func inventoryRewards(d rewardInventoryData) []inventoryReward {
	if d.CurrentUser == nil || d.CurrentUser.Inventory == nil {
		return nil
	}
	var out []inventoryReward
	for _, c := range d.CurrentUser.Inventory.CompletedRewardCampaigns {
		game := ""
		if c.Game != nil {
			game = c.Game.DisplayName
			if game == "" {
				game = c.Game.Name
			}
		}
		for _, r := range c.Rewards {
			if r.ID == "" || c.ID == "" {
				continue
			}
			name := r.Name
			if name == "" {
				name = c.Name
			}
			out = append(out, inventoryReward{
				ID: r.ID, CampaignID: c.ID, Name: name, Game: game,
				URL: redeemLink(r.RedemptionURL, c.ExternalURL),
			})
		}
	}
	return out
}

// rewardValueData is the RewardCodeModal reply.
type rewardValueData struct {
	CurrentUser *struct {
		Inventory *struct {
			RewardValue *struct {
				Value     string `json:"value"`
				ExpiresAt string `json:"expiresAt"`
			} `json:"rewardValue"`
		} `json:"inventory"`
	} `json:"currentUser"`
}

// refusalMessage reports whether a gql error text says the token may not
// read the field.
func refusalMessage(msg string) bool {
	m := strings.ToLower(msg)
	for _, s := range []string{"unauthorized", "forbidden", "integrity", "not allowed", "permission"} {
		if strings.Contains(m, s) {
			return true
		}
	}
	return false
}

// quietGQL runs a persisted query like client.gql but logs nothing: the
// caller decides how loud a failure is, and the reply carries reward codes,
// which must never reach a log. Refusals return errGQLRefused; a rotated
// hash returns ErrPersistedQueryNotFound.
func (c *client) quietGQL(ctx context.Context, token string, op Operation, variables map[string]any, out any) error {
	body, err := json.Marshal(gqlRequest{
		OperationName: op.Name,
		Variables:     variables,
		Extensions:    gqlExtensions{PersistedQuery: gqlPersistedQuery{Version: 1, Sha256Hash: op.Hash}},
	})
	if err != nil {
		return err
	}
	raw, status, err := c.transport.gqlPost(ctx, token, op.Name, body)
	if err != nil {
		return err
	}
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return fmt.Errorf("twitch gql %s: status %d: %w", op.Name, status, errGQLRefused)
	case status != http.StatusOK:
		return fmt.Errorf("twitch gql %s: status %d", op.Name, status)
	}
	var env gqlResponse
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("twitch gql %s: decode: %w", op.Name, err)
	}
	if persistedQueryNotFound(env.Errors) {
		return fmt.Errorf("twitch gql %s: %w", op.Name, ErrPersistedQueryNotFound)
	}
	empty := len(env.Data) == 0 || string(env.Data) == "null" || string(env.Data) == "{}"
	if len(env.Errors) > 0 && empty {
		msgs := make([]string, 0, len(env.Errors))
		refused := false
		for _, e := range env.Errors {
			msgs = append(msgs, e.Message)
			refused = refused || refusalMessage(e.Message)
		}
		if refused {
			return fmt.Errorf("twitch gql %s: %s: %w", op.Name, strings.Join(msgs, "; "), errGQLRefused)
		}
		return fmt.Errorf("twitch gql %s: %s", op.Name, strings.Join(msgs, "; "))
	}
	if out == nil || empty {
		return nil
	}
	return json.Unmarshal(env.Data, out)
}

// rewardLookups remembers failed code reads per user and reward, so a
// badge is not asked for a code every pass.
var rewardLookups = struct {
	mu     sync.Mutex
	failed map[string]time.Time
}{failed: map[string]time.Time{}}

func rewardLookupDue(key string, now time.Time) bool {
	rewardLookups.mu.Lock()
	defer rewardLookups.mu.Unlock()
	at, ok := rewardLookups.failed[key]
	return !ok || now.Sub(at) >= rewardLookupRetry
}

func rewardLookupFailed(key string, now time.Time) {
	rewardLookups.mu.Lock()
	defer rewardLookups.mu.Unlock()
	rewardLookups.failed[key] = now
}

func rewardLookupDone(key string) {
	rewardLookups.mu.Lock()
	defer rewardLookups.mu.Unlock()
	delete(rewardLookups.failed, key)
}

// readRewardValue reads one reward's code and expiry. ok is false when the
// reward has no code.
func (c *client) readRewardValue(ctx context.Context, token string, r inventoryReward) (code string, expires time.Time, ok bool, err error) {
	var d rewardValueData
	err = c.quietGQL(ctx, token, OpRewardCodeModal, map[string]any{
		"rewardCampaignID": r.CampaignID, "rewardID": r.ID,
	}, &d)
	if err != nil {
		return "", time.Time{}, false, err
	}
	if d.CurrentUser == nil || d.CurrentUser.Inventory == nil || d.CurrentUser.Inventory.RewardValue == nil {
		return "", time.Time{}, false, nil
	}
	v := d.CurrentUser.Inventory.RewardValue
	if strings.TrimSpace(v.Value) == "" {
		return "", time.Time{}, false, nil
	}
	expires, _ = time.Parse(time.RFC3339, v.ExpiresAt)
	return strings.TrimSpace(v.Value), expires, true, nil
}

func normName(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }

// RewardCodes reads the account's reward codes. known reports, per reward
// key, whether a code and an expiry are already stored; a reward with both
// is not read again. The reply never goes to a log. Both sources refused
// returns platform.ErrRewardCodesUnavailable. Satisfies
// platform.RewardCodeReader.
func (b *Backend) RewardCodes(ctx context.Context, s platform.Session, known func(key string) (code, expiry bool)) ([]platform.RewardCode, error) {
	if s.AccessToken == "" {
		return nil, platform.ErrRewardCodesUnavailable
	}
	b.c.bind(s)
	if known == nil {
		known = func(string) (bool, bool) { return false, false }
	}
	token := s.AccessToken

	var notes []rewardNotice
	var nd notificationsData
	nErr := b.c.quietGQL(ctx, token, OpOnsiteNotifications, map[string]any{
		"cursor": "", "displayType": "VIEWER", "language": "en",
		"limit": rewardNotificationsLimit, "shouldLoadLastBroadcast": false,
	}, &nd)
	if nErr == nil {
		if nd.CurrentUser == nil {
			nErr = fmt.Errorf("twitch gql %s: no current user: %w", OpOnsiteNotifications.Name, errGQLRefused)
		} else {
			notes = rewardNotices(nd)
		}
	}

	var rewards []inventoryReward
	var id rewardInventoryData
	iErr := b.c.quietGQL(ctx, token, OpInventory, map[string]any{"fetchRewardCampaigns": true}, &id)
	if iErr == nil {
		if id.CurrentUser == nil {
			iErr = fmt.Errorf("twitch gql %s: no current user: %w", OpInventory.Name, errGQLRefused)
		} else {
			rewards = inventoryRewards(id)
		}
	}

	if nErr != nil && iErr != nil {
		if errors.Is(nErr, errGQLRefused) && errors.Is(iErr, errGQLRefused) {
			return nil, fmt.Errorf("%w: %w", platform.ErrRewardCodesUnavailable, errors.Join(nErr, iErr))
		}
		return nil, errors.Join(nErr, iErr)
	}
	for _, e := range []error{nErr, iErr} {
		switch {
		case e == nil:
		case errors.Is(e, ErrPersistedQueryNotFound):
			slog.Warn("reward codes: a twitch query hash needs refreshing", "kind", "error", "err", e)
		default:
			slog.Debug("reward codes: one source failed", "err", e)
		}
	}

	byName := map[string]int{}
	byCode := map[string]int{}
	for i, n := range notes {
		if n.Name != "" {
			if _, dup := byName[normName(n.Name)]; !dup {
				byName[normName(n.Name)] = i
			}
		}
		byCode[n.Code] = i
	}
	used := make([]bool, len(notes))
	now := time.Now()
	userKey := s.UserID
	if userKey == "" {
		userKey = token
	}
	lookups := 0
	out := make([]platform.RewardCode, 0, len(rewards)+len(notes))
	for _, r := range rewards {
		rc := platform.RewardCode{Key: r.ID, Name: r.Name, Game: r.Game, RedeemURL: r.URL}
		take := func(i int) {
			n := notes[i]
			used[i] = true
			if rc.Code == "" {
				rc.Code = n.Code
			}
			if n.URL != "" {
				rc.RedeemURL = n.URL // the notification's own link is the redeem target
			}
			rc.ReceivedAt = n.At
		}
		if i, ok := byName[normName(r.Name)]; ok && !used[i] {
			take(i)
		}
		hasCode, hasExpiry := known(rc.Key)
		lk := userKey + "\x00" + r.ID
		if (!hasCode || !hasExpiry) && r.mayHaveCode() && rewardLookupDue(lk, now) {
			if lookups > 0 && !sleepCtx(ctx, rewardLookupSpacing) {
				return nil, ctx.Err()
			}
			lookups++
			code, exp, ok, err := b.c.readRewardValue(ctx, token, r)
			switch {
			case err != nil || !ok:
				rewardLookupFailed(lk, now)
				if err != nil {
					slog.Debug("reward codes: code read failed", "reward", r.ID, "err", err)
				}
			default:
				rewardLookupDone(lk)
				rc.Code = code
				rc.ExpiresAt = exp
				if i, ok := byCode[code]; ok && !used[i] {
					take(i)
				}
			}
		}
		if rc.Code == "" && !hasCode {
			continue // no code to show
		}
		out = append(out, rc)
	}
	for i, n := range notes {
		if used[i] {
			continue
		}
		name := n.Name
		if name == "" {
			name = "Twitch reward"
		}
		out = append(out, platform.RewardCode{
			Key: "notification:" + n.ID, Name: name, Code: n.Code, RedeemURL: n.URL, ReceivedAt: n.At,
		})
	}
	return out, nil
}

// sleepCtx waits d or until ctx ends, reporting whether the wait finished.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

var _ platform.RewardCodeReader = (*Backend)(nil)
