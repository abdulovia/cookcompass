// Package app wires configuration, the database and domain services into a
// single value shared by the HTTP server and CLI commands.
package app

import (
	"context"
	"database/sql"
	"io/fs"
	"log/slog"
	"os"

	"github.com/abdulovia/cookcompass/internal/catalog"
	"github.com/abdulovia/cookcompass/internal/config"
	"github.com/abdulovia/cookcompass/internal/content"
	"github.com/abdulovia/cookcompass/internal/db"
	"github.com/abdulovia/cookcompass/internal/ingredients"
	"github.com/abdulovia/cookcompass/internal/pool"
	"github.com/abdulovia/cookcompass/internal/recipes"
	"github.com/abdulovia/cookcompass/internal/seed"
	"github.com/abdulovia/cookcompass/internal/shops"
	"github.com/abdulovia/cookcompass/internal/storage"
	"github.com/abdulovia/cookcompass/internal/users"
	seedfs "github.com/abdulovia/cookcompass/seed"
	webfs "github.com/abdulovia/cookcompass/web"
)

// App holds long-lived services.
type App struct {
	Cfg     config.Config
	Log     *slog.Logger
	DB      *sql.DB
	Ings    *ingredients.Store
	Recipes *recipes.Store
	Catalog *catalog.Store
	Shops   *shops.Store
	Users   *users.Store
	Pool    *pool.Pool
	// Media mirrors recipe images into S3 storage; Disabled when unconfigured.
	Media *Media
	// Labels are display names of cuisines, countries and categories from
	// the bundled data and the content directory.
	Labels *content.Labels
	// DefaultUserID is the account used in single-user mode.
	DefaultUserID int64
}

// Open opens the database, applies migrations and builds services.
func Open(cfg config.Config, log *slog.Logger) (*App, error) {
	// Inbox directories are drop zones; create them on start so upgrades
	// (and fresh non-Docker runs) don't require manual mkdir.
	for _, dir := range []string{cfg.Scrape.InboxDir, cfg.Scrape.DatasetDir} {
		if dir != "" {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, err
			}
		}
	}
	d, err := db.Open(cfg.DB.Path)
	if err != nil {
		return nil, err
	}
	if err := db.Migrate(d, log); err != nil {
		_ = d.Close()
		return nil, err
	}
	ings := ingredients.NewStore(d)
	labels, err := content.LoadLabels(content.Layer(cfg.Content.Dir, seedfs.FS))
	if err != nil {
		_ = d.Close()
		return nil, err
	}
	a := &App{
		Labels:  labels,
		Cfg:     cfg,
		Log:     log,
		DB:      d,
		Ings:    ings,
		Recipes: recipes.NewStore(d, ings, cfg.Recipes.KeepRevisions),
		Catalog: catalog.NewStore(d, ings),
		Shops:   shops.NewStore(d),
		Users:   users.NewStore(d),
	}
	a.Pool = &pool.Pool{Recipes: a.Recipes, Ings: ings, Catalog: a.Catalog, Threshold: cfg.Availability.Threshold}
	a.Media = &Media{Backend: storage.NewS3(cfg.Storage.S3, log), Fetcher: a.Fetcher(), Recipes: a.Recipes}
	return a, nil
}

// Seed loads the bundled dataset (idempotent) and makes sure the default
// single-user account exists.
func (a *App) Seed(ctx context.Context) (seed.Report, error) {
	s := &seed.Seeder{FS: a.SeedFS(), Ings: a.Ings, Recipes: a.Recipes, Catalog: a.Catalog, Shops: a.Shops, Log: a.Log}
	rep, err := s.Run(ctx)
	if err != nil {
		return rep, err
	}
	a.DefaultUserID, err = a.Users.EnsureDefault(ctx, a.Cfg.Defaults.Country)
	return rep, err
}

// SeedFS is the bundled dataset overlaid with the content directory.
func (a *App) SeedFS() fs.FS { return content.Layer(a.Cfg.Content.Dir, seedfs.FS) }

// WebFS is the embedded UI (templates, static assets, locales) overlaid
// with the content directory's locales/ and static/.
func (a *App) WebFS() fs.FS { return content.Layer(a.Cfg.Content.Dir, webfs.FS) }

// Close releases resources.
func (a *App) Close() error { return a.DB.Close() }
