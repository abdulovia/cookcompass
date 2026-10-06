package app

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/abdulovia/cookcompass/internal/db"
	"github.com/abdulovia/cookcompass/internal/recipes"
)

// Check is one item of the setup checklist shown by `cookcompass doctor`
// and on the admin dashboard. Labels and fixes are UI text ids
// ("check.<ID>", "check.<ID>_fix") so both surfaces read the same words.
type Check struct {
	ID       string
	OK       bool
	Optional bool   // nice to have; never fails `doctor`
	Detail   string // facts, not prose: counts, paths, versions
}

// insecurePasswords are values from the examples that must never guard a
// reachable admin.
var insecurePasswords = map[string]bool{"cookcompass": true, "change-me": true, "admin": true, "password": true}

// Checks inspects the running installation. It never modifies anything
// except a throw-away probe file in each data directory.
func (a *App) Checks(ctx context.Context) []Check {
	var out []Check
	add := func(c Check) { out = append(out, c) }

	// Database and schema.
	if err := a.DB.PingContext(ctx); err != nil {
		add(Check{ID: "database", Detail: err.Error()})
	} else if v, err := db.Version(a.DB); err != nil {
		add(Check{ID: "database", Detail: err.Error()})
	} else {
		add(Check{ID: "database", OK: true, Detail: fmt.Sprintf("%s · schema v%d", a.Cfg.DB.Path, v)})
	}

	// Admin protection: no password = loopback only (fine on a laptop); a
	// known example password is never fine.
	switch pw := a.Cfg.Admin.Password; {
	case pw == "":
		add(Check{ID: "admin_password", OK: true, Detail: "loopback only"})
	case insecurePasswords[strings.ToLower(pw)]:
		add(Check{ID: "admin_password", Detail: "example password in use"})
	default:
		add(Check{ID: "admin_password", OK: true, Detail: "user " + a.Cfg.Admin.User})
	}

	// Writable data directories.
	var bad []string
	for _, dir := range []string{filepath.Dir(a.Cfg.DB.Path), a.Cfg.Scrape.CacheDir, a.Cfg.Scrape.InboxDir, a.Cfg.Scrape.DatasetDir} {
		if dir == "" {
			continue
		}
		if err := probeWritable(dir); err != nil {
			bad = append(bad, dir)
		}
	}
	add(Check{ID: "data_dirs", OK: len(bad) == 0, Detail: strings.Join(bad, ", ")})

	// Content: recipes, own recipes, catalog, default country.
	counts, err := a.Recipes.Counts(ctx)
	approved := counts[recipes.StatusApproved]
	add(Check{ID: "recipes", OK: err == nil && approved > 0, Detail: fmt.Sprintf("%d approved · %d pending", approved, counts[recipes.StatusPending])})
	var own int
	_ = a.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM recipes WHERE deleted_at IS NULL AND source <> ?`, recipes.SourceSeed).Scan(&own)
	add(Check{ID: "own_recipes", OK: own > 0, Optional: true, Detail: fmt.Sprintf("%d", own)})
	products, err := a.Catalog.ProductCounts(ctx)
	add(Check{ID: "catalog", OK: err == nil && len(products) > 0, Detail: fmt.Sprintf("%d countries", len(products))})
	_, cerr := a.Catalog.Country(ctx, a.Cfg.Defaults.Country)
	add(Check{ID: "default_country", OK: cerr == nil, Detail: a.Cfg.Defaults.Country})

	// Content directory (optional: an empty install uses bundled data).
	if st, err := os.Stat(a.Cfg.Content.Dir); err == nil && st.IsDir() {
		add(Check{ID: "content_dir", OK: true, Optional: true, Detail: a.Cfg.Content.Dir})
	} else {
		add(Check{ID: "content_dir", Optional: true, Detail: a.Cfg.Content.Dir})
	}

	// Optional services.
	add(Check{ID: "storage", OK: a.Media.Enabled(), Optional: true, Detail: orDash(a.Cfg.Storage.S3.Endpoint)})
	add(a.translatorCheck(ctx))
	return out
}

func (a *App) translatorCheck(ctx context.Context) Check {
	c := Check{ID: "translator", Optional: true, Detail: orDash(a.Cfg.Translate.URL)}
	if a.Cfg.Translate.URL == "" {
		return c
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(a.Cfg.Translate.URL, "/")+"/languages", nil)
	if err != nil {
		return c
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.Detail += " · unreachable"
		return c
	}
	resp.Body.Close()
	c.OK = resp.StatusCode == http.StatusOK
	return c
}

// ChecksPassed reports whether every required check is OK.
func ChecksPassed(cs []Check) bool {
	for _, c := range cs {
		if !c.OK && !c.Optional {
			return false
		}
	}
	return true
}

func probeWritable(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".doctor-*")
	if err != nil {
		return err
	}
	name := f.Name()
	f.Close()
	return os.Remove(name)
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
