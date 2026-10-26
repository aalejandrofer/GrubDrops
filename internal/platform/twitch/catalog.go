package twitch

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// ErrCatalogUnavailable means the token cannot list the drops dashboard:
// Twitch answers currentUser:null, which is what TV-client tokens get.
var ErrCatalogUnavailable = errors.New("twitch: drops dashboard unavailable for this token")

// catalogDetailWorkers bounds the DropCampaignDetails fan-out of one
// Catalog call.
const catalogDetailWorkers = 4

// catalogData decodes ViewerDropsDashboard with a nullable currentUser, so a
// TV token's null answer is told apart from an empty dashboard.
type catalogData struct {
	CurrentUser *struct {
		DropCampaigns []struct {
			ID             string `json:"id"`
			Name           string `json:"name"`
			Status         string `json:"status"`
			StartAtRaw     string `json:"startAt"`
			EndAtRaw       string `json:"endAt"`
			AccountLinkURL string `json:"accountLinkURL"`
			Self           struct {
				IsAccountConnected bool `json:"isAccountConnected"`
			} `json:"self"`
			Game struct {
				ID          string `json:"id"`
				DisplayName string `json:"displayName"`
				BoxArtURL   string `json:"boxArtURL"`
			} `json:"game"`
		} `json:"dropCampaigns"`
	} `json:"currentUser"`
}

// Catalog lists every ACTIVE drop campaign on the account's drops dashboard
// with its rewards, regardless of any game whitelist. It needs an
// Android-client token. It has no side effects on mining state: no allow-list
// capture and no PubSub. A campaign whose details fail keeps its row with no
// benefits. Use a Backend dedicated to catalog reads, since it rebinds the
// details user id to s.
func (b *Backend) Catalog(ctx context.Context, s platform.Session) ([]platform.Campaign, error) {
	if s.ClientID == ClientTV {
		return nil, ErrCatalogUnavailable
	}
	b.c.bind(s)
	var page catalogData
	vars := map[string]any{"fetchRewardCampaigns": false}
	if err := b.c.gql(ctx, s.AccessToken, OpCampaigns, vars, &page); err != nil {
		return nil, fmt.Errorf("list campaigns: %w", err)
	}
	if page.CurrentUser == nil {
		return nil, ErrCatalogUnavailable
	}
	// DropCampaignDetails resolves channelLogin as user(id:), so it must be
	// this session's user id.
	uid := s.UserID
	if uid == "" {
		id, err := b.disc.resolveCurrentUserID(ctx, s)
		if err != nil {
			return nil, fmt.Errorf("resolve user id: %w", err)
		}
		uid = id
	}
	b.disc.mu.Lock()
	if b.disc.userID != uid {
		b.disc.detailsCache = nil
	}
	b.disc.userID = uid
	b.disc.mu.Unlock()

	out := make([]platform.Campaign, 0, len(page.CurrentUser.DropCampaigns))
	for _, c := range page.CurrentUser.DropCampaigns {
		if c.Status != "ACTIVE" || c.ID == "" || strings.ContainsAny(c.ID, "| ") {
			continue
		}
		out = append(out, platform.Campaign{
			ID:                 c.ID,
			Platform:           "twitch",
			Game:               c.Game.DisplayName,
			GameID:             c.Game.ID,
			BoxArtURL:          BoxArtURL(c.Game.BoxArtURL, c.Game.ID),
			Name:               c.Name,
			StartsAt:           parseISO(c.StartAtRaw),
			EndsAt:             parseISO(c.EndAtRaw),
			Status:             "active",
			AccountLinked:      c.Self.IsAccountConnected,
			AccountLinkChecked: true,
			AccountLinkURL:     c.AccountLinkURL,
			Kind:               "drop",
		})
	}

	jobs := make(chan int)
	var wg sync.WaitGroup
	for range catalogDetailWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				benefits, _, err := b.disc.fetchDetails(ctx, s, out[i].ID)
				if err != nil {
					slog.Debug("twitch catalog: campaign details failed", "campaign", out[i].ID, "err", err)
					continue
				}
				out[i].Benefits = benefits
			}
		}()
	}
	for i := range out {
		if ctx.Err() != nil {
			break
		}
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	return out, nil
}
