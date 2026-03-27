package catalogscan

import (
	"context"
	"errors"
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
	"github.com/abdulovia/cookcompass/internal/testutil"
	seedfs "github.com/abdulovia/cookcompass/seed"
)

func TestExtractProducts(t *testing.T) {
	body, err := os.ReadFile("testdata/shop_ru.html")
	if err != nil {
		t.Fatal(err)
	}
	got := ExtractProducts(body, "https://www.shop.example/asian")
	if len(got) != 3 {
		t.Fatalf("products: %+v", got)
	}
	gcp := got[0]
	if gcp.Name != "Паста карри зелёная, 110 g" || gcp.SKU != "SHOP-GCP-110" || gcp.Price != 249.9 || gcp.Currency != "RUB" ||
		gcp.Category != "соусы" || gcp.Unit != "110 g" || gcp.Source != "scraped:shop.example" {
		t.Fatalf("first product: %+v", gcp)
	}
	if got[1].SKU != "4600000000017" || got[1].Price != 189 || got[1].Unit != "200 ml" {
		t.Fatalf("second product: %+v", got[1])
	}
	if got[2].Price != 10 {
		t.Fatalf("lowPrice: %+v", got[2])
	}
	if len(ExtractProducts([]byte("<html></html>"), "")) != 0 {
		t.Fatal("empty page")
	}
	if lastSegment("A / B / Fresh") != "fresh" || str(nil) != "" || num(nil) != 0 || firstMap(1) != nil || toStrings(1) != nil {
		t.Fatal("helpers")
	}
}

func newStore(t *testing.T) *catalog.Store {
	t.Helper()
	d := testutil.DB(t)
	ings := ingredients.NewStore(d)
	cat := catalog.NewStore(d, ings)
	if _, err := (&seed.Seeder{FS: seedfs.FS, Ings: ings, Recipes: recipes.NewStore(d, ings, 20), Catalog: cat}).Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	return cat
}

type stubSource struct {
	name string
	list []catalog.Product
	err  error
}

func (s stubSource) Name() string { return s.name }
func (s stubSource) Fetch(context.Context, string) ([]catalog.Product, error) {
	return s.list, s.err
}

func TestScannerQueueAndUserWins(t *testing.T) {
	ctx := context.Background()
	cat := newStore(t)
	before, _ := cat.IngredientSet(ctx, "RU")
	gcp, _ := cat.ListProducts(ctx, ProductFilterFor("green curry"))
	if len(gcp) != 0 {
		t.Fatal("seed RU should not sell green curry paste")
	}
	body, _ := os.ReadFile("testdata/shop_ru.html")
	found := ExtractProducts(body, "https://shop.example/asian")
	s := &Scanner{Catalog: cat, Sources: []CatalogSource{stubSource{"jsonld", found, nil}}, Seed: &SeedSource{FS: seedfs.FS}, Log: testutil.Logger()}
	res := s.Scan(ctx, "ru")
	if res.Created != 3 || res.Fallback || res.Country != "RU" || !strings.Contains(res.String(), "created=3") {
		t.Fatalf("scan: %s %v", res, res.Errors)
	}
	// Pending products do not change availability yet.
	after, _ := cat.IngredientSet(ctx, "RU")
	if len(after) != len(before) {
		t.Fatal("pending products must not count as available")
	}
	pending, _ := cat.ListProducts(ctx, catalog.ProductFilter{Country: "RU", Statuses: []string{catalog.StatusPending}})
	if len(pending) != 3 || pending[0].IngredientSlug == "" && pending[1].IngredientSlug == "" {
		t.Fatalf("pending: %+v", pending)
	}
	// Re-scan updates in place.
	if res := s.Scan(ctx, "RU"); res.Updated != 3 || res.Created != 0 {
		t.Fatalf("re-scan: %s", res)
	}
	// A user edit is never overwritten.
	p := pending[0]
	p.Source, p.Price = "user", 1
	if err := cat.UpdateProduct(ctx, p); err != nil {
		t.Fatal(err)
	}
	if res := s.Scan(ctx, "RU"); res.Skipped != 1 {
		t.Fatalf("user-edited product not skipped: %s", res)
	}
}

// ProductFilterFor is a tiny helper for tests.
func ProductFilterFor(q string) catalog.ProductFilter {
	return catalog.ProductFilter{Country: "RU", Query: q, Statuses: []string{catalog.StatusApproved, catalog.StatusPending}}
}

func TestScannerFallsBackToSeed(t *testing.T) {
	ctx := context.Background()
	cat := newStore(t)
	// Remove a seed product; an offline scan restores missing seed items only.
	milk, _ := cat.FindProduct(ctx, "US", catalog.ProductKey("US-MILK-1", ""))
	if _, err := cat.PurgePending(ctx); err != nil {
		t.Fatal(err)
	}
	if err := cat.SetProductStatus(ctx, milk.ID, catalog.StatusPending); err != nil {
		t.Fatal(err)
	}
	if _, err := cat.PurgePending(ctx); err != nil {
		t.Fatal(err)
	}
	s := &Scanner{Catalog: cat, Sources: []CatalogSource{stubSource{"jsonld", nil, errors.New("offline")}}, Seed: &SeedSource{FS: seedfs.FS}}
	res := s.Scan(ctx, "US")
	if !res.Fallback || res.Created != 1 || len(res.Errors) != 1 {
		t.Fatalf("fallback: %s %v", res, res.Errors)
	}
	restored, err := cat.FindProduct(ctx, "US", catalog.ProductKey("US-MILK-1", ""))
	if err != nil || restored.Status != catalog.StatusApproved {
		t.Fatalf("seed product not restored as approved: %+v %v", restored, err)
	}
	if res := (&Scanner{Catalog: cat, Seed: &SeedSource{FS: seedfs.FS}}).Scan(ctx, "ZZ"); len(res.Errors) == 0 {
		t.Fatal("unknown country should report an error")
	}
	if got := Countries(seedfs.FS); strings.Join(got, ",") != "DE,GB,RU,US" {
		t.Fatalf("countries %v", got)
	}
}

func TestFileSource(t *testing.T) {
	dir := t.TempDir()
	ru := filepath.Join(dir, "RU")
	if err := os.MkdirAll(filepath.Join(ru, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	example, _ := os.ReadFile("../../docs/examples/catalog.json")
	files := map[string]string{
		"corner.json": string(example),
		"market.csv":  "name,price,ingredient\nТахини,350,tahini\n",
		"notes.txt":   "ignored? no — unsupported",
	}
	for n, c := range files {
		if err := os.WriteFile(filepath.Join(ru, n), []byte(c), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	src := &FileSource{Dir: dir}
	list, err := src.Fetch(context.Background(), "ru")
	if len(list) != 4 || err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("file source: %d %v", len(list), err)
	}
	if list[0].Source != "file:corner-shop" || list[0].Currency != "RUB" || list[3].Source != "file:market.csv" {
		t.Fatalf("sources: %+v", list)
	}
	if l, err := src.Fetch(context.Background(), "DE"); l != nil || err != nil {
		t.Fatal("missing country dir is not an error")
	}
	if src.Name() != "file" || (&SeedSource{}).Name() != "seed" || (&JSONLDSource{}).Name() != "jsonld" {
		t.Fatal("names")
	}
	bad := map[string]string{
		"wrong.json":   `{"country":"US","products":[]}`,
		"broken.json":  `{`,
		"noname.json":  `{"products":[{"price":1}]}`,
		"broken.csv":   "sku\nx\n",
		"missing.json": "",
	}
	for n, c := range bad {
		p := filepath.Join(dir, n)
		if n != "missing.json" {
			_ = os.WriteFile(p, []byte(c), 0o600)
		}
		if _, err := ReadCatalogFile(p, "RU"); err == nil {
			t.Errorf("%s: expected error", n)
		}
	}
}

func TestJSONLDSourceAndTargets(t *testing.T) {
	body, _ := os.ReadFile("testdata/shop_ru.html")
	mux := http.NewServeMux()
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("User-agent: *\nDisallow: /private\n"))
	})
	mux.HandleFunc("/asian", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(body) })
	srv := httptest.NewServer(mux)
	defer srv.Close()

	dir := t.TempDir()
	tf := filepath.Join(dir, "targets.txt")
	content := "# shops\nru " + srv.URL + "/asian\nRU " + srv.URL + "/private/x\nUS file:local.html\n"
	if err := os.WriteFile(tf, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	targets, err := ReadTargets(tf)
	if err != nil || len(targets["RU"]) != 2 || targets["US"][0] != "file:"+filepath.Join(dir, "local.html") {
		t.Fatalf("targets %v %v", targets, err)
	}
	fc := fetch.New("CookCompassBot/test", "", 0, time.Second, 5*time.Second)
	fc.Sleep = func(context.Context, time.Duration) error { return nil }
	src := &JSONLDSource{Fetcher: fc, Targets: targets, Log: testutil.Logger()}
	list, err := src.Fetch(context.Background(), "RU")
	if err != nil || len(list) != 3 || list[0].CountryCode != "RU" {
		t.Fatalf("fetch: %d %v", len(list), err)
	}
	if _, err := src.Fetch(context.Background(), "US"); err == nil {
		t.Fatal("missing local file should error when nothing was read")
	}
	if l, err := src.Fetch(context.Background(), "DE"); l != nil || err != nil {
		t.Fatal("no targets → nothing")
	}
	for _, bad := range []string{"RU\n", "RUS http://x\n", "XX\n"} {
		_ = os.WriteFile(tf, []byte(bad), 0o600)
		if _, err := ReadTargets(tf); err == nil {
			t.Errorf("%q: expected error", bad)
		}
	}
	if _, err := ReadTargets(filepath.Join(dir, "nope")); err == nil {
		t.Fatal("missing targets file")
	}
	if _, err := (&SeedSource{FS: seedfs.FS}).Fetch(context.Background(), "ZZ"); err == nil {
		t.Fatal("seed for unknown country")
	}
}
