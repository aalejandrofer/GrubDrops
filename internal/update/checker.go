package update

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/store"
)

// Checker polls the GitHub releases API for the latest GrubDrops tag and caches
// it (in memory + kv) so the render path can ask Status() without any network
// call. Best-effort: any fetch error keeps the last-known cached value.
type Checker struct {
	httpClient *http.Client
	repo       string
	apiBase    string // overridable in tests; defaults to https://api.github.com
	s          *store.Settings
	latest     atomic.Value // string
}

// NewChecker builds a checker. The cached latest is seeded from kv so a restart
// keeps the prior answer until the next successful fetch.
func NewChecker(client *http.Client, repo string, s *store.Settings) *Checker {
	c := &Checker{httpClient: client, repo: repo, apiBase: "https://api.github.com", s: s}
	c.latest.Store("")
	if s != nil {
		if v, err := s.LatestRelease(context.Background()); err == nil && v != "" {
			c.latest.Store(v)
		}
	}
	return c
}

func (c *Checker) setLatest(tag string) { c.latest.Store(tag) }

func (c *Checker) getLatest() string {
	if v, ok := c.latest.Load().(string); ok {
		return v
	}
	return ""
}

type ghRelease struct {
	TagName string `json:"tag_name"`
}

// RunOnce fetches the latest release once and caches it. Returns an error on
// any failure; the cached value is left unchanged on error.
func (c *Checker) RunOnce(ctx context.Context) error {
	url := fmt.Sprintf("%s/repos/%s/releases/latest", c.apiBase, c.repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("github releases: status %d", resp.StatusCode)
	}
	var rel ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return err
	}
	if rel.TagName == "" {
		return fmt.Errorf("github releases: empty tag_name")
	}
	c.setLatest(rel.TagName)
	if c.s != nil {
		_ = c.s.SetLatestRelease(ctx, rel.TagName)
		_ = c.s.SetLastReleaseCheck(ctx, time.Now().Unix())
	}
	return nil
}

// Run checks once immediately, then every `every` until ctx is cancelled.
func (c *Checker) Run(ctx context.Context, every time.Duration) {
	if err := c.RunOnce(ctx); err != nil {
		slog.Warn("update check failed", "component", "update", "err", err)
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := c.RunOnce(ctx); err != nil {
				slog.Warn("update check failed", "component", "update", "err", err)
			}
		}
	}
}

// Status reports whether the cached latest release is newer than current, plus
// the latest tag. No network call. Safe to call from the render hot path.
func (c *Checker) Status(current string) (available bool, latest string) {
	latest = c.getLatest()
	return Newer(current, latest), latest
}
