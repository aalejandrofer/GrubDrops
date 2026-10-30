package api

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/i18n"
	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/store"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
	"github.com/aalejandrofer/grubdrops/internal/watcher"
)

const (
	channelPointsRefreshInterval = time.Hour
	maxConcurrentPointsRefreshes = 2
	channelPointsFetchTimeout    = 30 * time.Second
)

type channelPointsEntry struct {
	Balance   int
	FetchedAt time.Time
}

type channelPointsCache struct {
	mu         sync.Mutex
	entries    map[string]channelPointsEntry
	attempted  map[string]time.Time
	refreshing map[string]struct{}
}

func (c *channelPointsCache) get(ctx context.Context, accountID, channel string, fetch func(context.Context) (int, error)) (channelPointsEntry, bool) {
	if c == nil || fetch == nil {
		return channelPointsEntry{}, false
	}
	if ctx == nil {
		ctx = context.Background()
	}
	key := accountID + "\x00" + strings.ToLower(channel)

	c.mu.Lock()
	if c.entries == nil {
		c.entries = make(map[string]channelPointsEntry)
	}
	if c.attempted == nil {
		c.attempted = make(map[string]time.Time)
	}
	if c.refreshing == nil {
		c.refreshing = make(map[string]struct{})
	}
	old, ok := c.entries[key]
	if ok && time.Since(old.FetchedAt) < channelPointsRefreshInterval {
		c.mu.Unlock()
		return old, true
	}
	if attemptedAt := c.attempted[key]; !attemptedAt.IsZero() && time.Since(attemptedAt) < channelPointsRefreshInterval {
		c.mu.Unlock()
		return old, ok
	}
	if _, pending := c.refreshing[key]; !pending && len(c.refreshing) < maxConcurrentPointsRefreshes {
		c.refreshing[key] = struct{}{}
		c.attempted[key] = time.Now()
		go c.refresh(ctx, key, fetch)
	}
	c.mu.Unlock()
	return old, ok
}

func (c *channelPointsCache) refresh(parent context.Context, key string, fetch func(context.Context) (int, error)) {
	ctx, cancel := context.WithTimeout(parent, channelPointsFetchTimeout)
	balance, err := fetch(ctx)
	cancel()

	c.mu.Lock()
	delete(c.refreshing, key)
	if err == nil {
		entry := channelPointsEntry{Balance: balance, FetchedAt: time.Now()}
		c.entries[key] = entry
		c.attempted[key] = entry.FetchedAt
	}
	c.mu.Unlock()
}

func cachedChannelPoints(root context.Context, cache *channelPointsCache, sessions *store.SessionStore, reader platform.ChannelPointsBalanceReader, accountID, channel string) (channelPointsEntry, bool) {
	if cache == nil || reader == nil {
		return channelPointsEntry{}, false
	}
	return cache.get(root, accountID, channel, func(ctx context.Context) (int, error) {
		if sessions == nil {
			return 0, fmt.Errorf("account session store unavailable")
		}
		sess, exists, err := sessions.Get(ctx, accountID)
		if err != nil {
			return 0, err
		}
		if !exists {
			return 0, fmt.Errorf("account session not found")
		}
		sess.AccountID = accountID
		return reader.ChannelPointsBalance(ctx, sess, channel)
	})
}

func (d dashboardDeps) channelBalances(a gen.Account, s watcher.Snapshot, lang string) []dashChannel {
	if a.Platform != "twitch" && a.Platform != "kick" {
		return nil
	}
	names := make([]string, 0, 1+len(s.ExtraChannels))
	if s.Channel != "" {
		names = append(names, s.Channel)
	}
	names = append(names, s.ExtraChannels...)

	var reader platform.ChannelPointsBalanceReader
	if a.Platform == "twitch" && d.registry != nil {
		if b, ok := d.registry.Get(a.Platform); ok {
			reader, _ = b.(platform.ChannelPointsBalanceReader)
		}
	}
	seen := make(map[string]struct{}, len(names))
	out := make([]dashChannel, 0, len(names))
	for _, name := range names {
		key := strings.ToLower(name)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}

		item := dashChannel{Name: name, URL: channelURL(a.Platform, name)}
		if reader != nil && d.sessions != nil {
			entry, ok := cachedChannelPoints(d.rootCtx, d.points, d.sessions, reader, a.ID, name)
			if ok {
				item.Points = fmt.Sprintf("%d", entry.Balance)
				item.PointsAvailable = true
				item.PointsUpdated = formatShort(time.Since(entry.FetchedAt), lang) + " " + i18n.T(lang, "time.ago")
			}
		}
		out = append(out, item)
	}
	return out
}
