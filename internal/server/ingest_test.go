package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/abdulovia/cookcompass/internal/config"
	"github.com/abdulovia/cookcompass/internal/recipes"
)

// fixtureSite serves the ingest fixtures with a robots.txt that blocks /members/.
func fixtureSite(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("User-agent: *\nDisallow: /members/\n"))
	})
	for _, f := range []string{"graph_recipe.html", "array_recipe.html", "no_recipe.html"} {
		body, err := os.ReadFile(filepath.Join("..", "ingest", "testdata", f))
		if err != nil {
			t.Fatal(err)
		}
		mux.HandleFunc("/"+f, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(body) })
	}
	mux.HandleFunc("/members/secret.html", func(w http.ResponseWriter, _ *http.Request) { t.Error("robots.txt ignored") })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

var queueRow = regexp.MustCompile(`id="rq-(\d+)"`)

func TestScrapeQueueApproveFlow(t *testing.T) {
	site := fixtureSite(t)
	targets := filepath.Join(t.TempDir(), "targets.txt")
	list := strings.Join([]string{site.URL + "/graph_recipe.html", site.URL + "/members/secret.html", site.URL + "/no_recipe.html",
		site.URL + "/array_recipe.html"}, "\n")
	if err := os.WriteFile(targets, []byte(list), 0o600); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, func(c *config.Config) {
		c.Scrape.TargetsFile = targets
		c.Scrape.MinInterval = time.Millisecond // keep the test fast; production enforces >= 1s
	})

	// Scrape from the admin queue.
	resp, _ := h.post("/admin/queue/scrape", url.Values{"source": {"jsonld"}})
	if resp.StatusCode != http.StatusSeeOther || !strings.Contains(resp.Header.Get("Location"), "flash=scanned") {
		t.Fatalf("scrape: %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}
	_, body := h.get("/admin/queue")
	rows := queueRow.FindAllStringSubmatch(body, -1)
	if len(rows) != 2 || !strings.Contains(body, "Za&#39;atar Fried Eggs") || !strings.Contains(body, "Weeknight Tomato Pasta") ||
		!strings.Contains(body, "Needs review") {
		t.Fatalf("queue rows: %d", len(rows))
	}

	// Pending recipes are invisible to users.
	if _, body := h.get("/recipes?f=1&q=za"); strings.Contains(body, "Fried Eggs") {
		t.Fatal("pending recipe visible in browse")
	}
	// Approve the eggs via HTMX (row removed), reject the pasta via a plain form.
	eggs := h.mustFindByURL("https://recipes.example.org/zaatar-eggs")
	pasta := h.mustFindByURL("https://kitchen.example.com/tomato-pasta")
	resp, rowBody := h.post("/admin/queue/recipes/"+itoa(eggs.ID)+"/approve", nil, "HX-Request", "true")
	if resp.StatusCode != http.StatusOK || rowBody != "" {
		t.Fatalf("htmx approve: %d %q", resp.StatusCode, rowBody)
	}
	resp, _ = h.post("/admin/queue/recipes/"+itoa(pasta.ID)+"/reject", nil)
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("reject: %d", resp.StatusCode)
	}
	if _, body := h.get("/admin/queue"); !strings.Contains(body, "Nothing to review") {
		t.Fatal("queue should be empty")
	}
	// Approved, but za'atar isn't sold (or known) in the user's country, so the
	// recipe is hidden until "show unavailable" is on; its page explains why.
	_, body = h.get("/recipes?f=1")
	if !strings.Contains(body, ">46 recipes") {
		t.Fatalf("unavailable recipe should stay hidden: %s", snippet(body, "recipes</p>"))
	}
	h.post("/settings", url.Values{"country": {"US"}, "units": {"metric"}, "show_unavailable": {"1"}})
	_, body = h.get("/recipes?f=1")
	if !strings.Contains(body, ">47 recipes") || !strings.Contains(body, "Not sold nearby") {
		t.Fatalf("approved recipe should join the pool when showing unavailable: %s", snippet(body, "recipes</p>"))
	}
	if _, body := h.get("/recipes/" + eggs.Slug); !strings.Contains(body, "isn&#39;t sold where you live") {
		t.Fatal("detail page should explain unavailability")
	}
	if _, body := h.get("/recipes?f=1&q=tomato+pasta"); strings.Contains(body, "Weeknight Tomato Pasta") {
		t.Fatal("rejected recipe visible")
	}

	// Re-scraping keeps decisions: nothing returns to the queue.
	h.post("/admin/queue/scrape", url.Values{"source": {"jsonld"}})
	if _, body := h.get("/admin/queue"); !strings.Contains(body, "Nothing to review") {
		t.Fatal("re-scrape re-queued reviewed recipes")
	}

	// Bulk actions and errors.
	for _, p := range []string{"/admin/queue/recipes/9999/approve", "/admin/queue/recipes/x/reject"} {
		if resp, _ := h.post(p, nil); resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s: %d", p, resp.StatusCode)
		}
	}
	if resp, _ := h.post("/admin/queue/approve-all", nil); !strings.Contains(resp.Header.Get("Location"), "approved_all") {
		t.Fatal("approve all")
	}
	if resp, _ := h.post("/admin/queue/purge", nil); !strings.Contains(resp.Header.Get("Location"), "purged") {
		t.Fatal("purge")
	}
	if resp, _ := h.post("/admin/queue/scrape", url.Values{"source": {"nope"}}); !strings.Contains(resp.Header.Get("Location"), "scrape_failed") {
		t.Fatal("unknown source")
	}
}

func (h *harness) mustFindByURL(u string) *recipes.Recipe {
	h.t.Helper()
	r, err := h.app.Recipes.FindDuplicate(context.Background(), u, "")
	if err != nil || r == nil {
		h.t.Fatalf("recipe %s not found: %v", u, err)
	}
	return r
}

// TestAdminMealDBBackgroundImport: the TheMealDB source runs as a background
// job from the queue page and fills the review queue.
func TestAdminMealDBBackgroundImport(t *testing.T) {
	fixture, err := os.ReadFile("../ingest/testdata/mealdb/search_a.json")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("User-agent: *\nAllow: /\n")) })
	mux.HandleFunc("/api/1/search.php", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(fixture) })
	api := httptest.NewServer(mux)
	defer api.Close()

	h := newHarness(t, func(c *config.Config) {
		c.Scrape.MealDB = config.MealDB{URL: api.URL + "/api", Letters: "a"}
	})
	if _, body := h.get("/admin/queue"); !strings.Contains(body, `value="themealdb"`) {
		t.Fatal("queue page should offer the themealdb source")
	}
	resp, _ := h.post("/admin/queue/scrape", url.Values{"source": {"themealdb"}})
	if !strings.Contains(resp.Header.Get("Location"), "scrape_started") {
		t.Fatalf("start: %s", resp.Header.Get("Location"))
	}
	h.app.WaitJobs()
	_, body := h.get("/admin/queue")
	if !strings.Contains(body, "Test Tomato Pasta") || !strings.Contains(body, "themealdb: finished") {
		t.Fatalf("queue after the job: %s", snippet(body, "job-list"))
	}
	// The new cuisine has no visible recipe yet: filters stay uncluttered.
	if _, home := h.get("/recipes"); strings.Contains(home, `value="unknownland"`) {
		t.Fatal("empty cuisines must not be offered as filters")
	}
	// Once approved, it shows with its stored name.
	rec, err := h.app.Recipes.FindDuplicate(context.Background(), "https://www.themealdb.com/meal/90002", "")
	if err != nil || rec == nil {
		t.Fatal("imported recipe missing")
	}
	if err := h.app.Recipes.SetStatus(context.Background(), rec.ID, recipes.StatusApproved, recipes.Change{Source: "test"}); err != nil {
		t.Fatal(err)
	}
	h.setCountry("US", true)
	if _, page := h.get("/recipes"); !strings.Contains(page, `<option value="unknownland" >Unknownland</option>`) {
		t.Fatalf("approved cuisine should be a filter with its name: %s", snippet(page, "unknownland"))
	}
}
