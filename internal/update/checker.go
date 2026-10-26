package update

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
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

	runMu     sync.Mutex // serializes RunOnce so KV writes never interleave
	mu        sync.RWMutex
	notesURL  string
	notes     []string
	checkedAt time.Time
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
		ctx := context.Background()
		c.notesURL, _ = s.LatestReleaseURL(ctx)
		c.notes, _ = s.LatestReleaseNotes(ctx)
		if ts, err := s.LastReleaseCheck(ctx); err == nil && ts > 0 {
			c.checkedAt = time.Unix(ts, 0)
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
	Body    string `json:"body"`
	HTMLURL string `json:"html_url"`
}

// maxNotes caps how many release bullets are kept.
const maxNotes = 5

// parseNotes returns the first maxNotes bullet lines ("- " or "* ") of a
// release body with the marker stripped.
func parseNotes(body string) []string {
	var out []string
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "- ") && !strings.HasPrefix(line, "* ") {
			continue
		}
		out = append(out, strings.TrimSpace(line[2:]))
		if len(out) == maxNotes {
			break
		}
	}
	return out
}

// Release returns the cached latest tag, release page URL, highlights and the
// time of the last successful check. No network call.
func (c *Checker) Release() (latest, notesURL string, notes []string, checkedAt time.Time) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.getLatest(), c.notesURL, append([]string(nil), c.notes...), c.checkedAt
}

// RunOnce fetches the latest release once and caches it. Returns an error on
// any failure; the cached value is left unchanged on error.
func (c *Checker) RunOnce(ctx context.Context) error {
	c.runMu.Lock()
	defer c.runMu.Unlock()
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
	now := time.Now()
	notes := parseNotes(rel.Body)
	c.mu.Lock()
	c.latest.Store(rel.TagName)
	c.notesURL, c.notes, c.checkedAt = rel.HTMLURL, notes, now
	c.mu.Unlock()
	if c.s != nil {
		_ = c.s.SetLatestRelease(ctx, rel.TagName)
		_ = c.s.SetLatestReleaseURL(ctx, rel.HTMLURL)
		_ = c.s.SetLatestReleaseNotes(ctx, notes)
		_ = c.s.SetLastReleaseCheck(ctx, now.Unix())
	}
	return nil
}

// autoOn reports whether the background check is enabled (default true).
func (c *Checker) autoOn(ctx context.Context) bool {
	if c.s == nil {
		return true
	}
	on, err := c.s.UpdateAuto(ctx)
	return err != nil || on
}

func (c *Checker) tick(ctx context.Context) {
	if !c.autoOn(ctx) {
		return
	}
	if err := c.RunOnce(ctx); err != nil {
		slog.Warn("update check failed", "component", "update", "err", err)
	}
}

// Run checks once immediately, then every `every` until ctx is cancelled. A
// check is skipped while the update_auto setting is off.
func (c *Checker) Run(ctx context.Context, every time.Duration) {
	c.tick(ctx)
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c.tick(ctx)
		}
	}
}

// Status reports whether the cached latest release is newer than current, plus
// the latest tag. No network call. Safe to call from the render hot path.
func (c *Checker) Status(current string) (available bool, latest string) {
	latest = c.getLatest()
	return Newer(current, latest), latest
}
