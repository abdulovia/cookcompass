package server

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/abdulovia/cookcompass/internal/config"
	"github.com/abdulovia/cookcompass/internal/recipes"
)

const datasetDrop = `{"@type": "Recipe", "name": "Inbox Soup", "description": "From the inbox",
 "recipeIngredient": ["1 onion", "2 carrots", "1 l vegetable broth"],
 "recipeInstructions": [{"@type": "HowToStep", "text": "Chop"}, {"@type": "HowToStep", "text": "Simmer 20 minutes"}],
 "recipeCuisine": "american", "recipeYield": "4 servings", "url": "https://example.com/inbox-soup"}
{"@type": "Recipe", "name": "Broken Salad", "recipeIngredient": ["lettuce"]}`

// TestAdminDatasetImport: dropping a dataset into the recipe inbox, the
// admin queue page lists it; importing queues complete recipes only; the
// approve option publishes them straight away.
func TestAdminDatasetImport(t *testing.T) {
	inbox := t.TempDir()
	if err := os.WriteFile(filepath.Join(inbox, "drop.jsonl"), []byte(datasetDrop), 0o600); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, func(c *config.Config) { c.Scrape.DatasetDir = inbox })

	// The queue page lists the drop and offers the import form.
	if _, body := h.get("/admin/queue"); !strings.Contains(body, `name="file"`) || !strings.Contains(body, "drop.jsonl") {
		t.Fatal("import form should list inbox files")
	}

	// Import without approve: the complete recipe queues, the step-less one
	// is rejected by the completeness gate.
	form := url.Values{"file": {"drop.jsonl"}, "limit": {"10"}}
	if resp, _ := h.post("/admin/queue/import", form); !strings.Contains(resp.Header.Get("Location"), "recipes_imported") {
		t.Fatalf("import: %s", resp.Header.Get("Location"))
	}
	pending, err := h.app.Recipes.List(context.Background(), recipes.Filter{Statuses: []string{recipes.StatusPending}})
	if err != nil || len(pending) != 1 || pending[0].Name != "Inbox Soup" {
		t.Fatalf("pending after import: %d %v", len(pending), err)
	}
	if pending[0].ImageURL != "" || pending[0].Source != recipes.ScrapedSource("example.com") {
		t.Fatalf("mapped: %+v", pending[0])
	}
	// Not visible to the public yet.
	if _, body := h.get("/recipes"); strings.Contains(body, "Inbox Soup") {
		t.Fatal("pending recipe leaked to the public list")
	}

	// Import again with approve: duplicate is skipped, nothing new approved.
	form.Set("approve", "1")
	if resp, _ := h.post("/admin/queue/import", form); !strings.Contains(resp.Header.Get("Location"), "recipes_imported") {
		t.Fatalf("approve import: %s", resp.Header.Get("Location"))
	}
	if _, err := h.app.Recipes.GetBySlug(context.Background(), "inbox-soup"); err != nil {
		t.Fatalf("approved recipe should exist: %v", err)
	}
	if _, body := h.get("/recipes/inbox-soup"); !strings.Contains(body, "Inbox Soup") {
		t.Fatal("approved recipe should be public")
	}
}

// TestAdminDatasetImportFromURL: the import form also accepts a URL.
func TestAdminDatasetImportFromURL(t *testing.T) {
	h := newHarness(t)
	form := url.Values{"url": {h.srv.URL + "/nonexistent-dataset.json"}}
	resp, _ := h.post("/admin/queue/import", form)
	if !strings.Contains(resp.Header.Get("Location"), "import_failed") {
		t.Fatalf("bad url should fail: %s", resp.Header.Get("Location"))
	}
	// А file: URL from the form is rejected: only http(s) and inbox files
	// are allowed inputs.
	form = url.Values{"url": {"file:///etc/passwd"}}
	if resp, _ := h.post("/admin/queue/import", form); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("file url: %d", resp.StatusCode)
	}
}
