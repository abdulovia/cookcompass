// Package admin serves the local-only management UI: recipes, products,
// review queues and revision history.
package admin

import (
	"crypto/subtle"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/abdulovia/cookcompass/internal/app"
	"github.com/abdulovia/cookcompass/internal/config"
	"github.com/abdulovia/cookcompass/internal/recipes"
	"github.com/abdulovia/cookcompass/internal/web"
)

// Handler serves /admin.
type Handler struct {
	App *app.App
	R   *web.Renderer
	Log *slog.Logger
	// ContentLocales are the locales offered for recipe translations.
	ContentLocales []string
}

// Routes registers admin routes on r.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/", h.dashboard)
	r.Get("/recipes", h.recipeList)
	r.Get("/recipes/new", h.recipeNew)
	r.Post("/recipes", h.recipeCreate)
	r.Get("/recipes/{id}/edit", h.recipeEdit)
	r.Post("/recipes/{id}", h.recipeUpdate)
	r.Post("/recipes/{id}/delete", h.recipeDelete)
	r.Post("/recipes/{id}/restore", h.recipeRestore)
	r.Get("/recipes/{id}/revisions", h.recipeRevisions)
	r.Get("/queue", h.queue)
	r.Post("/queue/recipes/{id}/approve", h.setRecipeStatus(recipes.StatusApproved, "approved"))
	r.Post("/queue/recipes/{id}/reject", h.setRecipeStatus(recipes.StatusRejected, "rejected"))
	r.Post("/queue/approve-all", h.approveAll)
	r.Post("/queue/purge", h.purgePending)
	r.Post("/queue/scrape", h.scrapeNow)
	h.productRoutes(r)
}

// Guard protects the admin surface. With a password configured, HTTP basic
// auth is required; otherwise only loopback clients are allowed.
func Guard(cfg config.Admin) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if cfg.Password != "" {
				u, p, ok := r.BasicAuth()
				if ok && subtle.ConstantTimeCompare([]byte(u), []byte(cfg.User)) == 1 &&
					subtle.ConstantTimeCompare([]byte(p), []byte(cfg.Password)) == 1 {
					next.ServeHTTP(w, r)
					return
				}
				w.Header().Set("WWW-Authenticate", `Basic realm="CookCompass admin", charset="UTF-8"`)
				http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
				return
			}
			if isLoopback(r.RemoteAddr) {
				next.ServeHTTP(w, r)
				return
			}
			http.Error(w, "admin is only available from this computer (set COOK_ADMIN_PASSWORD to allow remote access)", http.StatusForbidden)
		})
	}
}

func isLoopback(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (h *Handler) page(r *http.Request, title string) *web.Page {
	p := web.NewPage(r, title)
	p.Admin = true
	return p
}

func (h *Handler) dashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	counts, err := h.App.Recipes.Counts(ctx)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	products, err := h.App.Catalog.ProductCounts(ctx)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	p := h.page(r, "admin.dashboard")
	p.Data = map[string]any{"Recipes": counts, "Products": products}
	h.R.Page(w, http.StatusOK, "admin/dashboard", p)
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	h.Log.Error("admin request failed", "path", r.URL.Path, "err", err)
	http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
}

func idParam(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, errors.New("bad id")
	}
	return id, nil
}
