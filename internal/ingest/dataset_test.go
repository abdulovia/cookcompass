package ingest

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/abdulovia/cookcompass/internal/fetch"
	"github.com/abdulovia/cookcompass/internal/recipes"
	"github.com/abdulovia/cookcompass/internal/testutil"
)

const typedArray = `[
  {"@type": "Recipe", "name": "Full Recipe", "description": "A test",
   "recipeIngredient": ["2 eggs", "100 g flour"],
   "recipeInstructions": [{"@type": "HowToStep", "text": "Mix"}, {"@type": "HowToStep", "text": "Fry"}],
   "recipeCuisine": "american", "prepTime": "PT10M", "cookTime": "PT5M", "recipeYield": "2 servings",
   "image": "https://img.example/full.jpg", "url": "https://example.com/full"},
  {"@type": "Recipe", "name": "No Steps", "recipeIngredient": ["1 apple"]}
]`

// OpenRecipes-style items: no @type, but a name and an ingredients list.
const openRecipesJSONL = `{"name": "Borscht", "url": "https://example.com/borscht", "ingredients": ["beet", "cabbage"], "recipeInstructions": ["Simmer the beets", "Serve with sour cream"], "image": "https://img.example/borscht.jpg"}
{"name": "Gumbo", "url": "https://example.com/gumbo", "recipeIngredient": ["okra", "roux"], "recipeInstructions": ["Make the roux", "Stew"]}
not json at all
{"name": "NoIngredients", "url": "https://example.com/bad"}`

func TestExtractDataset(t *testing.T) {
	// Typed schema.org array.
	raws := ExtractDataset([]byte(typedArray))
	if len(raws) != 2 {
		t.Fatalf("typed array: %d raws", len(raws))
	}
	full := raws[0]
	if full.Name != "Full Recipe" || full.CanonicalURL != "https://example.com/full" ||
		len(full.Ingredients) != 2 || len(full.Steps) != 2 || full.PrepMinutes != 10 ||
		full.ImageURL != "https://img.example/full.jpg" {
		t.Fatalf("mapping incomplete: %+v", full)
	}
	if full.Source != "jsonld" {
		t.Fatalf("source: %q", full.Source)
	}

	// JSONL with OpenRecipes-shaped items; the broken line stops the scan
	// (records after it are lost, like in a truncated download).
	raws = ExtractDataset([]byte(openRecipesJSONL))
	if len(raws) != 2 {
		t.Fatalf("jsonl: %d raws, want 2", len(raws))
	}
	if raws[0].Name != "Borscht" || raws[0].CanonicalURL != "https://example.com/borscht" ||
		raws[0].ImageURL != "https://img.example/borscht.jpg" {
		t.Fatalf("openrecipes mapping: %+v", raws[0])
	}

	// Multi-line objects (pretty-printed records, one after another) parse
	// too — record boundaries are JSON values, not newlines.
	raws = ExtractDataset([]byte(typedArray))
	if len(raws) != 2 || raws[0].Name != "Full Recipe" {
		t.Fatalf("multi-line records: %+v", raws)
	}

	// Nested under a key.
	raws = ExtractDataset([]byte(`{"recipes": [` + typedArray + `]}`))
	if len(raws) != 2 {
		t.Fatalf("nested: %d raws", len(raws))
	}

	// Single object.
	if raws = ExtractDataset([]byte(`{"@type":"Recipe","name":"Solo","recipeIngredient":["x"],"recipeInstructions":"Step one"}`)); len(raws) != 1 || raws[0].Name != "Solo" || len(raws[0].Steps) != 1 {
		t.Fatalf("single object: %+v", raws)
	}
}

func TestDatasetImportPipeline(t *testing.T) {
	e := newEnv(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "dump.jsonl"), []byte(openRecipesJSONL), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "typed.json"), []byte(typedArray), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("ignored"), 0o600); err != nil {
		t.Fatal(err)
	}
	fc := fetch.New("CookCompassBot/test", "", 0, time.Second, 5*time.Second)
	fc.Sleep = func(context.Context, time.Duration) error { return nil }
	src := &DatasetSource{Fetcher: fc, Dir: dir, Log: testutil.Logger()}

	// Inbox scan finds both datasets (readme.txt ignored); the limit caps
	// the merged output.
	raws, err := src.Fetch(context.Background(), recipes.FetchOpts{Limit: 4})
	if err != nil || len(raws) != 4 {
		t.Fatalf("inbox fetch: %d raws, err %v", len(raws), err)
	}

	// Import: complete recipes land in the queue, incomplete ones are invalid.
	res := e.im.Import(context.Background(), raws)
	if res.Created != 3 || res.Invalid != 1 {
		t.Fatalf("import result: %s", res.String())
	}
	if len(res.PendingIDs) != 3 {
		t.Fatalf("pending ids: %v", res.PendingIDs)
	}
	pending, err := e.store.List(context.Background(), recipes.Filter{Statuses: []string{recipes.StatusPending}})
	if err != nil || len(pending) != 3 {
		t.Fatalf("pending: %d %v", len(pending), err)
	}
	var borscht *recipes.Recipe
	for _, r := range pending {
		if strings.EqualFold(r.Name, "Borscht") {
			borscht = r
		}
	}
	if borscht == nil || borscht.Source != recipes.ScrapedSource("example.com") || len(borscht.Steps) == 0 {
		t.Fatalf("borscht mapped wrong: %+v", borscht)
	}

	// Re-import is deduplicated.
	res = e.im.Import(context.Background(), raws)
	if res.Created != 0 {
		t.Fatalf("re-import created %d", res.Created)
	}
}

func TestDatasetSourceURLFetch(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "one.json")
	if err := os.WriteFile(f, []byte(typedArray), 0o600); err != nil {
		t.Fatal(err)
	}
	fc := fetch.New("CookCompassBot/test", "", 0, time.Second, 5*time.Second)
	src := &DatasetSource{Fetcher: fc, Log: testutil.Logger()}
	raws, err := src.Fetch(context.Background(), recipes.FetchOpts{URLs: []string{"file:" + f}})
	if err != nil || len(raws) != 2 {
		t.Fatalf("url fetch: %d %v", len(raws), err)
	}
	if !strings.HasPrefix(raws[0].URL, "file:") {
		t.Fatalf("url should record origin: %q", raws[0].URL)
	}
}
