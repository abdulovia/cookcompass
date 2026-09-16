package ingest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/abdulovia/cookcompass/internal/catalog"
	"github.com/abdulovia/cookcompass/internal/fetch"
	"github.com/abdulovia/cookcompass/internal/ingredients"
	"github.com/abdulovia/cookcompass/internal/recipes"
	"github.com/abdulovia/cookcompass/internal/seed"
	"github.com/abdulovia/cookcompass/internal/shops"
	"github.com/abdulovia/cookcompass/internal/testutil"
	seedfs "github.com/abdulovia/cookcompass/seed"
)

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestExtractJSONLDGolden pins the raw extraction for every fixture page.
func TestExtractJSONLDGolden(t *testing.T) {
	out := map[string][]recipes.RawRecipe{}
	for _, f := range []string{"graph_recipe.html", "array_recipe.html", "no_recipe.html", "broken_and_valid.html"} {
		out[f] = ExtractJSONLD(readFixture(t, f), "https://fixture.test/"+f)
	}
	if len(out["no_recipe.html"]) != 0 {
		t.Fatalf("article page produced recipes: %+v", out["no_recipe.html"])
	}
	g := out["graph_recipe.html"]
	if len(g) != 1 || g[0].Name != "Za'atar Fried Eggs" || len(g[0].Steps) != 3 || g[0].Nutrition.Sodium != 500 ||
		g[0].CanonicalURL != "https://recipes.example.org/zaatar-eggs/" || g[0].PrepMinutes != 5 {
		t.Fatalf("graph recipe: %+v", g)
	}
	a := out["array_recipe.html"]
	if len(a) != 1 || len(a[0].Steps) != 3 || a[0].Steps[0] != "Boil the pasta in salted water." || a[0].TotalMinutes != 70 ||
		a[0].ImageURL != "https://kitchen.example.com/a.jpg" || a[0].Ingredients[4] != "½ tsp chili flakes (optional)" {
		t.Fatalf("array recipe: %+v", a)
	}
	testutil.Golden(t, "extracted", out)
}

func TestParseDuration(t *testing.T) {
	tests := map[string]int{"PT15M": 15, "PT1H": 60, "PT1H30M": 90, "P0DT1H10M": 70, "P1D": 1440, "PT90S": 2, "pt5m": 5,
		"": 0, "15 minutes": 0, "PT": 0}
	for in, want := range tests {
		if got := ParseDuration(in); got != want {
			t.Errorf("ParseDuration(%q) = %d want %d", in, got, want)
		}
	}
}

type env struct {
	store *recipes.Store
	im    *Importer
}

func newEnv(t *testing.T) env {
	t.Helper()
	d := testutil.DB(t)
	ings := ingredients.NewStore(d)
	store := recipes.NewStore(d, ings, 20)
	if _, err := (&seed.Seeder{FS: seedfs.FS, Ings: ings, Recipes: store, Catalog: catalog.NewStore(d, ings), Shops: shops.NewStore(d)}).Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	im, err := NewImporter(context.Background(), store, testutil.Logger())
	if err != nil {
		t.Fatal(err)
	}
	return env{store: store, im: im}
}

func TestNormalize(t *testing.T) {
	e := newEnv(t)
	raw := ExtractJSONLD(readFixture(t, "graph_recipe.html"), "https://recipes.example.org/zaatar-eggs/")[0]
	r, err := e.im.Normalize(raw)
	if err != nil {
		t.Fatal(err)
	}
	if r.Source != "scraped:recipes.example.org" || r.Status != recipes.StatusPending || r.Cuisine != "middle-eastern" ||
		r.Servings != 2 || r.Difficulty != recipes.Easy || r.VideoURL == "" || r.ImageURL == "" {
		t.Fatalf("normalized: %+v", r)
	}
	if strings.Join(r.MealTimes, ",") != "breakfast" {
		t.Fatalf("meals %v", r.MealTimes)
	}
	pasta, _ := e.im.Normalize(ExtractJSONLD(readFixture(t, "array_recipe.html"), "https://kitchen.example.com/x")[0])
	if pasta.Cuisine != "italian" || pasta.Servings != 4 || pasta.CookMinutes != 70 || pasta.VideoURL != "" ||
		strings.Join(pasta.MealTimes, ",") != "dinner" || pasta.Difficulty != recipes.Medium {
		t.Fatalf("pasta: %+v", pasta)
	}
	var flagged, optional int
	for _, in := range pasta.Ingredients {
		if in.NeedsReview {
			flagged++
		}
		if !in.Essential {
			optional++
		}
	}
	if flagged != 1 || optional != 1 {
		t.Fatalf("pasta: %d flagged, %d optional: %+v", flagged, optional, pasta.Ingredients)
	}
	for _, bad := range []recipes.RawRecipe{{}, {Name: "x"}, {Name: "x", Ingredients: []string{"a"}}} {
		if _, err := e.im.Normalize(bad); err == nil {
			t.Errorf("invalid raw accepted: %+v", bad)
		}
	}
	hard := recipes.RawRecipe{Name: "Big", URL: "file:x", Ingredients: make([]string, 15), Steps: []string{"a"}, Yield: "lots"}
	for i := range hard.Ingredients {
		hard.Ingredients[i] = "1 egg"
	}
	if r, _ := e.im.Normalize(hard); r.Difficulty != recipes.Hard || r.Servings != 2 || r.Source != "scraped:local" {
		t.Fatalf("hard/local: %+v", r)
	}
	if got := truncate(strings.Repeat("a", 600), 500); len([]rune(got)) != 500 {
		t.Fatalf("truncate %d", len([]rune(got)))
	}
}

func TestImportDedupAndUserWins(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	raws := ExtractJSONLD(readFixture(t, "graph_recipe.html"), "https://recipes.example.org/zaatar-eggs/")
	raws = append(raws, ExtractJSONLD(readFixture(t, "broken_and_valid.html"), "https://pancakes.example.net/banana")...)
	raws = append(raws, recipes.RawRecipe{Name: "No steps", URL: "https://x.test", Ingredients: []string{"1 egg"}})

	res := e.im.Import(ctx, raws)
	if res.Created != 2 || res.Invalid != 1 || res.Fetched != 3 {
		t.Fatalf("first import: %s %v", res, res.Errors)
	}
	pending, _ := e.store.List(ctx, recipes.Filter{Statuses: []string{recipes.StatusPending}})
	if len(pending) != 2 {
		t.Fatalf("pending queue: %d", len(pending))
	}
	approved, _ := e.store.List(ctx, recipes.Filter{})
	for _, r := range approved {
		if strings.Contains(r.Name, "Za'atar") {
			t.Fatal("scraped recipe visible before approval")
		}
	}

	// Same scrape again: nothing changes.
	if res := e.im.Import(ctx, raws[:2]); res.Unchanged != 2 || res.Created != 0 {
		t.Fatalf("re-import: %s", res)
	}

	// Upstream change on a different URL variant still dedups by canonical URL.
	changed := raws[0]
	changed.URL = "https://recipes.example.org/zaatar-eggs/?utm_source=rss"
	changed.Description = "Now with sumac."
	if res := e.im.Import(ctx, []recipes.RawRecipe{changed}); res.Updated != 1 {
		t.Fatalf("update: %s %v", res, res.Errors)
	}
	eggs, _ := e.store.FindDuplicate(ctx, "https://recipes.example.org/zaatar-eggs", "")
	if eggs == nil || eggs.Description != "Now with sumac." || eggs.Status != recipes.StatusPending {
		t.Fatalf("updated recipe: %+v", eggs)
	}

	// Without a canonical URL, the content key dedups (pancakes have none in JSON-LD; the page URL is used).
	// A user edit makes the recipe authoritative: later scrapes are recorded but not applied.
	eggs.Name = "Za'atar Eggs (my version)"
	eggs.Source = recipes.SourceUser
	if err := e.store.Update(ctx, eggs, recipes.Change{Source: recipes.SourceUser, Note: "edit"}); err != nil {
		t.Fatal(err)
	}
	changed.Description = "Upstream rewrite."
	if res := e.im.Import(ctx, []recipes.RawRecipe{changed}); res.Withheld != 1 {
		t.Fatalf("withheld: %s %v", res, res.Errors)
	}
	cur, _ := e.store.Get(ctx, eggs.ID)
	if cur.Name != "Za'atar Eggs (my version)" || cur.Description == "Upstream rewrite." {
		t.Fatal("scrape overwrote a user edit")
	}
	revs, _ := e.store.Revisions(ctx, eggs.ID)
	if revs[0].Applied || !strings.Contains(revs[0].Snapshot, "Upstream rewrite.") {
		t.Fatalf("withheld revision missing: %+v", revs[0])
	}

	// Rejected recipes stay rejected when re-scraped.
	pan, _ := e.store.FindDuplicate(ctx, "https://pancakes.example.net/banana", "")
	if err := e.store.SetStatus(ctx, pan.ID, recipes.StatusRejected, recipes.Change{}); err != nil {
		t.Fatal(err)
	}
	p2 := raws[1]
	p2.Steps = append(p2.Steps, "Serve with honey.")
	e.im.Import(ctx, []recipes.RawRecipe{p2})
	pan, _ = e.store.Get(ctx, pan.ID)
	if pan.Status != recipes.StatusRejected || len(pan.Steps) != 4 {
		t.Fatalf("rejected recipe after re-scrape: %s %d steps", pan.Status, len(pan.Steps))
	}
}

func TestJSONLDSourceOverHTTP(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("User-agent: *\nDisallow: /members/\n"))
	})
	for _, f := range []string{"graph_recipe.html", "array_recipe.html", "no_recipe.html"} {
		body := readFixture(t, f)
		mux.HandleFunc("/"+f, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(body) })
	}
	srv := httptest.NewServer(mux)
	defer srv.Close()

	fc := fetch.New("CookCompassBot/test", t.TempDir(), time.Hour, time.Second, 5*time.Second)
	fc.Sleep = func(context.Context, time.Duration) error { return nil }
	src := &JSONLDSource{Fetcher: fc, Log: testutil.Logger(), Targets: []string{
		srv.URL + "/graph_recipe.html", srv.URL + "/members/secret.html", srv.URL + "/no_recipe.html", srv.URL + "/array_recipe.html",
	}}
	if src.Name() != "jsonld" {
		t.Fatal(src.Name())
	}
	raws, err := src.Fetch(context.Background(), recipes.FetchOpts{})
	if err != nil || len(raws) != 2 {
		t.Fatalf("fetch: %d %v", len(raws), err)
	}
	limited, _ := src.Fetch(context.Background(), recipes.FetchOpts{Limit: 1})
	if len(limited) != 1 {
		t.Fatalf("limit: %d", len(limited))
	}
	only, _ := src.Fetch(context.Background(), recipes.FetchOpts{URLs: []string{srv.URL + "/array_recipe.html"}})
	if len(only) != 1 || only[0].Name != "Weeknight Tomato Pasta" {
		t.Fatalf("url override: %+v", only)
	}
	// Everything blocked/unreachable → error, no recipes (offline degrades gracefully upstream).
	if _, err := src.Fetch(context.Background(), recipes.FetchOpts{URLs: []string{srv.URL + "/members/x", "http://127.0.0.1:1/down"}}); err == nil {
		t.Fatal("expected error when nothing could be fetched")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := src.Fetch(ctx, recipes.FetchOpts{}); err == nil {
		t.Fatal("cancelled context")
	}
}

func TestTargetsAndBundledSamples(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "pages"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"b.html", "a.html"} {
		if err := os.WriteFile(filepath.Join(dir, "pages", f), []byte("<html></html>"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	tf := filepath.Join(dir, "targets.txt")
	content := "# comment\n\nhttps://example.com/r1\nfile:pages/*.html\nfile://" + filepath.Join(dir, "pages", "a.html") + "\n"
	if err := os.WriteFile(tf, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ReadTargets(tf)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"https://example.com/r1", "file:" + filepath.Join(dir, "pages", "a.html"), "file:" + filepath.Join(dir, "pages", "b.html"),
		"file:" + filepath.Join(dir, "pages", "a.html")}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("targets:\n%v\n%v", got, want)
	}
	if _, err := ReadTargets(filepath.Join(dir, "missing.txt")); err == nil {
		t.Fatal("missing file")
	}
	if err := os.WriteFile(tf, []byte("file:[\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadTargets(tf); err == nil {
		t.Fatal("bad glob")
	}

	// The offline sample pages (test data only) work end to end.
	fc := fetch.New("CookCompassBot/test", "", 0, time.Second, time.Second)
	srcs, err := Sources(fc, "testdata/targets.txt", "", testutil.Logger())
	if err != nil {
		t.Fatal(err)
	}
	bundled, err := ReadTargets("testdata/targets.txt")
	if err != nil {
		t.Fatal(err)
	}
	var files []string
	for _, t := range bundled {
		if strings.HasPrefix(t, "file:") {
			files = append(files, t)
		}
	}
	if len(files) == 0 {
		t.Fatal("test targets lost their offline samples")
	}
	// The shipped targets list holds real, vetted pages only: no sample
	// files that would fill a fresh install's queue with demo recipes.
	shipped, err := ReadTargets("../../seed/sources/jsonld_targets.txt")
	if err != nil || len(shipped) == 0 {
		t.Fatalf("shipped targets: %v %v", shipped, err)
	}
	for _, u := range shipped {
		if !strings.HasPrefix(u, "https://") {
			t.Errorf("shipped targets must be live https pages, got %q", u)
		}
	}
	raws, err := srcs["jsonld"].Fetch(context.Background(), recipes.FetchOpts{URLs: files})
	if err != nil || len(raws) != 3 {
		t.Fatalf("bundled samples: %d %v", len(raws), err)
	}
	e := newEnv(t)
	res := e.im.Import(context.Background(), raws)
	if res.Created != 3 {
		t.Fatalf("import samples: %s %v", res, res.Errors)
	}
	if _, err := Sources(fc, filepath.Join(dir, "nope.txt"), "", testutil.Logger()); err != nil {
		t.Fatalf("missing targets file should only warn: %v", err)
	}
}
