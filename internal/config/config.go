// Package config loads application settings from an optional YAML file and
// environment variables. Environment variables always win over the file.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the complete runtime configuration.
type Config struct {
	Server       Server       `yaml:"server"`
	DB           DB           `yaml:"db"`
	Log          Log          `yaml:"log"`
	Admin        Admin        `yaml:"admin"`
	Auth         Auth         `yaml:"auth"`
	Recipes      Recipes      `yaml:"recipes"`
	Scrape       Scrape       `yaml:"scrape"`
	Availability Availability `yaml:"availability"`
	Defaults     Defaults     `yaml:"defaults"`
}

// Server holds HTTP listener settings.
type Server struct {
	Addr         string        `yaml:"addr"`
	ReadTimeout  time.Duration `yaml:"read_timeout"`
	WriteTimeout time.Duration `yaml:"write_timeout"`
}

// DB holds SQLite settings.
type DB struct {
	Path string `yaml:"path"`
}

// Log holds logging settings.
type Log struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"` // json | text
}

// Admin guards the /admin surface. When Password is empty, only loopback
// clients may access it.
type Admin struct {
	User     string `yaml:"user"`
	Password string `yaml:"password"`
}

// Auth controls user accounts.
type Auth struct {
	// MultiUser enables /login and /register. Single-user mode (default)
	// binds every visitor to the local default user.
	MultiUser     bool `yaml:"multi_user"`
	SecureCookies bool `yaml:"secure_cookies"`
}

// Recipes holds recipe-store settings.
type Recipes struct {
	KeepRevisions int `yaml:"keep_revisions"`
}

// Scrape configures the polite HTTP fetcher used by all scrapers.
type Scrape struct {
	UserAgent   string        `yaml:"user_agent"`
	CacheDir    string        `yaml:"cache_dir"`
	CacheTTL    time.Duration `yaml:"cache_ttl"`
	MinInterval time.Duration `yaml:"min_interval"`
	Timeout     time.Duration `yaml:"timeout"`
	TargetsFile string        `yaml:"targets_file"`
	CatalogFile string        `yaml:"catalog_targets_file"`
	InboxDir    string        `yaml:"catalog_inbox_dir"`
	DatasetDir  string        `yaml:"recipe_inbox_dir"` // dataset drops for the "dataset" recipe source
}

// Availability configures the recipe feasibility matcher.
type Availability struct {
	// Threshold is the fraction (0..1] of essential ingredients that must be
	// obtainable (directly or via substitute) for a recipe to count as available.
	Threshold float64 `yaml:"threshold"`
}

// Defaults are used for new users and when detection fails.
type Defaults struct {
	Country string `yaml:"country"`
	Locale  string `yaml:"locale"`
}

// Default returns a configuration that works out of the box on a laptop.
func Default() Config {
	return Config{
		Server: Server{Addr: ":8080", ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second},
		DB:     DB{Path: "data/cookcompass.db"},
		Log:    Log{Level: "info", Format: "json"},
		Admin:  Admin{User: "admin"},
		Recipes: Recipes{
			KeepRevisions: 20,
		},
		Scrape: Scrape{
			UserAgent:   "CookCompassBot/1.0 (+https://github.com/abdulovia/cookcompass; local recipe indexer)",
			CacheDir:    "data/cache/http",
			CacheTTL:    7 * 24 * time.Hour,
			MinInterval: time.Second,
			Timeout:     15 * time.Second,
			TargetsFile: "seed/sources/jsonld_targets.txt",
			CatalogFile: "seed/sources/catalog_targets.txt",
			InboxDir:    "data/catalog-inbox",
			DatasetDir:  "data/recipe-inbox",
		},
		Availability: Availability{Threshold: 1.0},
		Defaults:     Defaults{Country: "US", Locale: "en"},
	}
}

// Load reads the YAML file at path (missing file is fine) and applies
// COOK_* environment overrides.
func Load(path string) (Config, error) {
	cfg := Default()
	if path != "" {
		b, err := os.ReadFile(path)
		switch {
		case err == nil:
			if err := yaml.Unmarshal(b, &cfg); err != nil {
				return cfg, fmt.Errorf("parse %s: %w", path, err)
			}
		case errors.Is(err, os.ErrNotExist):
			// optional file
		default:
			return cfg, fmt.Errorf("read %s: %w", path, err)
		}
	}
	if err := applyEnv(&cfg, os.LookupEnv); err != nil {
		return cfg, err
	}
	return cfg, cfg.Validate()
}

// Validate checks invariants.
func (c Config) Validate() error {
	var errs []error
	if c.Server.Addr == "" {
		errs = append(errs, errors.New("server.addr is required"))
	}
	if c.DB.Path == "" {
		errs = append(errs, errors.New("db.path is required"))
	}
	if c.Recipes.KeepRevisions < 1 {
		errs = append(errs, errors.New("recipes.keep_revisions must be >= 1"))
	}
	if c.Availability.Threshold <= 0 || c.Availability.Threshold > 1 {
		errs = append(errs, errors.New("availability.threshold must be in (0,1]"))
	}
	if c.Scrape.MinInterval < time.Second {
		errs = append(errs, errors.New("scrape.min_interval must be >= 1s"))
	}
	return errors.Join(errs...)
}

type lookupFunc func(string) (string, bool)

func applyEnv(c *Config, lookup lookupFunc) error {
	str := func(key string, dst *string) {
		if v, ok := lookup(key); ok && v != "" {
			*dst = v
		}
	}
	var errs []error
	boolean := func(key string, dst *bool) {
		if v, ok := lookup(key); ok && v != "" {
			b, err := strconv.ParseBool(v)
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", key, err))
				return
			}
			*dst = b
		}
	}
	integer := func(key string, dst *int) {
		if v, ok := lookup(key); ok && v != "" {
			n, err := strconv.Atoi(v)
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", key, err))
				return
			}
			*dst = n
		}
	}
	float := func(key string, dst *float64) {
		if v, ok := lookup(key); ok && v != "" {
			f, err := strconv.ParseFloat(v, 64)
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", key, err))
				return
			}
			*dst = f
		}
	}
	dur := func(key string, dst *time.Duration) {
		if v, ok := lookup(key); ok && v != "" {
			d, err := time.ParseDuration(v)
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", key, err))
				return
			}
			*dst = d
		}
	}

	str("COOK_ADDR", &c.Server.Addr)
	str("COOK_DB", &c.DB.Path)
	str("COOK_LOG_LEVEL", &c.Log.Level)
	str("COOK_LOG_FORMAT", &c.Log.Format)
	str("COOK_ADMIN_USER", &c.Admin.User)
	str("COOK_ADMIN_PASSWORD", &c.Admin.Password)
	boolean("COOK_MULTI_USER", &c.Auth.MultiUser)
	boolean("COOK_SECURE_COOKIES", &c.Auth.SecureCookies)
	integer("COOK_KEEP_REVISIONS", &c.Recipes.KeepRevisions)
	str("COOK_SCRAPE_UA", &c.Scrape.UserAgent)
	str("COOK_SCRAPE_CACHE", &c.Scrape.CacheDir)
	dur("COOK_SCRAPE_MIN_INTERVAL", &c.Scrape.MinInterval)
	str("COOK_SCRAPE_TARGETS", &c.Scrape.TargetsFile)
	str("COOK_CATALOG_TARGETS", &c.Scrape.CatalogFile)
	str("COOK_CATALOG_INBOX", &c.Scrape.InboxDir)
	str("COOK_RECIPE_INBOX", &c.Scrape.DatasetDir)
	float("COOK_AVAILABILITY_THRESHOLD", &c.Availability.Threshold)
	str("COOK_DEFAULT_COUNTRY", &c.Defaults.Country)
	str("COOK_DEFAULT_LOCALE", &c.Defaults.Locale)
	c.Defaults.Country = strings.ToUpper(c.Defaults.Country)
	return errors.Join(errs...)
}
