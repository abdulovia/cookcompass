package fetch

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestParseRobots(t *testing.T) {
	body := `# comment
User-agent: *
Disallow: /private/
Allow: /private/ok
Disallow: /*.pdf$
Crawl-delay: 2

User-agent: BadBot
User-agent: OtherBot
Disallow: /

User-agent: cookcompassbot
Disallow: /no-cookcompass/
Allow: /
`
	star := ParseRobots(body, "SomeCrawler/1.0")
	tests := []struct {
		path string
		want bool
	}{
		{"/", true},
		{"/private/secret", false},
		{"/private/ok/page", true},
		{"/files/menu.pdf", false},
		{"/files/menu.pdf?x=1", true},
		{"/recipes/pancakes", true},
	}
	for _, tc := range tests {
		if got := star.Allowed(tc.path); got != tc.want {
			t.Errorf("* Allowed(%q) = %v", tc.path, got)
		}
	}
	if star.CrawlDelay != 2*time.Second {
		t.Errorf("crawl delay %v", star.CrawlDelay)
	}
	mine := ParseRobots(body, "CookCompassBot/1.0 (+https://example)")
	if mine.Allowed("/no-cookcompass/x") || !mine.Allowed("/private/secret") {
		t.Error("specific group must override *")
	}
	bad := ParseRobots(body, "OtherBot")
	if bad.Allowed("/anything") {
		t.Error("grouped user-agents share rules")
	}
	if !ParseRobots("", "x").Allowed("/") || !ParseRobots("Disallow: /\n", "x").Allowed("/") {
		t.Error("empty or agent-less robots allow everything")
	}
	if !ParseRobots("User-agent: *\nDisallow:\n", "x").Allowed("/a") {
		t.Error("empty Disallow allows")
	}
	if ParseRobots("User-agent: *\nDisallow: /*/print$\n", "x").Allowed("/r/print") {
		t.Error("wildcard + anchor")
	}
	if !ParseRobots("User-agent: *\nDisallow: /*/print$\n", "x").Allowed("/r/print/more") {
		t.Error("anchor must not match longer paths")
	}
	if ParseRobots("User-agent: *\nDisallow: /exact$\n", "x").Allowed("/exact") {
		t.Error("anchored exact path")
	}
}

type site struct {
	mu       sync.Mutex
	hits     map[string]int
	robots   string
	robotsSC int
}

func (s *site) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.hits[r.URL.Path]++
		s.mu.Unlock()
		if r.Header.Get("User-Agent") != "TestBot/1.0" {
			http.Error(w, "no UA", http.StatusBadRequest)
			return
		}
		switch r.URL.Path {
		case "/robots.txt":
			if s.robotsSC != 0 {
				w.WriteHeader(s.robotsSC)
				return
			}
			_, _ = w.Write([]byte(s.robots))
		case "/missing":
			http.NotFound(w, r)
		case "/big":
			_, _ = w.Write([]byte(strings.Repeat("x", MaxBody+10)))
		default:
			_, _ = w.Write([]byte("page " + r.URL.Path))
		}
	})
}

func newClient(t *testing.T) (*Client, *[]time.Duration) {
	c := New("TestBot/1.0", t.TempDir(), time.Hour, time.Second, 5*time.Second)
	var slept []time.Duration
	var mu sync.Mutex
	now := time.Unix(1_700_000_000, 0)
	c.Now = func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	c.Sleep = func(_ context.Context, d time.Duration) error {
		mu.Lock()
		slept = append(slept, d)
		now = now.Add(d)
		mu.Unlock()
		return nil
	}
	return c, &slept
}

func TestGetPoliteness(t *testing.T) {
	s := &site{hits: map[string]int{}, robots: "User-agent: *\nDisallow: /private\nCrawl-delay: 3\n"}
	srv := httptest.NewServer(s.handler())
	defer srv.Close()
	c, slept := newClient(t)
	ctx := context.Background()

	body, err := c.Get(ctx, srv.URL+"/a")
	if err != nil || string(body) != "page /a" {
		t.Fatalf("get: %q %v", body, err)
	}
	if _, err := c.Get(ctx, srv.URL+"/b"); err != nil {
		t.Fatal(err)
	}
	// robots, /a, /b → two waits of the 3s crawl-delay (it exceeds MinInterval).
	if len(*slept) != 2 || (*slept)[0] != 3*time.Second {
		t.Fatalf("rate limit waits: %v", *slept)
	}
	// Cached: no new request.
	if _, err := c.Get(ctx, srv.URL+"/a"); err != nil || s.hits["/a"] != 1 {
		t.Fatalf("cache miss: hits=%d err=%v", s.hits["/a"], err)
	}
	if s.hits["/robots.txt"] != 1 {
		t.Fatalf("robots fetched %d times", s.hits["/robots.txt"])
	}
	if _, err := c.Get(ctx, srv.URL+"/private/x"); !errors.Is(err, ErrDisallowed) || s.hits["/private/x"] != 0 {
		t.Fatalf("robots not enforced: %v", err)
	}
	if _, err := c.Get(ctx, srv.URL+"/missing"); !errors.Is(err, ErrStatus) {
		t.Fatalf("404: %v", err)
	}
	if _, err := c.Get(ctx, srv.URL+"/big"); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("size limit: %v", err)
	}

	// A fresh client re-uses the on-disk cache, including robots.txt.
	c2 := New("TestBot/1.0", c.CacheDir, time.Hour, time.Second, time.Second)
	if _, err := c2.Get(ctx, srv.URL+"/a"); err != nil || s.hits["/a"] != 1 || s.hits["/robots.txt"] != 1 {
		t.Fatalf("disk cache: %v hits=%v", err, s.hits)
	}
	// Expired cache refetches.
	c2.CacheTTL = time.Nanosecond
	time.Sleep(2 * time.Millisecond)
	c2.Sleep = func(context.Context, time.Duration) error { return nil }
	if _, err := c2.Get(ctx, srv.URL+"/a"); err != nil || s.hits["/a"] != 2 {
		t.Fatalf("ttl: %v hits=%d", err, s.hits["/a"])
	}
}

func TestRobotsStatusHandling(t *testing.T) {
	for _, tc := range []struct {
		code int
		ok   bool
	}{{404, true}, {503, false}} {
		s := &site{hits: map[string]int{}, robotsSC: tc.code}
		srv := httptest.NewServer(s.handler())
		c, _ := newClient(t)
		_, err := c.Get(context.Background(), srv.URL+"/page")
		if (err == nil) != tc.ok {
			t.Errorf("robots %d: err=%v", tc.code, err)
		}
		srv.Close()
	}
}

func TestGetErrors(t *testing.T) {
	c, _ := newClient(t)
	ctx := context.Background()
	for _, u := range []string{"ftp://x/y", "::bad", "http://127.0.0.1:1/unreachable"} {
		if _, err := c.Get(ctx, u); err == nil {
			t.Errorf("%s: expected error", u)
		}
	}
	// file: URLs read local files directly.
	p := filepath.Join(t.TempDir(), "sample.html")
	if err := os.WriteFile(p, []byte("<html>ok</html>"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, u := range []string{"file://" + p, "file:" + p} {
		if b, err := c.Get(ctx, u); err != nil || string(b) != "<html>ok</html>" {
			t.Errorf("%s: %q %v", u, b, err)
		}
	}
	if _, err := c.Get(ctx, "file:/does/not/exist"); err == nil {
		t.Error("missing file")
	}
	// Context cancellation during a rate-limit wait.
	real := New("TestBot/1.0", "", 0, time.Hour, time.Second)
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if err := real.wait(cctx, "h", 0); err != nil {
		t.Fatalf("first request should not wait: %v", err)
	}
	if err := real.wait(cctx, "h", 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled wait: %v", err)
	}
}
