// Package seed loads the bundled dataset into the database. Loading is
// idempotent: existing recipes and products are never overwritten, so
// running it on every start only adds what is new.
package seed

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"path"
	"sort"
	"strings"

	"github.com/abdulovia/cookcompass/internal/catalog"
	"github.com/abdulovia/cookcompass/internal/ingredients"
	"github.com/abdulovia/cookcompass/internal/recipes"
	"github.com/abdulovia/cookcompass/internal/shops"
)

// Report summarizes a seeding run.
type Report struct {
	Ingredients     int
	Cuisines        int
	Countries       int
	Shops           int
	ProductsAdded   int
	RecipesAdded    int
	RecipesExisting int
}

func (r Report) String() string {
	return fmt.Sprintf("ingredients=%d cuisines=%d countries=%d shops=%d products_added=%d recipes_added=%d recipes_existing=%d",
		r.Ingredients, r.Cuisines, r.Countries, r.Shops, r.ProductsAdded, r.RecipesAdded, r.RecipesExisting)
}

// Seeder loads seed files from an fs.FS.
type Seeder struct {
	FS      fs.FS
	Ings    *ingredients.Store
	Recipes *recipes.Store
	Catalog *catalog.Store
	Shops   *shops.Store
	Log     *slog.Logger
}

type ingredientFile struct {
	Slug        string            `json:"slug"`
	Name        string            `json:"name"`
	Category    string            `json:"category"`
	Staple      bool              `json:"staple"`
	Names       map[string]string `json:"names"`
	Aliases     []string          `json:"aliases"`
	Substitutes []struct {
		Slug  string `json:"slug"`
		Ratio string `json:"ratio"`
	} `json:"substitutes"`
}

type countryFile struct {
	catalog.Country
	Cities []catalog.City `json:"cities"`
}

// Run loads everything.
func (s *Seeder) Run(ctx context.Context) (Report, error) {
	var rep Report
	steps := []func(context.Context, *Report) error{s.ingredients, s.cuisines, s.countries, s.shops, s.products, s.recipes}
	for _, step := range steps {
		if err := step(ctx, &rep); err != nil {
			return rep, err
		}
	}
	return rep, nil
}

func (s *Seeder) readJSON(name string, v any) error {
	b, err := fs.ReadFile(s.FS, name)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

func (s *Seeder) ingredients(ctx context.Context, rep *Report) error {
	var list []ingredientFile
	if err := s.readJSON("ingredients.json", &list); err != nil {
		return err
	}
	for _, in := range list {
		if _, err := s.Ings.Upsert(ctx, ingredients.Ingredient{Slug: in.Slug, Name: in.Name, Category: in.Category,
			Staple: in.Staple, Names: in.Names, Aliases: in.Aliases}); err != nil {
			return err
		}
	}
	for _, in := range list {
		for _, sub := range in.Substitutes {
			if err := s.Ings.SetSubstitute(ctx, in.Slug, sub.Slug, sub.Ratio); err != nil {
				return err
			}
		}
	}
	rep.Ingredients = len(list)
	return nil
}

func (s *Seeder) cuisines(ctx context.Context, rep *Report) error {
	var list []recipes.Cuisine
	if err := s.readJSON("cuisines.json", &list); err != nil {
		return err
	}
	for _, c := range list {
		if _, err := s.Recipes.UpsertCuisine(ctx, c.Slug, c.Name); err != nil {
			return err
		}
	}
	rep.Cuisines = len(list)
	return nil
}

func (s *Seeder) countries(ctx context.Context, rep *Report) error {
	var list []countryFile
	if err := s.readJSON("countries.json", &list); err != nil {
		return err
	}
	for _, c := range list {
		if err := s.Catalog.UpsertCountry(ctx, c.Country); err != nil {
			return err
		}
		for _, city := range c.Cities {
			city.CountryCode = c.Code
			if _, err := s.Catalog.UpsertCity(ctx, city); err != nil {
				return err
			}
		}
	}
	rep.Countries = len(list)
	return nil
}

// shops loads the bundled shop presets (search URL templates). Upserts keep
// admin edits to name/website intact only when the seed row itself changes;
// the shop list is small and stable by design.
func (s *Seeder) shops(ctx context.Context, rep *Report) error {
	f, err := s.FS.Open("shops.csv")
	if errors.Is(err, fs.ErrNotExist) {
		return nil // test/minimal datasets may omit shop presets
	}
	if err != nil {
		return err
	}
	defer f.Close()
	cr := csv.NewReader(f)
	cr.TrimLeadingSpace = true
	header, err := cr.Read()
	if err != nil {
		return fmt.Errorf("shops.csv: %w", err)
	}
	col := map[string]int{}
	for i, h := range header {
		col[strings.ToLower(strings.TrimSpace(h))] = i
	}
	get := func(rec []string, name string) string {
		if i, ok := col[name]; ok && i < len(rec) {
			return strings.TrimSpace(rec[i])
		}
		return ""
	}
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("shops.csv: %w", err)
		}
		sh := shops.Shop{CountryCode: get(rec, "country"), Slug: get(rec, "slug"), Name: get(rec, "name"),
			Website: get(rec, "website"), SearchURLTemplate: get(rec, "search_url_template"), Active: true}
		if sh.Name == "" {
			continue
		}
		if err := s.Shops.Upsert(ctx, &sh); err != nil {
			return fmt.Errorf("shops.csv %s/%s: %w", sh.CountryCode, sh.Slug, err)
		}
		rep.Shops++
	}
	return nil
}

func (s *Seeder) products(ctx context.Context, rep *Report) error {
	files, err := fs.Glob(s.FS, "products/*.csv")
	if err != nil {
		return err
	}
	sort.Strings(files)
	for _, f := range files {
		country := strings.ToUpper(strings.TrimSuffix(path.Base(f), ".csv"))
		r, err := s.FS.Open(f)
		if err != nil {
			return err
		}
		products, err := catalog.ReadCSV(r, country)
		r.Close()
		if err != nil {
			return fmt.Errorf("%s: %w", f, err)
		}
		for _, p := range products {
			p.Source = "seed"
			res, err := s.Catalog.UpsertProduct(ctx, p, true)
			if err != nil {
				return fmt.Errorf("%s %s: %w", f, p.SKU, err)
			}
			if res == catalog.Created {
				rep.ProductsAdded++
			}
		}
	}
	return nil
}

func (s *Seeder) recipes(ctx context.Context, rep *Report) error {
	files, err := fs.Glob(s.FS, "recipes/*.json")
	if err != nil {
		return err
	}
	sort.Strings(files)
	for _, f := range files {
		var in recipes.Input
		if err := s.readJSON(f, &in); err != nil {
			return err
		}
		if in.Slug == "" {
			in.Slug = strings.TrimSuffix(path.Base(f), ".json")
		}
		if _, err := s.Recipes.GetBySlug(ctx, in.Slug); err == nil {
			rep.RecipesExisting++
			continue
		}
		r, err := s.Recipes.FromInput(ctx, in)
		if err != nil {
			return fmt.Errorf("%s: %w", f, err)
		}
		r.Source = recipes.SourceSeed
		if err := s.Recipes.Create(ctx, r, recipes.Change{Source: recipes.SourceSeed, Note: "seed"}); err != nil {
			return fmt.Errorf("%s: %w", f, err)
		}
		rep.RecipesAdded++
	}
	if s.Log != nil {
		s.Log.Info("seed complete", "report", rep.String())
	}
	return nil
}
