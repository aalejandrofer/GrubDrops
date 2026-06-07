package twitch

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// sameGame compares two Twitch game/category display names. Both sides
// originate from Twitch (campaign.game.displayName vs the live stream's
// category), so a normalized case/space-insensitive compare is enough to
// catch "PUBG: BATTLEGROUNDS" vs "PUBG: Battlegrounds" etc.
func sameGame(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

// liveCheckConcurrency caps the in-flight OpGetStreamInfo requests
// listEligible issues in parallel. Twitch's gql edge tolerates ~20
// concurrent requests per session — DevilXD's bulk_check_online uses
// the same batch size.
const liveCheckConcurrency = 20

type channels struct {
	c *client
}

// streamLiveData decodes the VideoPlayerStreamInfoOverlayChannel
// response. user.id = channel_id (broadcaster), stream.id = broadcast_id,
// broadcastSettings.game.{id,displayName} = the live category. These IDs
// feed the SendEvents heartbeat — Twitch tracks watch minutes against
// (user_id watching, channel_id broadcasting, game_id, broadcast_id).
type streamLiveData struct {
	User struct {
		ID                string `json:"id"`
		Login             string `json:"login"`
		BroadcastSettings struct {
			Title string `json:"title"`
			Game  *struct {
				ID          string `json:"id"`
				DisplayName string `json:"displayName"`
			} `json:"game"`
		} `json:"broadcastSettings"`
		Stream *struct {
			ID           string `json:"id"`
			ViewersCount int    `json:"viewersCount"`
			Game         *struct {
				ID          string `json:"id"`
				DisplayName string `json:"displayName"`
			} `json:"game"`
		} `json:"stream"`
	} `json:"user"`
}

// gameDirectoryData decodes the DirectoryPage_Game response — used
// when a campaign's allow.channels list is empty (i.e. all streams of
// the game qualify). Returns the top live drops-enabled streams.
type gameDirectoryData struct {
	Game struct {
		Streams struct {
			Edges []struct {
				Node struct {
					ID           string `json:"id"`
					ViewersCount int    `json:"viewersCount"`
					Game         *struct {
						ID          string `json:"id"`
						DisplayName string `json:"displayName"`
					} `json:"game"`
					Broadcaster struct {
						ID    string `json:"id"`
						Login string `json:"login"`
					} `json:"broadcaster"`
				} `json:"node"`
			} `json:"edges"`
		} `json:"streams"`
	} `json:"game"`
}

// listForGameDirectory falls back to the DirectoryPage_Game query when
// a campaign's allow.channels is empty. Returns up to 30 live
// drops-enabled streams for the given game slug, fully populated with
// ChannelID + BroadcastID + GameID needed by the SendEvents heartbeat.
func (ch *channels) listForGameDirectory(ctx context.Context, sess platform.Session, gameSlug string) ([]platform.Stream, error) {
	if gameSlug == "" {
		return nil, nil
	}
	vars := map[string]any{
		"limit":              30,
		"slug":               gameSlug,
		"imageWidth":         50,
		"includeCostreaming": false,
		"options": map[string]any{
			"broadcasterLanguages": []string{},
			"freeformTags":         nil,
			"includeRestricted":    []string{"SUB_ONLY_LIVE"},
			"recommendationsContext": map[string]any{
				"platform": "web",
			},
			"sort":          "VIEWER_COUNT",
			"systemFilters": []string{"DROPS_ENABLED"},
			"tags":          []string{},
			"requestID":     "JIRA-VXP-2397",
		},
		"sortTypeIsRecency": false,
	}
	var resp gameDirectoryData
	if err := ch.c.gql(ctx, sess.AccessToken, OpGameDirectory, vars, &resp); err != nil {
		return nil, fmt.Errorf("game directory %s: %w", gameSlug, err)
	}
	out := make([]platform.Stream, 0, len(resp.Game.Streams.Edges))
	for _, e := range resp.Game.Streams.Edges {
		if e.Node.Broadcaster.Login == "" {
			continue
		}
		gameID, gameName := "", ""
		if e.Node.Game != nil {
			gameID, gameName = e.Node.Game.ID, e.Node.Game.DisplayName
		}
		out = append(out, platform.Stream{
			Channel:      e.Node.Broadcaster.Login,
			ViewerCount:  e.Node.ViewersCount,
			DropsEnabled: true,
			ChannelID:    e.Node.Broadcaster.ID,
			BroadcastID:  e.Node.ID,
			GameID:       gameID,
			Game:         gameName,
		})
	}
	return out, nil
}

func (ch *channels) listEligible(ctx context.Context, sess platform.Session, c platform.Campaign, allowedLogins []string) ([]platform.Stream, error) {
	if len(allowedLogins) == 0 {
		return nil, nil
	}
	// Parallel liveness probe (P2). Each OpGetStreamInfo call is
	// independent; collapsing N RTTs into ~ceil(N/20) windows cuts
	// pickStream latency from seconds to ~one RTT when allow.channels
	// is long. First error wins to keep the existing failure shape.
	results := make([]platform.Stream, len(allowedLogins))
	hits := make([]bool, len(allowedLogins))
	sem := make(chan struct{}, liveCheckConcurrency)
	probeCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var firstErr error
	var errMu sync.Mutex
	var wg sync.WaitGroup
	for i, login := range allowedLogins {
		i, login := i, login
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			if probeCtx.Err() != nil {
				return
			}
			var sd streamLiveData
			if err := ch.c.gql(probeCtx, sess.AccessToken, OpGetStreamInfo,
				map[string]any{"channel": login}, &sd); err != nil {
				errMu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("stream live %s: %w", login, err)
					cancel()
				}
				errMu.Unlock()
				return
			}
			if sd.User.Stream == nil {
				return
			}
			gameID, gameName := "", ""
			if sd.User.Stream.Game != nil {
				gameID, gameName = sd.User.Stream.Game.ID, sd.User.Stream.Game.DisplayName
			} else if sd.User.BroadcastSettings.Game != nil {
				gameID, gameName = sd.User.BroadcastSettings.Game.ID, sd.User.BroadcastSettings.Game.DisplayName
			}
			// Category gate: an allow-listed channel that's live but
			// streaming a DIFFERENT game earns ZERO drop progress — Twitch
			// only credits watch-time when the stream's category matches
			// the campaign's game. Esports allow-list channels are the
			// usual offender (live, but on "Just Chatting" or another
			// title between matches). Skip those so pickStream advances to
			// a channel that actually serves the drop. Only filter when
			// both sides are known; an empty/unknown stream game falls
			// through (rare metadata gap — the AvailableDropIDs probe in
			// pickStream is the secondary gate).
			if c.Game != "" && gameName != "" && !sameGame(gameName, c.Game) {
				slog.Debug("watcher skip allow-list channel on wrong game",
					"channel", sd.User.Login, "streaming", gameName, "want", c.Game)
				return
			}
			results[i] = platform.Stream{
				Channel:      sd.User.Login,
				ViewerCount:  sd.User.Stream.ViewersCount,
				DropsEnabled: true,
				ChannelID:    sd.User.ID,
				BroadcastID:  sd.User.Stream.ID,
				GameID:       gameID,
				Game:         gameName,
			}
			hits[i] = true
		}()
	}
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	out := make([]platform.Stream, 0, len(allowedLogins))
	for i, hit := range hits {
		if hit {
			out = append(out, results[i])
		}
	}
	return out, nil
}
