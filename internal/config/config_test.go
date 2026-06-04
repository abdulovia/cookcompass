package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultIsValid(t *testing.T) {
	if err := Default().Validate(); err != nil {
		t.Fatalf("default config invalid: %v", err)
	}
}

func TestLoadFileAndEnv(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.yaml")
	yml := "server:\n  addr: \":9090\"\nrecipes:\n  keep_revisions: 5\ndefaults:\n  country: de\n"
	if err := os.WriteFile(p, []byte(yml), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("COOK_DB", "/tmp/x.db")
	t.Setenv("COOK_MULTI_USER", "true")
	t.Setenv("COOK_SCRAPE_MIN_INTERVAL", "2s")
	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Addr != ":9090" || cfg.Recipes.KeepRevisions != 5 {
		t.Errorf("file values not applied: %+v", cfg)
	}
	if cfg.DB.Path != "/tmp/x.db" || !cfg.Auth.MultiUser || cfg.Scrape.MinInterval != 2*time.Second {
		t.Errorf("env values not applied: %+v", cfg)
	}
	if cfg.Defaults.Country != "DE" {
		t.Errorf("country should be upper-cased, got %q", cfg.Defaults.Country)
	}
}

func TestLoadMissingFile(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "nope.yaml")); err != nil {
		t.Fatalf("missing file should be ignored: %v", err)
	}
}

func TestLoadErrors(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		yml  string
	}{
		{"bad bool", map[string]string{"COOK_MULTI_USER": "maybe"}, ""},
		{"bad int", map[string]string{"COOK_KEEP_REVISIONS": "x"}, ""},
		{"bad float", map[string]string{"COOK_AVAILABILITY_THRESHOLD": "x"}, ""},
		{"bad duration", map[string]string{"COOK_SCRAPE_MIN_INTERVAL": "soon"}, ""},
		{"threshold out of range", map[string]string{"COOK_AVAILABILITY_THRESHOLD": "1.5"}, ""},
		{"too fast scraping", map[string]string{"COOK_SCRAPE_MIN_INTERVAL": "100ms"}, ""},
		{"zero revisions", map[string]string{"COOK_KEEP_REVISIONS": "0"}, ""},
		{"bad yaml", nil, "server: [\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			p := ""
			if tc.yml != "" {
				p = filepath.Join(t.TempDir(), "c.yaml")
				if err := os.WriteFile(p, []byte(tc.yml), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := Load(p); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestValidateEmptyFields(t *testing.T) {
	c := Default()
	c.Server.Addr = ""
	c.DB.Path = ""
	if err := c.Validate(); err == nil {
		t.Fatal("expected error")
	}
}

func TestStorageEnvOverrides(t *testing.T) {
	t.Setenv("COOK_S3_ENDPOINT", "minio:9000")
	t.Setenv("COOK_S3_ACCESS_KEY", "ak")
	t.Setenv("COOK_S3_SECRET_KEY", "sk")
	t.Setenv("COOK_S3_BUCKET", "bkt")
	t.Setenv("COOK_S3_SECURE", "true")
	t.Setenv("COOK_S3_CACHE_IMAGES", "false")
	t.Setenv("COOK_RECIPE_INBOX", "/tmp/inbox")
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Storage.S3.Endpoint != "minio:9000" || cfg.Storage.S3.AccessKey != "ak" || cfg.Storage.S3.SecretKey != "sk" ||
		cfg.Storage.S3.Bucket != "bkt" || !cfg.Storage.S3.Secure || cfg.Storage.S3.CacheImages {
		t.Fatalf("s3 env: %+v", cfg.Storage.S3)
	}
	if cfg.Scrape.DatasetDir != "/tmp/inbox" {
		t.Fatalf("recipe inbox: %q", cfg.Scrape.DatasetDir)
	}
}
