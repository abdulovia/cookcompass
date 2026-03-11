package app

import (
	"context"
	"fmt"
	"sort"

	"github.com/abdulovia/cookcompass/internal/fetch"
	"github.com/abdulovia/cookcompass/internal/ingest"
	"github.com/abdulovia/cookcompass/internal/recipes"
)

// Fetcher returns the polite HTTP client configured for scrapers.
func (a *App) Fetcher() *fetch.Client {
	s := a.Cfg.Scrape
	return fetch.New(s.UserAgent, s.CacheDir, s.CacheTTL, s.MinInterval, s.Timeout)
}

// RecipeSources lists the configured recipe sources by name.
func (a *App) RecipeSources() (map[string]recipes.RecipeSource, error) {
	return ingest.Sources(a.Fetcher(), a.Cfg.Scrape.TargetsFile, a.Log)
}

// RecipeSourceNames returns the sorted source names.
func (a *App) RecipeSourceNames() []string {
	srcs, err := a.RecipeSources()
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(srcs))
	for n := range srcs {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// ScrapeRecipes runs one source and imports its results into the review
// queue. A source that can't reach anything returns its error alongside an
// empty result; the database is never left half-written.
func (a *App) ScrapeRecipes(ctx context.Context, source string, opts recipes.FetchOpts) (ingest.Result, error) {
	srcs, err := a.RecipeSources()
	if err != nil {
		return ingest.Result{}, err
	}
	src, ok := srcs[source]
	if !ok {
		return ingest.Result{}, fmt.Errorf("unknown source %q (available: %v)", source, a.RecipeSourceNames())
	}
	raws, fetchErr := src.Fetch(ctx, opts)
	im, err := ingest.NewImporter(ctx, a.Recipes, a.Log)
	if err != nil {
		return ingest.Result{}, err
	}
	res := im.Import(ctx, raws)
	return res, fetchErr
}
