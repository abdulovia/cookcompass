package ingest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/abdulovia/cookcompass/internal/fetch"
	"github.com/abdulovia/cookcompass/internal/recipes"
	"github.com/abdulovia/cookcompass/internal/testutil"
)

func readMealDB(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/mealdb/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParseMealDB(t *testing.T) {
	raws, err := ParseMealDB(readMealDB(t, "search_a.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(raws) != 2 {
		t.Fatalf("nameless meals must be skipped: %d", len(raws))
	}
	pasta := raws[0]
	if pasta.Name != "Test Tomato Pasta" || pasta.CanonicalURL != "https://www.themealdb.com/meal/90001" ||
		pasta.VideoURL == "" || pasta.ImageURL == "" || pasta.Cuisines[0] != "Italian" {
		t.Fatalf("mapping: %+v", pasta)
	}
	if strings.Join(pasta.Ingredients, "|") != "200g spaghetti|2 tablespoons olive oil|2 cloves garlic|400g chopped tomatoes|1 handful basil" {
		t.Fatalf("ingredients: %q", pasta.Ingredients)
	}
	if len(pasta.Steps) != 3 || strings.HasPrefix(pasta.Steps[0], "STEP") || !strings.HasPrefix(pasta.Steps[0], "Boil") {
		t.Fatalf("steps: %q", pasta.Steps)
	}
	// One long paragraph is cut into short steps; a blank measure leaves the
	// bare ingredient name.
	pancakes := raws[1]
	if len(pancakes.Steps) < 3 || pancakes.Ingredients[3] != "honey" {
		t.Fatalf("pancakes: %d steps, %q", len(pancakes.Steps), pancakes.Ingredients)
	}
	if empty, err := ParseMealDB(readMealDB(t, "search_q.json")); err != nil || len(empty) != 0 {
		t.Fatalf(`{"meals": null}: %v %v`, empty, err)
	}
	if _, err := ParseMealDB([]byte("<html>")); err == nil {
		t.Fatal("garbage should fail")
	}
}

// TestMealDBSourceImport fetches from a local stand-in of the API and runs
// the importer: recipes queue as pending with mapped cuisine and meals.
func TestMealDBSourceImport(t *testing.T) {
	var hits atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("User-agent: *\nAllow: /\n")) })
	mux.HandleFunc("/api/json/v1/test-key/search.php", func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Query().Get("f") == "a" {
			_, _ = w.Write(readMealDB(t, "search_a.json"))
			return
		}
		_, _ = w.Write(readMealDB(t, "search_q.json"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	fc := fetch.New("CookCompassBot/test", "", 0, time.Millisecond, 5*time.Second)
	src := &MealDBSource{Fetcher: fc, BaseURL: srv.URL + "/api/json/v1", Key: "test-key", Letters: "ab", Log: testutil.Logger()}
	if !src.LongRunning() || src.Name() != "themealdb" {
		t.Fatal("source identity")
	}
	raws, err := src.Fetch(context.Background(), recipes.FetchOpts{})
	if err != nil || len(raws) != 2 || hits.Load() != 2 {
		t.Fatalf("fetch: %d recipes, %d requests, %v", len(raws), hits.Load(), err)
	}
	if one, _ := src.Fetch(context.Background(), recipes.FetchOpts{Limit: 1}); len(one) != 1 {
		t.Fatalf("limit: %d", len(one))
	}

	e := newEnv(t)
	res := e.im.Import(context.Background(), raws)
	if res.Created != 2 {
		t.Fatalf("import: %s %v", res, res.Errors)
	}
	r, err := e.store.FindDuplicate(context.Background(), "https://www.themealdb.com/meal/90001", "")
	if err != nil || r == nil {
		t.Fatalf("dedup by canonical URL: %v", err)
	}
	if r.Status != recipes.StatusPending || r.Cuisine != "italian" || r.Source != recipes.ScrapedSource("themealdb.com") ||
		!r.HasMealTime(recipes.Dinner) {
		t.Fatalf("queued recipe: status=%s cuisine=%s source=%s meals=%v", r.Status, r.Cuisine, r.Source, r.MealTimes)
	}
	if again := e.im.Import(context.Background(), raws); again.Created != 0 {
		t.Fatalf("re-import must dedupe: %s", again)
	}

	// Unreachable API: an error, nothing imported.
	down := &MealDBSource{Fetcher: fc, BaseURL: "http://127.0.0.1:1/api", Letters: "a"}
	if got, err := down.Fetch(context.Background(), recipes.FetchOpts{}); err == nil || len(got) != 0 {
		t.Fatalf("down: %d %v", len(got), err)
	}
}
