// Package app wires configuration, the database and domain services into a
// single value shared by the HTTP server and CLI commands.
package app

import (
	"context"
	"database/sql"
	"log/slog"

	"github.com/abdulovia/cookcompass/internal/catalog"
	"github.com/abdulovia/cookcompass/internal/config"
	"github.com/abdulovia/cookcompass/internal/db"
	"github.com/abdulovia/cookcompass/internal/ingredients"
	"github.com/abdulovia/cookcompass/internal/recipes"
	"github.com/abdulovia/cookcompass/internal/seed"
	seedfs "github.com/abdulovia/cookcompass/seed"
)

// App holds long-lived services.
type App struct {
	Cfg     config.Config
	Log     *slog.Logger
	DB      *sql.DB
	Ings    *ingredients.Store
	Recipes *recipes.Store
	Catalog *catalog.Store
}

// Open opens the database, applies migrations and builds services.
func Open(cfg config.Config, log *slog.Logger) (*App, error) {
	d, err := db.Open(cfg.DB.Path)
	if err != nil {
		return nil, err
	}
	if err := db.Migrate(d, log); err != nil {
		_ = d.Close()
		return nil, err
	}
	ings := ingredients.NewStore(d)
	return &App{
		Cfg:     cfg,
		Log:     log,
		DB:      d,
		Ings:    ings,
		Recipes: recipes.NewStore(d, ings, cfg.Recipes.KeepRevisions),
		Catalog: catalog.NewStore(d, ings),
	}, nil
}

// Seed loads the bundled dataset (idempotent).
func (a *App) Seed(ctx context.Context) (seed.Report, error) {
	s := &seed.Seeder{FS: seedfs.FS, Ings: a.Ings, Recipes: a.Recipes, Catalog: a.Catalog, Log: a.Log}
	return s.Run(ctx)
}

// Close releases resources.
func (a *App) Close() error { return a.DB.Close() }
