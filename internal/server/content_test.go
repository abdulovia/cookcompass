package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/abdulovia/cookcompass/internal/config"
)

// contentDir writes an operator content directory for a test.
func contentDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// TestContentDirectory: one directory adds a country with a shop and a
// product, a recipe and a UI text override — no rebuild, no locale edits.
func TestContentDirectory(t *testing.T) {
	recipe, err := os.ReadFile("../../docs/examples/recipe.json")
	if err != nil {
		t.Fatal(err)
	}
	dir := contentDir(t, map[string]string{
		"cuisines.json": `[{"slug":"greek","name":"Hellenic","names":{"ru":"Эллинская"}}]`,
		"countries.json": `[{"code":"FR","name":"France","names":{"ru":"Франция"},"currency":"EUR","lat":46.6,"lng":2.2,"default_locale":"fr",
			"cities":[{"name":"Paris","lat":48.8566,"lng":2.3522}]}]`,
		"shops.csv":             "country,slug,name,website,search_url_template\nFR,monoprix,Monoprix,https://www.monoprix.fr,https://www.monoprix.fr/search?q={q}\n",
		"products/fr.csv":       "sku,name,category,ingredient,price,unit\nFR-BREAD-1,Baguette,bakery,bread,,1 pc\n",
		"recipes/my-toast.json": string(recipe),
		"locales/en.json":       `{"footer.local": "Served by our family kitchen."}`,
		"static/custom.css":     `:root{--brand:#c0392b}`,
	})
	h := newHarness(t, func(c *config.Config) { c.Content.Dir = dir })

	_, body := h.get("/settings")
	if !strings.Contains(body, `value="FR"`) {
		t.Fatal("content country missing from settings")
	}
	if !strings.Contains(body, "Served by our family kitchen.") || strings.Contains(body, "Runs entirely on your computer") {
		t.Fatal("locale override not applied")
	}
	// Untouched keys still come from the bundled file.
	if !strings.Contains(body, "Shopping list") {
		t.Fatal("bundled strings lost after a partial override")
	}
	if resp, body := h.get("/recipes/my-toast"); resp.StatusCode != 200 || !strings.Contains(body, "Buttered Toast with Honey") {
		t.Fatalf("content recipe: %d", resp.StatusCode)
	}
	if resp, body := h.get("/static/custom.css"); resp.StatusCode != 200 || !strings.Contains(body, "#c0392b") {
		t.Fatalf("content static asset: %d", resp.StatusCode)
	}
	if resp, _ := h.get("/static/css/app.css"); resp.StatusCode != 200 {
		t.Fatal("bundled static asset lost")
	}
	// Names come with the data, per language, with English as fallback.
	if _, body := h.get("/recipes"); !strings.Contains(body, ">Hellenic<") {
		t.Fatal("content cuisine name not used")
	}
	if _, body := h.get("/settings", "Cookie", "lang=ru"); !strings.Contains(body, ">Франция<") || !strings.Contains(body, ">США<") {
		t.Fatal("country names should be localized from data")
	}
	if _, body := h.get("/recipes", "Cookie", "lang=ru"); !strings.Contains(body, ">Эллинская<") || !strings.Contains(body, ">Тайская<") {
		t.Fatal("cuisine names should be localized from data")
	}
	h.setCountry("FR", false)
	if _, body := h.get("/settings"); !strings.Contains(body, "Monoprix") {
		t.Fatal("content shop missing for its country")
	}
}

// TestSiteConfig: branding and feature switches come from site.yaml in the
// content directory — routes, navigation and buttons follow.
func TestSiteConfig(t *testing.T) {
	dir := contentDir(t, map[string]string{"site.yaml": `
name: {en: Family Kitchen, ru: Семейная кухня}
accent: "#c0392b"
footer_links: [{label: {en: About us, ru: О нас}, url: "https://example.org/about"}]
features: {shopping_list: false, learning: false}
`})
	h := newHarness(t, func(c *config.Config) { c.Content.Dir = dir })

	_, body := h.get("/")
	for _, want := range []string{"<title>Family Kitchen</title>", "Family Kitchen</a>", "--brand: #c0392b",
		`href="https://example.org/about">About us<`, `href="/goals"`} {
		if !strings.Contains(body, want) {
			t.Errorf("home lacks %q", want)
		}
	}
	for _, gone := range []string{`href="/list"`, `href="/learn"`, `id="progress"`} {
		if strings.Contains(body, gone) {
			t.Errorf("disabled feature still shows %q", gone)
		}
	}
	if _, body := h.get("/", "Cookie", "lang=ru"); !strings.Contains(body, "Семейная кухня") || !strings.Contains(body, ">О нас<") {
		t.Error("site texts should follow the page language")
	}
	for _, p := range []string{"/list", "/learn"} {
		if resp, _ := h.get(p); resp.StatusCode != 404 {
			t.Errorf("%s: %d, want 404 for a disabled feature", p, resp.StatusCode)
		}
	}
	if resp, _ := h.post("/recipes/pad-thai/shopping", nil); resp.StatusCode != 404 {
		t.Errorf("shopping POST: %d", resp.StatusCode)
	}
	_, body = h.get("/recipes/pad-thai")
	if strings.Contains(body, "/shopping") || strings.Contains(body, "/learned") || !strings.Contains(body, "/favorite") {
		t.Error("recipe actions should follow the feature switches")
	}
}

// TestOptionsHelper: option lists come from one backend table, with
// labels, descriptions and the current selection.
func TestOptionsHelper(t *testing.T) {
	h := newHarness(t)
	_, body := h.get("/recipes?f=1&meal=dinner&difficulty=hard")
	if !strings.Contains(body, `<option value="dinner" selected>Dinner</option>`) ||
		!strings.Contains(body, `<option value="hard" selected>Hard</option>`) {
		t.Fatal("browse options should mark the current filter")
	}
	_, body = h.get("/goals")
	if !strings.Contains(body, `name="goal" value="lose_weight"`) || !strings.Contains(body, " — <span class=\"muted\">") {
		t.Fatal("goal options should carry their descriptions")
	}
}

// TestAdminChecklist: the dashboard shows the same setup checks as
// `cookcompass doctor`, with fixes for what is missing.
func TestAdminChecklist(t *testing.T) {
	h := newHarness(t)
	_, body := h.get("/admin")
	for _, want := range []string{`id="checklist"`, "Getting started", "Recipes published", "Everything required is in place.",
		"Your own recipes", "docs/recipes.md", "Image storage"} {
		if !strings.Contains(body, want) {
			t.Errorf("dashboard lacks %q", want)
		}
	}
	// An optional item that is open shows its fix.
	if !strings.Contains(body, "TheMealDB in the review queue") {
		t.Error("open optional item should show its hint")
	}
}
