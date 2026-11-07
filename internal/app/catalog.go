package app

import (
	"context"
	"errors"
	"io/fs"

	"github.com/abdulovia/cookcompass/internal/catalog"
	"github.com/abdulovia/cookcompass/internal/catalogscan"
	seedfs "github.com/abdulovia/cookcompass/seed"
)

// Scanner builds the catalog scanner from configuration: shop pages listed
// in the catalog targets file, files in the catalog inbox, and the bundled
// seed as fallback.
func (a *App) Scanner() *catalogscan.Scanner {
	targets, err := catalogscan.ReadTargets(a.Cfg.Scrape.CatalogFile)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		a.Log.Warn("catalog targets file unreadable; skipping shop pages", "err", err)
	}
	return &catalogscan.Scanner{
		Catalog: a.Catalog,
		Sources: []catalogscan.CatalogSource{
			&catalogscan.JSONLDSource{Fetcher: a.Fetcher(), Targets: targets, Log: a.Log},
			&catalogscan.FileSource{Dir: a.Cfg.Scrape.InboxDir},
		},
		Seed: &catalogscan.SeedSource{FS: seedfs.FS},
		Log:  a.Log,
	}
}

// ScanCatalog refreshes one country, or every known country for "" / "all".
func (a *App) ScanCatalog(ctx context.Context, country string) ([]catalogscan.Result, error) {
	var codes []string
	if country == "" || country == "all" || country == "ALL" {
		cs, err := a.Catalog.Countries(ctx)
		if err != nil {
			return nil, err
		}
		for _, c := range cs {
			codes = append(codes, c.Code)
		}
	} else {
		c, err := a.Catalog.Country(ctx, country)
		if err != nil {
			return nil, err
		}
		codes = []string{c.Code}
	}
	s := a.Scanner()
	out := make([]catalogscan.Result, 0, len(codes))
	for _, cc := range codes {
		out = append(out, s.Scan(ctx, cc))
	}
	return out, nil
}

// ImportProductsFile imports a CSV/JSON catalog file as an authoritative edit.
func (a *App) ImportProductsFile(ctx context.Context, path, country string) (catalog.ImportResult, error) {
	list, err := catalogscan.ReadCatalogFile(path, country)
	if err != nil {
		return catalog.ImportResult{}, err
	}
	return a.Catalog.Import(ctx, list), nil
}
