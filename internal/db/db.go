// Package db opens the SQLite database and applies migrations.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite" // registers the "sqlite" driver

	"github.com/abdulovia/cookcompass/migrations"
)

// TimeFormat is the textual timestamp format stored in the database.
const TimeFormat = "2006-01-02T15:04:05Z"

// Open opens (creating if needed) the SQLite database at path with WAL,
// foreign keys and a busy timeout enabled.
func Open(path string) (*sql.DB, error) {
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create db dir: %w", err)
		}
	}
	dsn := "file:" + path + "?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)" +
		"&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)&_txlock=immediate"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	db.SetConnMaxIdleTime(5 * time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	return db, nil
}

// Migrate applies all pending migrations.
func Migrate(db *sql.DB, log *slog.Logger) error {
	goose.SetBaseFS(migrations.FS)
	goose.SetLogger(gooseLogger{log})
	if err := goose.SetDialect("sqlite3"); err != nil {
		return err
	}
	return goose.Up(db, ".")
}

// Version returns the current schema version.
func Version(db *sql.DB) (int64, error) {
	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("sqlite3"); err != nil {
		return 0, err
	}
	return goose.GetDBVersion(db)
}

// Backup writes a consistent snapshot of the database to dst using VACUUM INTO.
func Backup(ctx context.Context, db *sql.DB, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if _, err := os.Stat(dst); err == nil {
		return fmt.Errorf("backup target %s already exists", dst)
	}
	_, err := db.ExecContext(ctx, "VACUUM INTO ?", dst)
	return err
}

// Now returns the current UTC time formatted for storage.
func Now() string { return time.Now().UTC().Format(TimeFormat) }

// ParseTime parses a stored timestamp; zero time on failure.
func ParseTime(s string) time.Time {
	t, _ := time.Parse(TimeFormat, s)
	return t
}

// Tx runs fn in a transaction, committing on success.
func Tx(ctx context.Context, db *sql.DB, fn func(*sql.Tx) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

type gooseLogger struct{ log *slog.Logger }

func (g gooseLogger) Fatalf(format string, v ...any) { g.log.Error(fmt.Sprintf(format, v...)) }
func (g gooseLogger) Printf(format string, v ...any) { g.log.Debug(fmt.Sprintf(format, v...)) }
