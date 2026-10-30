package api

import (
	"context"
	"strings"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

type pointBalanceEntry struct {
	balance     *int
	updatedAt   *string
	lastAttempt time.Time
}

type pointBalanceFlight struct{ _ byte }

type pointBalanceJSON struct {
	AccountID string  `json:"account_id"`
	Balance   *int    `json:"balance"`
	UpdatedAt *string `json:"updated_at"`
}

const (
	pointsBalanceTTL = time.Hour
	pointsRetryDelay = 5 * time.Minute
)

func (s *server) pointsFor(platformName, channel string, accountIDs []string) []pointBalanceJSON {
	if platformName != "twitch" {
		return nil
	}
	out := make([]pointBalanceJSON, 0, len(accountIDs))
	seen := make(map[string]struct{}, len(accountIDs))
	for _, accountID := range accountIDs {
		if _, ok := seen[accountID]; ok {
			continue
		}
		seen[accountID] = struct{}{}
		key := accountID + "/" + channel
		now := time.Now()
		s.pointsMu.Lock()
		if s.pointsCache == nil {
			s.pointsCache = make(map[string]pointBalanceEntry)
		}
		if s.pointsInFlight == nil {
			s.pointsInFlight = make(map[string]*pointBalanceFlight)
		}
		entry := s.pointsCache[key]
		due := entry.lastAttempt.IsZero() || now.Sub(entry.lastAttempt) >= pointsRetryDelay
		if entry.updatedAt != nil && now.Sub(parsePointTimestamp(entry.updatedAt)) < pointsBalanceTTL {
			due = false
		}
		if due && s.pointsInFlight[key] == nil {
			entry.lastAttempt = now
			s.pointsCache[key] = entry
			flight := &pointBalanceFlight{}
			s.pointsInFlight[key] = flight
			go s.refreshPointBalance(key, accountID, channel, flight)
		}
		s.pointsMu.Unlock()
		out = append(out, pointBalanceJSON{AccountID: accountID, Balance: entry.balance, UpdatedAt: entry.updatedAt})
	}
	return out
}

func parsePointTimestamp(v *string) time.Time {
	if v == nil {
		return time.Time{}
	}
	t, _ := time.Parse(time.RFC3339, *v)
	return t
}

func (s *server) refreshPointBalance(key, accountID, channel string, flight *pointBalanceFlight) {
	defer func() {
		s.pointsMu.Lock()
		if s.pointsInFlight[key] == flight {
			delete(s.pointsInFlight, key)
		}
		s.pointsMu.Unlock()
	}()
	ctx, cancel := context.WithTimeout(s.rootCtx(), 20*time.Second)
	defer cancel()
	if s.d.Registry == nil || s.d.Sessions == nil {
		return
	}
	backend, ok := s.d.Registry.Get("twitch")
	if !ok {
		return
	}
	reader, ok := backend.(platform.ChannelPointsBalanceReader)
	if !ok {
		return
	}
	session, exists, err := s.d.Sessions.Get(ctx, accountID)
	if err != nil || !exists {
		return
	}
	session.AccountID = accountID
	balance, err := reader.ChannelPointsBalance(ctx, session, channel)
	if err != nil {
		return
	}
	updatedAt := time.Now().UTC().Format(time.RFC3339)
	s.pointsMu.Lock()
	if s.pointsInFlight[key] != flight {
		s.pointsMu.Unlock()
		return
	}
	entry := s.pointsCache[key]
	entry.balance = &balance
	entry.updatedAt = &updatedAt
	s.pointsCache[key] = entry
	s.pointsMu.Unlock()
	if s.d.Hub != nil {
		s.d.Hub.Publish(Event{Name: EventForceWatch, Data: struct{}{}})
	}
}

func (s *server) forgetPointBalances(accountID string) {
	s.pointsMu.Lock()
	defer s.pointsMu.Unlock()
	prefix := accountID + "/"
	for key := range s.pointsCache {
		if strings.HasPrefix(key, prefix) {
			delete(s.pointsCache, key)
			delete(s.pointsInFlight, key)
		}
	}
}
