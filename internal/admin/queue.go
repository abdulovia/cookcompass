package admin

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/abdulovia/cookcompass/internal/app"
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
	data := map[string]any{"Recipes": pending, "Sources": h.App.RecipeSourceNames(), "InboxFiles": h.App.RecipeInboxFiles()}
	for k, v := range h.queueExtra(ctx) {
		data[k] = v
	}
	p := h.page(r, "admin.queue")
	p.Data = data
	h.R.Page(w, http.StatusOK, "admin/queue", p)
}

// datasetImport runs one dataset import from the queue page: an inbox file
// or an http(s) URL, capped by a limit, optionally auto-approving what it
// created.
func (h *Handler) datasetImport(w http.ResponseWriter, r *http.Request) {
	u := r.FormValue("url")
	if u != "" && !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		web.Redirect(w, r, "/admin/queue", "import_failed")
		return
	}
	limit := 200
	if n, err := strconv.Atoi(r.FormValue("limit")); err == nil && n > 0 && n <= 10000 {
		limit = n
	}
	opt := app.DatasetOptions{File: r.FormValue("file"), URL: u, Limit: limit,
		Approve: r.FormValue("approve") == "1"}
	res, err := h.App.ImportDataset(r.Context(), opt)
	if err != nil {
		h.Log.Warn("dataset import", "err", err, "result", res.String())
	}
	if res.Created == 0 && err != nil {
		web.Redirect(w, r, "/admin/queue", "import_failed")
		return
	}
	h.Log.Info("dataset import", "result", res.String())
	web.Redirect(w, r, "/admin/queue", "recipes_imported")
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
