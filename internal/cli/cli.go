// Package cli implements the cookcompass command-line interface. The same
// binary serves HTTP and runs maintenance commands so it can be used both on
// the host and inside the container.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/abdulovia/cookcompass/internal/app"
	"github.com/abdulovia/cookcompass/internal/config"
	"github.com/abdulovia/cookcompass/internal/db"
	"github.com/abdulovia/cookcompass/internal/server"
)

// App carries process-wide IO so commands are testable.
type App struct {
	Stdout io.Writer
	Stderr io.Writer
	Stdin  io.Reader
}

// env is what every command receives.
type env struct {
	*App
	cfg config.Config
	log *slog.Logger
	ctx context.Context
}

// open opens the database and services.
func (e *env) open() (*app.App, error) { return app.Open(e.cfg, e.log) }

type command struct {
	name    string
	summary string
	run     func(e *env, args []string) error
}

func commands() []command {
	return []command{
		{"serve", "run the HTTP server (migrates and seeds first)", runServe},
		{"migrate", "apply database migrations", runMigrate},
		{"seed", "load the bundled dataset (idempotent)", runSeed},
		{"recipe", "recipe create|delete|restore|list", runRecipe},
		{"scrape", "scrape recipes into the review queue (--source jsonld [--url U] [--limit N])", runScrape},
		{"scan", "refresh product catalogs into the review queue (--country all|US|…)", runScan},
		{"products", "products import --file F --country CC | export --country CC [--out F]", runProducts},
		{"approve-all", "approve every pending recipe and product", runApproveAll},
		{"purge-pending", "delete every pending recipe and product", runPurgePending},
		{"backup", "write a consistent database snapshot (--out path)", runBackup},
		{"healthcheck", "probe /healthz on the local server (for containers)", runHealthcheck},
		{"version", "print version", func(e *env, _ []string) error {
			_, err := fmt.Fprintln(e.Stdout, server.Version)
			return err
		}},
	}
}

// Run dispatches args (without program name) and returns an exit code.
func (a *App) Run(args []string) int {
	if len(args) == 0 {
		args = []string{"serve"}
	}
	name, rest := args[0], args[1:]
	if name == "help" || name == "-h" || name == "--help" {
		a.usage()
		return 0
	}
	for _, c := range commands() {
		if c.name != name {
			continue
		}
		cfg, err := config.Load(configPath())
		if err != nil {
			fmt.Fprintf(a.Stderr, "config: %v\n", err)
			return 2
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		e := &env{App: a, cfg: cfg, log: NewLogger(a.Stderr, cfg.Log), ctx: ctx}
		if err := c.run(e, rest); err != nil {
			fmt.Fprintf(a.Stderr, "%s: %v\n", name, err)
			return 1
		}
		return 0
	}
	fmt.Fprintf(a.Stderr, "unknown command %q\n", name)
	a.usage()
	return 2
}

func (a *App) usage() {
	fmt.Fprintln(a.Stderr, "usage: cookcompass <command> [flags]\n\ncommands:")
	for _, c := range commands() {
		fmt.Fprintf(a.Stderr, "  %-16s %s\n", c.name, c.summary)
	}
}

func configPath() string {
	if p := os.Getenv("COOK_CONFIG"); p != "" {
		return p
	}
	return "config.yaml"
}

// NewLogger builds the process logger.
func NewLogger(w io.Writer, c config.Log) *slog.Logger {
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(c.Level)); err != nil {
		lvl = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: lvl}
	if strings.EqualFold(c.Format, "text") {
		return slog.New(slog.NewTextHandler(w, opts))
	}
	return slog.New(slog.NewJSONHandler(w, opts))
}

func runServe(e *env, _ []string) error {
	ln, err := net.Listen("tcp", e.cfg.Server.Addr)
	if err != nil {
		return err
	}
	return Serve(e.ctx, e.cfg, e.log, ln)
}

// Serve opens the app, seeds it and runs the HTTP server on ln until ctx
// is cancelled.
func Serve(ctx context.Context, cfg config.Config, log *slog.Logger, ln net.Listener) error {
	a, err := app.Open(cfg, log)
	if err != nil {
		_ = ln.Close()
		return err
	}
	defer a.Close()
	if _, err := a.Seed(ctx); err != nil {
		_ = ln.Close()
		return fmt.Errorf("seed: %w", err)
	}
	srv, err := server.New(a)
	if err != nil {
		_ = ln.Close()
		return err
	}
	if cfg.Admin.Password == "" {
		log.Warn("admin password not set: /admin is limited to loopback clients")
	}
	httpSrv := &http.Server{
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       cfg.Server.ReadTimeout,
		WriteTimeout:      cfg.Server.WriteTimeout,
	}
	errc := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", ln.Addr().String(), "version", server.Version)
		errc <- httpSrv.Serve(ln)
	}()
	select {
	case err := <-errc:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
	}
	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return httpSrv.Shutdown(shutdownCtx)
}

func runMigrate(e *env, _ []string) error {
	a, err := e.open()
	if err != nil {
		return err
	}
	defer a.Close()
	v, err := db.Version(a.DB)
	if err != nil {
		return err
	}
	fmt.Fprintf(e.Stdout, "schema version %d\n", v)
	return nil
}

func runSeed(e *env, _ []string) error {
	a, err := e.open()
	if err != nil {
		return err
	}
	defer a.Close()
	rep, err := a.Seed(e.ctx)
	if err != nil {
		return err
	}
	fmt.Fprintln(e.Stdout, rep.String())
	return nil
}

func runBackup(e *env, args []string) error {
	fs := flag.NewFlagSet("backup", flag.ContinueOnError)
	fs.SetOutput(e.Stderr)
	out := fs.String("out", "", "destination file (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *out == "" {
		*out = fmt.Sprintf("backups/cookcompass-%s.db", time.Now().Format("20060102-150405"))
	}
	a, err := e.open()
	if err != nil {
		return err
	}
	defer a.Close()
	if err := db.Backup(e.ctx, a.DB, *out); err != nil {
		return err
	}
	fmt.Fprintf(e.Stdout, "backup written to %s\n", *out)
	return nil
}

func runHealthcheck(e *env, args []string) error {
	fs := flag.NewFlagSet("healthcheck", flag.ContinueOnError)
	fs.SetOutput(e.Stderr)
	url := fs.String("url", "", "health URL (default derived from server.addr)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *url == "" {
		addr := e.cfg.Server.Addr
		if strings.HasPrefix(addr, ":") {
			addr = "127.0.0.1" + addr
		}
		*url = "http://" + addr + "/healthz"
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(*url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unhealthy: %s", resp.Status)
	}
	return nil
}
