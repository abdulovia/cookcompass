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
