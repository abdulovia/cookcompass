// Package fetch is a polite HTTP client for scrapers: it identifies itself
// with a clear User-Agent, obeys robots.txt, waits between requests to the
// same host, and caches responses on disk so re-runs and offline work don't
// hit the network.
package fetch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Errors.
var (
	ErrDisallowed = errors.New("disallowed by robots.txt")
	ErrStatus     = errors.New("unexpected HTTP status")
	ErrTooLarge   = errors.New("response too large")
)

// MaxBody caps a single response (pages beyond this are not recipes).
const MaxBody = 5 << 20

// Client fetches pages politely. The zero value is not usable; use New.
type Client struct {
	HTTP        *http.Client
	UserAgent   string
	CacheDir    string        // "" disables the disk cache
	CacheTTL    time.Duration // 0 = cache forever
	MinInterval time.Duration // per host

	// Hooks for tests.
	Now   func() time.Time
	Sleep func(ctx context.Context, d time.Duration) error

	mu     sync.Mutex
	last   map[string]time.Time
	robots map[string]*Robots
}

// New returns a Client.
func New(userAgent, cacheDir string, ttl, minInterval, timeout time.Duration) *Client {
	return &Client{
		HTTP:        &http.Client{Timeout: timeout},
		UserAgent:   userAgent,
		CacheDir:    cacheDir,
		CacheTTL:    ttl,
		MinInterval: minInterval,
	}
}

func (c *Client) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c *Client) sleep(ctx context.Context, d time.Duration) error {
	if c.Sleep != nil {
		return c.Sleep(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// Get returns the body at rawURL. "file:" URLs read local files (bundled
// samples and offline captures) and skip robots, rate limiting and caching.
func (c *Client) Get(ctx context.Context, rawURL string) ([]byte, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	switch u.Scheme {
	case "file":
		p := u.Path
		if u.Opaque != "" {
			p = u.Opaque
		}
		return readLimited(p)
	case "http", "https":
	default:
		return nil, fmt.Errorf("unsupported URL scheme %q", u.Scheme)
	}
	if body, ok := c.cached(rawURL); ok {
		return body, nil
	}
	rob, err := c.robotsFor(ctx, u)
	if err != nil {
		return nil, err
	}
	if !rob.Allowed(u.RequestURI()) {
		return nil, fmt.Errorf("%s: %w", rawURL, ErrDisallowed)
	}
	body, status, err := c.do(ctx, u, rob.CrawlDelay)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("%s: %w %d", rawURL, ErrStatus, status)
	}
	c.store(rawURL, body)
	return body, nil
}

// do performs a rate-limited GET.
func (c *Client) do(ctx context.Context, u *url.URL, crawlDelay time.Duration) ([]byte, int, error) {
	if err := c.wait(ctx, u.Host, crawlDelay); err != nil {
		return nil, 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/json;q=0.9,*/*;q=0.5")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxBody+1))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	if len(body) > MaxBody {
		return nil, resp.StatusCode, ErrTooLarge
	}
	return body, resp.StatusCode, nil
}

// wait blocks until MinInterval (or the site's Crawl-delay, if longer)
// has passed since the last request to host.
func (c *Client) wait(ctx context.Context, host string, crawlDelay time.Duration) error {
	interval := max(c.MinInterval, min(crawlDelay, 30*time.Second))
	c.mu.Lock()
	if c.last == nil {
		c.last = map[string]time.Time{}
	}
	next := c.last[host].Add(interval)
	now := c.now()
	wait := next.Sub(now)
	if wait < 0 {
		wait = 0
	}
	c.last[host] = now.Add(wait)
	c.mu.Unlock()
	if wait > 0 {
		return c.sleep(ctx, wait)
	}
	return nil
}

// robotsFor fetches and caches robots.txt for u's host. A missing file
// (4xx) allows everything; a server error disallows everything, as the
// robots exclusion standard recommends.
func (c *Client) robotsFor(ctx context.Context, u *url.URL) (*Robots, error) {
	key := u.Scheme + "://" + u.Host
	c.mu.Lock()
	if r, ok := c.robots[key]; ok {
		c.mu.Unlock()
		return r, nil
	}
	c.mu.Unlock()
	var rob *Robots
	if body, ok := c.cached(key + "/robots.txt"); ok {
		rob = ParseRobots(string(body), c.UserAgent)
	} else {
		ru := &url.URL{Scheme: u.Scheme, Host: u.Host, Path: "/robots.txt"}
		body, status, err := c.do(ctx, ru, 0)
		switch {
		case err != nil:
			return nil, fmt.Errorf("robots.txt for %s: %w", u.Host, err)
		case status >= 500:
			rob = &Robots{disallowAll: true}
		case status >= 400:
			rob = &Robots{}
		default:
			rob = ParseRobots(string(body), c.UserAgent)
			c.store(key+"/robots.txt", body)
		}
	}
	c.mu.Lock()
	if c.robots == nil {
		c.robots = map[string]*Robots{}
	}
	c.robots[key] = rob
	c.mu.Unlock()
	return rob, nil
}

func (c *Client) cachePath(key string) string {
	sum := sha256.Sum256([]byte(key))
	h := hex.EncodeToString(sum[:])
	return filepath.Join(c.CacheDir, h[:2], h+".body")
}

func (c *Client) cached(key string) ([]byte, bool) {
	if c.CacheDir == "" {
		return nil, false
	}
	p := c.cachePath(key)
	st, err := os.Stat(p)
	if err != nil || (c.CacheTTL > 0 && c.now().Sub(st.ModTime()) > c.CacheTTL) {
		return nil, false
	}
	b, err := os.ReadFile(p)
	return b, err == nil
}

func (c *Client) store(key string, body []byte) {
	if c.CacheDir == "" {
		return
	}
	p := c.cachePath(key)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return
	}
	tmp := p + ".tmp"
	if os.WriteFile(tmp, body, 0o644) == nil {
		_ = os.Rename(tmp, p)
	}
}

func readLimited(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, MaxBody+1))
	if err != nil {
		return nil, err
	}
	if len(b) > MaxBody {
		return nil, ErrTooLarge
	}
	return b, nil
}

// Robots is a parsed robots.txt for one user agent.
type Robots struct {
	rules       []rule
	disallowAll bool
	CrawlDelay  time.Duration
}

type rule struct {
	allow   bool
	pattern string
}

// ParseRobots extracts the rules that apply to userAgent: the group naming
// its product token if present, otherwise the "*" group.
func ParseRobots(body, userAgent string) *Robots {
	token := strings.ToLower(userAgent)
	if i := strings.IndexAny(token, "/ "); i > 0 {
		token = token[:i]
	}
	type group struct {
		agents []string
		rules  []rule
		delay  time.Duration
	}
	var groups []*group
	var cur *group
	lastWasAgent := false
	for _, line := range strings.Split(body, "\n") {
		if i := strings.Index(line, "#"); i >= 0 {
			line = line[:i]
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		k = strings.ToLower(strings.TrimSpace(k))
		v = strings.TrimSpace(v)
		switch k {
		case "user-agent":
			if cur == nil || !lastWasAgent {
				cur = &group{}
				groups = append(groups, cur)
			}
			cur.agents = append(cur.agents, strings.ToLower(v))
			lastWasAgent = true
			continue
		case "allow", "disallow":
			if cur != nil && (v != "" || k == "allow") {
				cur.rules = append(cur.rules, rule{allow: k == "allow", pattern: v})
			}
		case "crawl-delay":
			if cur != nil {
				if d, err := time.ParseDuration(v + "s"); err == nil {
					cur.delay = d
				}
			}
		}
		lastWasAgent = false
	}
	var star, mine *group
	for _, g := range groups {
		for _, a := range g.agents {
			if a == "*" && star == nil {
				star = g
			}
			if a != "*" && strings.Contains(token, a) && mine == nil {
				mine = g
			}
		}
	}
	chosen := mine
	if chosen == nil {
		chosen = star
	}
	if chosen == nil {
		return &Robots{}
	}
	return &Robots{rules: chosen.rules, CrawlDelay: chosen.delay}
}

// Allowed reports whether path (with query) may be fetched. The longest
// matching rule wins; on a tie, Allow wins.
func (r *Robots) Allowed(path string) bool {
	if r.disallowAll {
		return false
	}
	best, allowed := -1, true
	for _, ru := range r.rules {
		if ru.pattern == "" {
			continue
		}
		if matchRobots(ru.pattern, path) {
			l := len(ru.pattern)
			if l > best || (l == best && ru.allow) {
				best, allowed = l, ru.allow
			}
		}
	}
	return allowed
}

// matchRobots supports "*" wildcards and a trailing "$" anchor.
func matchRobots(pattern, path string) bool {
	anchored := strings.HasSuffix(pattern, "$")
	pattern = strings.TrimSuffix(pattern, "$")
	parts := strings.Split(pattern, "*")
	if !strings.HasPrefix(path, parts[0]) {
		return false
	}
	pos := len(parts[0])
	for _, p := range parts[1:] {
		i := strings.Index(path[pos:], p)
		if i < 0 {
			return false
		}
		pos += i + len(p)
	}
	if anchored {
		if len(parts) > 1 {
			return strings.HasSuffix(path, parts[len(parts)-1])
		}
		return pos == len(path)
	}
	return true
}
