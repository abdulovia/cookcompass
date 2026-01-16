package db

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"
)

func TestOpenMigrateBackup(t *testing.T) {
	dir := t.TempDir()
	d, err := Open(filepath.Join(dir, "sub", "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := Migrate(d, log); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(d, log); err != nil {
		t.Fatalf("second migrate should be a no-op: %v", err)
	}
	v, err := Version(d)
	if err != nil || v < 1 {
		t.Fatalf("version %d err %v", v, err)
	}
	var fk int
	if err := d.QueryRow("PRAGMA foreign_keys").Scan(&fk); err != nil || fk != 1 {
		t.Fatalf("foreign keys not enabled: %d %v", fk, err)
	}

	ctx := context.Background()
	err = Tx(ctx, d, func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO cuisines (slug, name) VALUES ('x', 'X')`)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("boom")
	err = Tx(ctx, d, func(tx *sql.Tx) error {
		_, _ = tx.Exec(`INSERT INTO cuisines (slug, name) VALUES ('y', 'Y')`)
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("want sentinel, got %v", err)
	}
	var n int
	_ = d.QueryRow(`SELECT COUNT(*) FROM cuisines`).Scan(&n)
	if n != 1 {
		t.Fatalf("rollback failed, %d rows", n)
	}

	dst := filepath.Join(dir, "backups", "b.db")
	if err := Backup(ctx, d, dst); err != nil {
		t.Fatal(err)
	}
	if err := Backup(ctx, d, dst); err == nil {
		t.Fatal("overwriting a backup must fail")
	}
}

func TestRevisionsAppendOnly(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := Migrate(d, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatal(err)
	}
	mustExec := func(q string) {
		t.Helper()
		if _, err := d.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	mustExec(`INSERT INTO recipes (id, slug, name, difficulty, dedup_key) VALUES (1, 'a', 'A', 'easy', 'k')`)
	mustExec(`INSERT INTO recipe_revisions (recipe_id, rev, source, snapshot_json) VALUES (1, 1, 'user', '{}')`)
	if _, err := d.Exec(`UPDATE recipe_revisions SET note = 'x'`); err == nil {
		t.Fatal("update on recipe_revisions must be rejected")
	}
}

func TestTimeHelpers(t *testing.T) {
	ts := Now()
	if ParseTime(ts).IsZero() {
		t.Fatalf("roundtrip failed for %q", ts)
	}
	if !ParseTime("garbage").IsZero() {
		t.Fatal("garbage should parse to zero")
	}
	if time.Since(ParseTime(ts)) > time.Minute {
		t.Fatal("Now is not current")
	}
}

func TestOpenBadPath(t *testing.T) {
	if _, err := Open("/dev/null/x/y.db"); err == nil {
		t.Fatal("expected error")
	}
}
