package seed

import (
	"context"
	"encoding/json"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/abdulovia/cookcompass/internal/catalog"
	"github.com/abdulovia/cookcompass/internal/ingredients"
	"github.com/abdulovia/cookcompass/internal/recipes"
	"github.com/abdulovia/cookcompass/internal/shops"
	"github.com/abdulovia/cookcompass/internal/testutil"
	seedfs "github.com/abdulovia/cookcompass/seed"
)

func newSeeder(t *testing.T, fsys fs.FS) *Seeder {
	t.Helper()
	d := testutil.DB(t)
	ings := ingredients.NewStore(d)
	return &Seeder{FS: fsys, Ings: ings, Recipes: recipes.NewStore(d, ings, 20), Catalog: catalog.NewStore(d, ings), Shops: shops.NewStore(d), Log: testutil.Logger()}
}

func TestSeedBundledDataset(t *testing.T) {
	ctx := context.Background()
	s := newSeeder(t, seedfs.FS)
	rep, err := s.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Ingredients < 100 || rep.Cuisines < 12 || rep.Countries < 3 || rep.Shops < 10 || rep.RecipesAdded < 5 {
		t.Fatalf("dataset too small: %s", rep)
	}
	counts, err := s.Catalog.ProductCounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for cc, n := range counts {
		if n < 150 {
			t.Errorf("country %s has only %d products", cc, n)
		}
	}
	// Every preset shop has a usable search template.
	shopList, err := s.Shops.List(ctx, "", true)
	if err != nil {
		t.Fatal(err)
	}
	for _, sh := range shopList {
		if sh.SearchURLTemplate == "" || !strings.Contains(sh.SearchURLTemplate, "{q}") {
			t.Errorf("shop %s/%s: template %q", sh.CountryCode, sh.Slug, sh.SearchURLTemplate)
		}
	}

	// Second run adds nothing.
	rep2, err := s.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rep2.RecipesAdded != 0 || rep2.ProductsAdded != 0 || rep2.RecipesExisting != rep.RecipesAdded {
		t.Fatalf("seeding is not idempotent: %s", rep2)
	}

	list, err := s.Recipes.List(ctx, recipes.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range list {
		if r.Source != recipes.SourceSeed {
			t.Errorf("%s: source %q", r.Slug, r.Source)
		}
		ru, ok := r.Translations["ru"]
		if !ok || ru.Name == "" || len(ru.Steps) == 0 {
			t.Errorf("%s: missing Russian translation", r.Slug)
		}
		if len(ru.Ingredients) != len(r.Ingredients) {
			t.Errorf("%s: %d ru ingredient lines for %d ingredients", r.Slug, len(ru.Ingredients), len(r.Ingredients))
		}
		if len(ru.Steps) != len(r.Steps) {
			t.Errorf("%s: %d ru steps for %d steps", r.Slug, len(ru.Steps), len(r.Steps))
		}
		for _, in := range r.Ingredients {
			if in.IngredientID == 0 {
				t.Errorf("%s: ingredient %q not linked", r.Slug, in.Raw)
			}
			if in.NeedsReview {
				t.Errorf("%s: ingredient %q needs review", r.Slug, in.Raw)
			}
		}
		if r.Nutrition.Calories <= 0 || r.Nutrition.Protein <= 0 {
			t.Errorf("%s: missing nutrition", r.Slug)
		}
		if !recipes.IsYouTubeURL(r.VideoURL) {
			t.Errorf("%s: video url %q", r.Slug, r.VideoURL)
		}
	}
}

func TestSeedProductsDoNotOverwriteUserEdits(t *testing.T) {
	ctx := context.Background()
	s := newSeeder(t, seedfs.FS)
	if _, err := s.Run(ctx); err != nil {
		t.Fatal(err)
	}
	p, err := s.Catalog.FindProduct(ctx, "US", catalog.ProductKey("", "US-MILK-1", ""))
	if err != nil {
		t.Fatal(err)
	}
	p.Price = 9.99
	p.Source = "user"
	if err := s.Catalog.UpdateProduct(ctx, p); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Run(ctx); err != nil {
		t.Fatal(err)
	}
	again, _ := s.Catalog.GetProduct(ctx, p.ID)
	if again.Price != 9.99 {
		t.Fatalf("seed overwrote user price: %v", again.Price)
	}
}

func TestSeedErrors(t *testing.T) {
	ctx := context.Background()
	good := func() fstest.MapFS {
		return fstest.MapFS{
			"ingredients.json": {Data: []byte(`[{"slug":"egg","name":"egg"}]`)},
			"cuisines.json":    {Data: []byte(`[{"slug":"x","name":"X"}]`)},
			"countries.json":   {Data: []byte(`[{"code":"US","name":"US","currency":"USD","cities":[{"name":"A","lat":1,"lng":2}]}]`)},
		}
	}
	recipe := func(ing string) []byte {
		b, _ := json.Marshal(recipes.Input{Cuisine: "x", MealTimes: []string{"lunch"}, Difficulty: "easy", Servings: 1,
			Ingredients: []recipes.InputIngredient{{Text: "1 egg", Ingredient: ing}},
			I18n:        map[string]recipes.InputText{"en": {Name: "Egg", Steps: []string{"Boil."}}}})
		return b
	}
	tests := map[string]func(fstest.MapFS){
		"missing ingredients": func(m fstest.MapFS) { delete(m, "ingredients.json") },
		"bad json":            func(m fstest.MapFS) { m["cuisines.json"] = &fstest.MapFile{Data: []byte(`{`)} },
		"bad substitute": func(m fstest.MapFS) {
			m["ingredients.json"] = &fstest.MapFile{Data: []byte(`[{"slug":"egg","name":"egg","substitutes":[{"slug":"nope"}]}]`)}
		},
		"bad country": func(m fstest.MapFS) {
			m["countries.json"] = &fstest.MapFile{Data: []byte(`[{"code":"USA","name":""}]`)}
		},
		"bad product csv": func(m fstest.MapFS) { m["products/us.csv"] = &fstest.MapFile{Data: []byte("sku\nx\n")} },
		"unknown country product": func(m fstest.MapFS) {
			m["products/zz.csv"] = &fstest.MapFile{Data: []byte("sku,name\nA,Apple\n")}
		},
		"unknown ingredient": func(m fstest.MapFS) { m["recipes/egg.json"] = &fstest.MapFile{Data: recipe("nope")} },
		"invalid recipe": func(m fstest.MapFS) {
			m["recipes/egg.json"] = &fstest.MapFile{Data: []byte(`{"cuisine":"x","difficulty":"easy"}`)}
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			m := good()
			mutate(m)
			if _, err := newSeeder(t, m).Run(ctx); err == nil {
				t.Fatal("expected error")
			}
		})
	}
	m := good()
	m["recipes/egg.json"] = &fstest.MapFile{Data: recipe("egg")}
	rep, err := newSeeder(t, m).Run(ctx)
	if err != nil || rep.RecipesAdded != 1 {
		t.Fatalf("minimal dataset: %v %v", rep, err)
	}
}
