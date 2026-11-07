package admin

import (
	"context"
	"errors"
	"net/http"

	"github.com/abdulovia/cookcompass/internal/recipes"
	"github.com/abdulovia/cookcompass/internal/web"
)

var reviewChange = recipes.Change{Source: recipes.SourceUser, Note: "review"}

func (h *Handler) queue(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	pending, err := h.App.Recipes.List(ctx, recipes.Filter{Statuses: []string{recipes.StatusPending}, Deleted: recipes.DeletedInclude})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	data := map[string]any{"Recipes": pending, "Sources": h.App.RecipeSourceNames()}
	for k, v := range h.queueExtra(ctx) {
		data[k] = v
	}
	p := h.page(r, "admin.queue")
	p.Data = data
	h.R.Page(w, http.StatusOK, "admin/queue", p)
}

// queueExtra lets other review queues (products) add their rows.
func (h *Handler) queueExtra(ctx context.Context) map[string]any { return h.productQueueData(ctx) }

// reviewed answers an approve/reject: HTMX gets an empty body (the row is
// swapped out), plain forms are redirected back to the queue.
func reviewed(w http.ResponseWriter, r *http.Request, flash string) {
	if r.Header.Get("HX-Request") == "true" {
		w.WriteHeader(http.StatusOK)
		return
	}
	web.Redirect(w, r, "/admin/queue", flash)
}

func (h *Handler) setRecipeStatus(status, flash string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := idParam(r)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		err = h.App.Recipes.SetStatus(r.Context(), id, status, reviewChange)
		if errors.Is(err, recipes.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			h.fail(w, r, err)
			return
		}
		reviewed(w, r, flash)
	}
}

func (h *Handler) approveAll(w http.ResponseWriter, r *http.Request) {
	if _, err := h.App.Recipes.ApproveAllPending(r.Context(), reviewChange); err != nil {
		h.fail(w, r, err)
		return
	}
	if _, err := h.App.Catalog.ApproveAllPending(r.Context()); err != nil {
		h.fail(w, r, err)
		return
	}
	web.Redirect(w, r, "/admin/queue", "approved_all")
}

func (h *Handler) purgePending(w http.ResponseWriter, r *http.Request) {
	if _, err := h.App.Recipes.PurgePending(r.Context()); err != nil {
		h.fail(w, r, err)
		return
	}
	if _, err := h.App.Catalog.PurgePending(r.Context()); err != nil {
		h.fail(w, r, err)
		return
	}
	web.Redirect(w, r, "/admin/queue", "purged")
}

func (h *Handler) scrapeNow(w http.ResponseWriter, r *http.Request) {
	res, err := h.App.ScrapeRecipes(r.Context(), r.FormValue("source"), recipes.FetchOpts{})
	if err != nil {
		h.Log.Warn("scrape from admin", "err", err, "result", res.String())
	}
	if err != nil && res.Fetched == 0 {
		web.Redirect(w, r, "/admin/queue", "scrape_failed")
		return
	}
	web.Redirect(w, r, "/admin/queue", "scanned")
}
