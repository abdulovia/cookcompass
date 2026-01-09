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

	"github.com/abdulovia/cookcompass/internal/config"
	"github.com/abdulovia/cookcompass/internal/server"
)

// App carries process-wide IO so commands are testable.
type App struct {
	Stdout io.Writer
	Stderr io.Writer
	Stdin  io.Reader
}

type command struct {
	name    string
	summary string
	run     func(a *App, cfg config.Config, log *slog.Logger, args []string) error
}

func (a *App) commands() []command {
	return []command{
		{"serve", "run the HTTP server", runServe},
		{"healthcheck", "probe /healthz on the local server (for containers)", runHealthcheck},
		{"version", "print version", func(a *App, _ config.Config, _ *slog.Logger, _ []string) error {
			_, err := fmt.Fprintln(a.Stdout, server.Version)
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
	for _, c := range a.commands() {
		if c.name != name {
			continue
		}
		cfg, err := config.Load(configPath())
		if err != nil {
			fmt.Fprintf(a.Stderr, "config: %v\n", err)
			return 2
		}
		log := NewLogger(a.Stderr, cfg.Log)
		if err := c.run(a, cfg, log, rest); err != nil {
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
	for _, c := range a.commands() {
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

func runServe(_ *App, cfg config.Config, log *slog.Logger, _ []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ln, err := net.Listen("tcp", cfg.Server.Addr)
	if err != nil {
		return err
	}
	return Serve(ctx, cfg, log, ln)
}

// Serve runs the HTTP server on ln until ctx is cancelled.
func Serve(ctx context.Context, cfg config.Config, log *slog.Logger, ln net.Listener) error {
	srv := server.New(cfg, log)
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

func runHealthcheck(_ *App, cfg config.Config, _ *slog.Logger, args []string) error {
	fs := flag.NewFlagSet("healthcheck", flag.ContinueOnError)
	url := fs.String("url", "", "health URL (default derived from server.addr)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *url == "" {
		addr := cfg.Server.Addr
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
