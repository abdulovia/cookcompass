// Package server wires HTTP routes, middleware and application services.
package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/abdulovia/cookcompass/internal/config"
)

// Version is set at build time via -ldflags.
var Version = "dev"

// Server bundles dependencies shared by handlers.
type Server struct {
	cfg     config.Config
	log     *slog.Logger
	started time.Time
}

// New builds a Server.
func New(cfg config.Config, log *slog.Logger) *Server {
	return &Server{cfg: cfg, log: log, started: time.Now()}
}

// Handler returns the root HTTP handler.
func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.RealIP, middleware.Recoverer)
	r.Get("/healthz", s.healthz)
	return r
}

func (s *Server) healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":  "ok",
		"version": Version,
		"uptime":  time.Since(s.started).Round(time.Second).String(),
	})
}
