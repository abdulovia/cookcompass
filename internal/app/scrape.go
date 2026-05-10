package app

import (
	"context"
	"fmt"
	"path/filepath"
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
	return ingest.Sources(a.Fetcher(), a.Cfg.Scrape.TargetsFile, a.Cfg.Scrape.DatasetDir, a.Log)
}

// DatasetOptions describe one dataset import run.
type DatasetOptions struct {
	File    string // inbox file name (not a path)
	URL     string // http(s) or file: URL; wins over File
	Limit   int    // cap recipes per run (0 = all)
	Approve bool   // approve the newly queued recipes immediately
}

// ImportDataset runs the dataset source once and imports what it finds into
// the review queue. With Approve set, everything this run left in the queue
// (new recipes and still-pending duplicates) is approved — earlier pending
// items from other sources stay queued.
func (a *App) ImportDataset(ctx context.Context, opt DatasetOptions) (ingest.Result, error) {
	opts := recipes.FetchOpts{Limit: opt.Limit}
	switch {
	case opt.URL != "":
		opts.URLs = []string{opt.URL}
	case opt.File != "":
		opts.URLs = []string{"file:" + filepath.Join(a.Cfg.Scrape.DatasetDir, opt.File)}
	}
	srcs, err := a.RecipeSources()
	if err != nil {
		return ingest.Result{}, err
	}
	raws, fetchErr := srcs["dataset"].Fetch(ctx, opts)
	im, err := ingest.NewImporter(ctx, a.Recipes, a.Log)
	if err != nil {
		return ingest.Result{}, err
	}
	res := im.Import(ctx, raws)
	if opt.Approve {
		for _, id := range res.PendingIDs {
			if err := a.Recipes.SetStatus(ctx, id, recipes.StatusApproved, recipes.Change{Source: "dataset", Note: "approved on import"}); err != nil {
				res.Errors = append(res.Errors, err)
			}
		}
	}
	return res, fetchErr
}

// RecipeInboxFiles lists dataset drops for the admin import form.
func (a *App) RecipeInboxFiles() []string {
	files, err := ingest.InboxFiles(a.Cfg.Scrape.DatasetDir)
	if err != nil {
		a.Log.Warn("recipe inbox unreadable", "err", err)
		return nil
	}
	return files
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
