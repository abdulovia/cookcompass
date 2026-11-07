package server

import (
	"context"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/abdulovia/cookcompass/internal/app"
	"github.com/abdulovia/cookcompass/internal/config"
	"github.com/abdulovia/cookcompass/internal/recipes"
	"github.com/abdulovia/cookcompass/internal/testutil"
)

type harness struct {
	t      *testing.T
	app    *app.App
	srv    *httptest.Server
	client *http.Client
}

func newHarness(t *testing.T, mutate ...func(*config.Config)) *harness {
	t.Helper()
	cfg := config.Default()
	cfg.DB.Path = filepath.Join(t.TempDir(), "test.db")
	cfg.Scrape.CacheDir = filepath.Join(t.TempDir(), "cache")
	for _, m := range mutate {
		m(&cfg)
	}
	a, err := app.Open(cfg, testutil.Logger())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	if _, err := a.Seed(context.Background()); err != nil {
		t.Fatal(err)
	}
	s, err := New(a)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return &harness{t: t, app: a, srv: ts, client: client}
}

func (h *harness) do(req *http.Request) (*http.Response, string) {
	h.t.Helper()
	resp, err := h.client.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

func (h *harness) get(path string, hdr ...string) (*http.Response, string) {
	h.t.Helper()
	req, _ := http.NewRequest(http.MethodGet, h.srv.URL+path, nil)
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	return h.do(req)
}

func (h *harness) post(path string, form url.Values, hdr ...string) (*http.Response, string) {
	h.t.Helper()
	req, _ := http.NewRequest(http.MethodPost, h.srv.URL+path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", h.srv.URL)
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	return h.do(req)
}

func TestHealthzAndStatic(t *testing.T) {
	h := newHarness(t)
	resp, body := h.get("/healthz")
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, `"status":"ok"`) {
		t.Fatalf("healthz %d %s", resp.StatusCode, body)
	}
	resp, _ = h.get("/static/css/app.css")
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Cache-Control") == "" {
		t.Fatalf("static %d", resp.StatusCode)
	}
	resp, body = h.get("/")
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, "CookCompass") {
		t.Fatalf("home %d", resp.StatusCode)
	}
	if resp.Header.Get("Content-Security-Policy") == "" || resp.Header.Get("X-Frame-Options") != "DENY" {
		t.Fatal("security headers missing")
	}
}

func TestSameOriginBlocksCrossSitePost(t *testing.T) {
	h := newHarness(t)
	resp, _ := h.post("/lang", url.Values{"lang": {"en"}}, "Origin", "https://evil.example")
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-site POST status %d", resp.StatusCode)
	}
	req, _ := http.NewRequest(http.MethodPost, h.srv.URL+"/lang", strings.NewReader("lang=en"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", "https://evil.example/page")
	if resp, _ := h.do(req); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-site referer status %d", resp.StatusCode)
	}
	// No Origin/Referer (CLI tools) is allowed.
	req, _ = http.NewRequest(http.MethodPost, h.srv.URL+"/lang", strings.NewReader("lang=en&back=//evil"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, _ = h.do(req)
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/" {
		t.Fatalf("lang redirect %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
}

func TestAdminGuard(t *testing.T) {
	h := newHarness(t, func(c *config.Config) { c.Admin.Password = "s3cret" })
	resp, _ := h.get("/admin/recipes")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no auth: %d", resp.StatusCode)
	}
	req, _ := http.NewRequest(http.MethodGet, h.srv.URL+"/admin/recipes", nil)
	req.SetBasicAuth("admin", "wrong")
	if resp, _ := h.do(req); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong password: %d", resp.StatusCode)
	}
	req.SetBasicAuth("admin", "s3cret")
	if resp, _ := h.do(req); resp.StatusCode != http.StatusOK {
		t.Fatalf("right password: %d", resp.StatusCode)
	}
	// Without a password, loopback (httptest) is allowed.
	open := newHarness(t)
	if resp, _ := open.get("/admin"); resp.StatusCode != http.StatusOK {
		t.Fatalf("loopback admin: %d", resp.StatusCode)
	}
}

var idRe = regexp.MustCompile(`/admin/recipes/(\d+)/edit`)

// TestRecipeCRUDRoundTrip drives the admin UI: create → edit → revise →
// soft-delete → restore, checking revisions and the public listing.
func TestRecipeCRUDRoundTrip(t *testing.T) {
	h := newHarness(t)
	form := url.Values{
		"name": {"Garlic Butter Toast"}, "description": {"Crunchy."}, "cuisine": {"french"},
		"meal_times": {"breakfast", "snack"}, "difficulty": {"easy"},
		"prep_minutes": {"2"}, "cook_minutes": {"4"}, "servings": {"1"},
		"ingredients": {"2 slices bread\n1 tbsp butter\n1 clove garlic, minced\nparsley, for garnish (optional)"},
		"steps":       {"Toast the bread.\nMix butter and garlic.\nSpread and serve."},
		"calories":    {"250"}, "protein": {"6"}, "fat": {"12"}, "carbs": {"28"},
		"video_url":           {"https://youtu.be/dQw4w9WgXcQ"},
		"translation_locales": {"ru"}, "t_ru_name": {"Тост с чесночным маслом"}, "t_ru_steps": {"Поджарьте хлеб."},
	}

	// Validation failure re-renders with 422 and keeps input.
	bad := url.Values{}
	for k, v := range form {
		bad[k] = v
	}
	bad.Set("name", "")
	bad.Set("servings", "many")
	resp, body := h.post("/admin/recipes", bad)
	if resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(body, "This field is required") ||
		!strings.Contains(body, "Please enter a number") || !strings.Contains(body, "Mix butter and garlic") {
		t.Fatalf("validation: %d", resp.StatusCode)
	}

	// Create.
	resp, _ = h.post("/admin/recipes", form)
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("create: %d", resp.StatusCode)
	}
	m := idRe.FindStringSubmatch(resp.Header.Get("Location"))
	if m == nil {
		t.Fatalf("redirect %q", resp.Header.Get("Location"))
	}
	id := m[1]
	resp, body = h.get("/admin/recipes/" + id + "/edit?flash=created")
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, "Garlic Butter Toast") || !strings.Contains(body, "Created.") ||
		!strings.Contains(body, "parsley, for garnish (optional)") {
		t.Fatalf("edit page: %d", resp.StatusCode)
	}
	rec, err := h.app.Recipes.GetBySlug(context.Background(), "garlic-butter-toast")
	if err != nil {
		t.Fatal(err)
	}
	if rec.Source != recipes.SourceUser || len(rec.Ingredients) != 4 || rec.Ingredients[3].Essential ||
		rec.Translations["ru"].Name != "Тост с чесночным маслом" {
		t.Fatalf("stored recipe: %+v", rec)
	}

	// Edit (revise).
	form.Set("name", "Garlic Butter Toast Deluxe")
	form.Set("steps", form.Get("steps")+"\nEat while warm.")
	form.Del("translation_locales")
	resp, _ = h.post("/admin/recipes/"+id, form)
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("update: %d", resp.StatusCode)
	}
	rec, _ = h.app.Recipes.Get(context.Background(), rec.ID)
	if rec.Name != "Garlic Butter Toast Deluxe" || rec.Slug != "garlic-butter-toast" || len(rec.Steps) != 4 {
		t.Fatalf("update not applied: %+v", rec)
	}
	if rec.Translations["ru"].Name == "" {
		t.Fatal("translations not posted must be preserved")
	}

	// Revisions + diff.
	resp, body = h.get("/admin/recipes/" + id + "/revisions")
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, "#2") || !strings.Contains(body, `class="add"`) ||
		!strings.Contains(body, "Garlic Butter Toast Deluxe") {
		t.Fatalf("revisions: %d", resp.StatusCode)
	}
	if resp, _ := h.get("/admin/recipes/" + id + "/revisions?a=1&b=2"); resp.StatusCode != http.StatusOK {
		t.Fatal("explicit compare")
	}

	// Soft delete → hidden from list, shown in trash.
	resp, _ = h.post("/admin/recipes/"+id+"/delete", url.Values{"back": {"/admin/recipes"}})
	if resp.StatusCode != http.StatusSeeOther || !strings.Contains(resp.Header.Get("Location"), "flash=deleted") {
		t.Fatalf("delete: %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}
	_, body = h.get("/admin/recipes")
	if strings.Contains(body, "Garlic Butter Toast Deluxe") {
		t.Fatal("deleted recipe still listed")
	}
	_, body = h.get("/admin/recipes?deleted=only")
	if !strings.Contains(body, "Garlic Butter Toast Deluxe") {
		t.Fatal("deleted recipe missing from trash")
	}

	// Restore.
	resp, _ = h.post("/admin/recipes/"+id+"/restore", url.Values{"back": {"https://evil.example"}})
	if resp.StatusCode != http.StatusSeeOther || !strings.HasPrefix(resp.Header.Get("Location"), "/admin/recipes") {
		t.Fatalf("restore: %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}
	revs, _ := h.app.Recipes.Revisions(context.Background(), rec.ID)
	if len(revs) != 4 || revs[0].Note != "restored" {
		t.Fatalf("revisions after lifecycle: %d", len(revs))
	}

	// Errors and edge cases.
	for _, p := range []string{"/admin/recipes/999/edit", "/admin/recipes/abc/edit", "/admin/recipes/999/revisions"} {
		if resp, _ := h.get(p); resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s: %d", p, resp.StatusCode)
		}
	}
	if resp, _ := h.post("/admin/recipes/999", form); resp.StatusCode != http.StatusNotFound {
		t.Errorf("update missing: %d", resp.StatusCode)
	}
	if resp, _ := h.post("/admin/recipes/"+id+"/restore", nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("double restore: %d", resp.StatusCode)
	}
	if resp, _ := h.post("/admin/recipes/x/delete", nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("bad id delete: %d", resp.StatusCode)
	}
	bad.Set("name", "x")
	bad.Set("servings", "2")
	bad.Set("cuisine", "atlantis")
	if resp, body := h.post("/admin/recipes/"+id, bad); resp.StatusCode != http.StatusUnprocessableEntity ||
		!strings.Contains(body, "Please pick one") {
		t.Errorf("bad cuisine on update: %d", resp.StatusCode)
	}

	// Dashboard, list filters and the new-recipe form render.
	for _, p := range []string{"/admin", "/admin/recipes/new", "/admin/recipes?status=pending&review=1&q=toast&deleted=include"} {
		if resp, _ := h.get(p); resp.StatusCode != http.StatusOK {
			t.Errorf("%s: %d", p, resp.StatusCode)
		}
	}
}

func TestHTMXRedirect(t *testing.T) {
	h := newHarness(t)
	list, _ := h.app.Recipes.List(context.Background(), recipes.Filter{Limit: 1})
	resp, _ := h.post("/admin/recipes/"+itoa(list[0].ID)+"/delete", nil, "HX-Request", "true")
	if resp.StatusCode != http.StatusNoContent || resp.Header.Get("HX-Redirect") == "" {
		t.Fatalf("htmx redirect: %d %v", resp.StatusCode, resp.Header)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func recipesChange() recipes.Change { return recipes.Change{Source: recipes.SourceUser, Note: "test"} }
