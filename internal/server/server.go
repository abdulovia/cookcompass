// Package server wires HTTP routes, middleware and application services.
package server

import (
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/abdulovia/cookcompass/internal/admin"
	"github.com/abdulovia/cookcompass/internal/app"
	"github.com/abdulovia/cookcompass/internal/i18n"
	"github.com/abdulovia/cookcompass/internal/web"
	webfs "github.com/abdulovia/cookcompass/web"
)

// Version is set at build time via -ldflags.
var Version = "dev"

// LangCookie stores the explicitly chosen UI language.
const LangCookie = "lang"

// Server bundles dependencies shared by handlers.
type Server struct {
	app     *app.App
	log     *slog.Logger
	i18n    *i18n.Bundle
	render  *web.Renderer
	started time.Time
}

// New builds a Server around an opened App.
func New(a *app.App) (*Server, error) {
	bundle, err := i18n.Load(webfs.FS, "locales")
	if err != nil {
		return nil, err
	}
	r, err := web.NewRenderer(webfs.FS)
	if err != nil {
		return nil, err
	}
	return &Server{app: a, log: a.Log, i18n: bundle, render: r, started: time.Now()}, nil
}

// Handler returns the root HTTP handler.
func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID, s.logRequests, middleware.Recoverer, securityHeaders, sameOrigin, s.locale)

	static, _ := fs.Sub(webfs.FS, "static")
	r.Handle("/static/*", http.StripPrefix("/static/", cacheStatic(http.FileServer(http.FS(static)))))
	r.Get("/healthz", s.healthz)
	r.Post("/lang", s.setLanguage)
	r.Get("/", s.home)

	adm := &admin.Handler{App: s.app, R: s.render, Log: s.log, ContentLocales: s.contentLocales()}
	r.Route("/admin", func(r chi.Router) {
		r.Use(admin.Guard(s.app.Cfg.Admin))
		adm.Routes(r)
	})
	return r
}

// contentLocales are the non-base locales recipes can be translated into.
func (s *Server) contentLocales() []string {
	var out []string
	for _, l := range s.i18n.Languages() {
		if l.Code != i18n.Default {
			out = append(out, l.Code)
		}
	}
	return out
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	status, code := "ok", http.StatusOK
	if err := s.app.DB.PingContext(r.Context()); err != nil {
		status, code = "db unavailable", http.StatusServiceUnavailable
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":  status,
		"version": Version,
		"uptime":  time.Since(s.started).Round(time.Second).String(),
	})
}

func (s *Server) home(w http.ResponseWriter, r *http.Request) {
	p := web.NewPage(r, "")
	s.render.Page(w, http.StatusOK, "pages/home", p)
}

// setLanguage stores the chosen language in a cookie and goes back.
func (s *Server) setLanguage(w http.ResponseWriter, r *http.Request) {
	lang := r.FormValue("lang")
	if s.i18n.Has(lang) {
		http.SetCookie(w, &http.Cookie{Name: LangCookie, Value: lang, Path: "/", MaxAge: 365 * 24 * 3600,
			HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: s.app.Cfg.Auth.SecureCookies})
	}
	web.Redirect(w, r, safeBack(r.FormValue("back")), "")
}

// safeBack only allows local absolute paths as redirect targets.
func safeBack(back string) string {
	if !strings.HasPrefix(back, "/") || strings.HasPrefix(back, "//") || strings.Contains(back, "\\") {
		return "/"
	}
	return back
}

// locale picks the request language: cookie, then Accept-Language.
func (s *Server) locale(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pref := ""
		if c, err := r.Cookie(LangCookie); err == nil {
			pref = c.Value
		}
		lang := s.i18n.Match(pref, r.Header.Get("Accept-Language"))
		ctx := web.WithLocalizer(r.Context(), s.i18n.Localizer(lang), s.i18n.Languages())
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		if strings.HasPrefix(r.URL.Path, "/static/") || r.URL.Path == "/healthz" {
			return
		}
		s.log.Info("request", "method", r.Method, "path", r.URL.Path, "status", ww.Status(),
			"bytes", ww.BytesWritten(), "duration_ms", time.Since(start).Milliseconds(),
			"request_id", middleware.GetReqID(r.Context()))
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		// Alpine.js evaluates attribute expressions, which needs 'unsafe-eval'.
		h.Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline' 'unsafe-eval'; "+
			"style-src 'self' 'unsafe-inline'; img-src 'self' https: data:; frame-src https://www.youtube-nocookie.com; "+
			"connect-src 'self'; form-action 'self'; base-uri 'self'; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}

// sameOrigin rejects cross-site state-changing requests (CSRF defence that
// needs no tokens): when a browser sends Origin or Referer, its host must
// match the request host.
func sameOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		src := r.Header.Get("Origin")
		if src == "" || src == "null" {
			src = r.Header.Get("Referer")
		}
		if src != "" {
			u, err := url.Parse(src)
			if err != nil || !strings.EqualFold(u.Host, r.Host) {
				http.Error(w, "cross-site request blocked", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func cacheStatic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=3600")
		next.ServeHTTP(w, r)
	})
}
