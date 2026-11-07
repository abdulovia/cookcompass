// Package testutil provides helpers shared by tests: temporary migrated
// databases and golden-file comparison.
package testutil

import (
	"database/sql"
	"encoding/json"
	"flag"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/abdulovia/cookcompass/internal/db"
)

var update = flag.Bool("update", false, "rewrite golden files")

// DB returns a migrated SQLite database in a temp dir, closed on cleanup.
func DB(t testing.TB) *sql.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := db.Migrate(d, Logger()); err != nil {
		t.Fatal(err)
	}
	return d
}

// Logger discards output.
func Logger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// Golden compares v (marshalled as indented JSON) with testdata/<name>.golden.json.
// Run `go test ./... -update` to rewrite.
func Golden(t testing.TB, name string, v any) {
	t.Helper()
	got, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	GoldenBytes(t, name+".golden.json", got)
}

// GoldenBytes compares raw bytes with testdata/<file>.
func GoldenBytes(t testing.TB, file string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", file)
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s (run with -update to create): %v", path, err)
	}
	if string(want) != string(got) {
		t.Errorf("golden mismatch for %s (run with -update to accept)\n--- want\n%s\n--- got\n%s", path, want, got)
	}
}
