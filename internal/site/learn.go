package site

import (
	"errors"
	"net/http"
	"sort"

	"github.com/go-chi/chi/v5"

	"github.com/abdulovia/cookcompass/internal/learning"
	"github.com/abdulovia/cookcompass/internal/pool"
	"github.com/abdulovia/cookcompass/internal/recipes"
	"github.com/abdulovia/cookcompass/internal/users"
	"github.com/abdulovia/cookcompass/internal/web"
)

// LearnedRecipe pairs a learned recipe with its localized view.
type LearnedRecipe struct {
	Recipe *recipes.Recipe
	Cooked int
}

// CookedView is a history entry with its recipe.
type CookedView struct {
	users.CookEvent
	Recipe *recipes.Recipe
}

// progress computes the learning path for the current user.
func (h *Handler) progress(r *http.Request) ([]learning.Level, map[int64]bool, map[int64]int, error) {
	prof := web.Profile(r.Context())
	if prof == nil || prof.ID == 0 {
		return learning.Path(nil), map[int64]bool{}, map[int64]int{}, nil
	}
	learned, err := h.App.Users.Learned(r.Context(), prof.ID)
	if err != nil {
		return nil, nil, nil, err
	}
	cooked, err := h.App.Users.CookedCounts(r.Context(), prof.ID)
	if err != nil {
		return nil, nil, nil, err
	}
	ids := make([]int64, 0, len(learned))
	set := map[int64]bool{}
	for id := range learned {
		ids = append(ids, id)
		set[id] = true
	}
	counts := map[string]int{}
	if len(ids) > 0 {
		list, err := h.App.Recipes.List(r.Context(), recipes.Filter{IDs: ids, Deleted: recipes.DeletedInclude,
			Statuses: []string{recipes.StatusApproved, recipes.StatusPending, recipes.StatusRejected}})
		if err != nil {
			return nil, nil, nil, err
		}
		for _, rec := range list {
			counts[rec.Difficulty]++
		}
	}
	return learning.Path(counts), set, cooked, nil
}

func (h *Handler) learn(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	levels, learned, cooked, err := h.progress(r)
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	lang := web.NewPage(r, "").Lang
	cur := learning.Current(levels)
	var next []pool.Item
	if cur != nil {
		items, err := h.App.Pool.Visible(ctx, h.viewer(r))
		if err != nil {
			h.Fail(w, r, err)
			return
		}
		for _, it := range items {
			if it.Base.Difficulty == cur.Difficulty && !learned[it.Base.ID] {
				next = append(next, it)
			}
		}
		sort.SliceStable(next, func(i, j int) bool {
			// Recipes already cooked once come first: they are closest to "learned".
			ci, cj := cooked[next[i].Base.ID] > 0, cooked[next[j].Base.ID] > 0
			if ci != cj {
				return ci
			}
			return next[i].Base.TotalMinutes() < next[j].Base.TotalMinutes()
		})
		if len(next) > 3 {
			next = next[:3]
		}
	}
	// Learned recipes and recent history (soft-deleted recipes still show: history survives).
	ids := []int64{}
	for id := range learned {
		ids = append(ids, id)
	}
	var recent []users.CookEvent
	if prof := web.Profile(ctx); prof != nil && prof.ID != 0 {
		if recent, err = h.App.Users.RecentCooked(ctx, prof.ID, 10); err != nil {
			h.Fail(w, r, err)
			return
		}
	}
	for _, e := range recent {
		ids = append(ids, e.RecipeID)
	}
	byID := map[int64]*recipes.Recipe{}
	if len(ids) > 0 {
		list, err := h.App.Recipes.List(ctx, recipes.Filter{IDs: ids, Deleted: recipes.DeletedInclude,
			Statuses: []string{recipes.StatusApproved, recipes.StatusPending, recipes.StatusRejected}})
		if err != nil {
			h.Fail(w, r, err)
			return
		}
		for _, rec := range list {
			byID[rec.ID] = rec.Localized(lang)
		}
	}
	learnedBy := map[string][]LearnedRecipe{}
	for id := range learned {
		if rec, ok := byID[id]; ok {
			learnedBy[rec.Difficulty] = append(learnedBy[rec.Difficulty], LearnedRecipe{Recipe: rec, Cooked: cooked[id]})
		}
	}
	for _, l := range learnedBy {
		sort.Slice(l, func(i, j int) bool { return l[i].Recipe.Name < l[j].Recipe.Name })
	}
	var history []CookedView
	for _, e := range recent {
		if rec, ok := byID[e.RecipeID]; ok {
			history = append(history, CookedView{CookEvent: e, Recipe: rec})
		}
	}
	done, goal := learning.Total(levels)
	p := web.NewPage(r, "learn.title")
	p.Data = map[string]any{"Levels": levels, "Current": cur, "Next": next, "LearnedBy": learnedBy, "History": history,
		"Done": done, "Goal": goal}
	h.R.Page(w, http.StatusOK, "pages/learn", p)
}

// cooked logs "I cooked this".
func (h *Handler) cooked(w http.ResponseWriter, r *http.Request) {
	h.recipeAction(w, r, "cooked", func(prof *users.Profile, rec *recipes.Recipe) error {
		return h.App.Users.LogCooked(r.Context(), prof.ID, rec.ID)
	})
}

// learnedToggle marks or unmarks a recipe as learned.
func (h *Handler) learnedToggle(w http.ResponseWriter, r *http.Request) {
	on := r.FormValue("learned") == "1"
	flash := "unlearned"
	if on {
		flash = "learned"
	}
	h.recipeAction(w, r, flash, func(prof *users.Profile, rec *recipes.Recipe) error {
		return h.App.Users.SetLearned(r.Context(), prof.ID, rec.ID, on)
	})
}

func (h *Handler) recipeAction(w http.ResponseWriter, r *http.Request, flash string, fn func(*users.Profile, *recipes.Recipe) error) {
	prof := h.requireUser(w, r)
	if prof == nil {
		return
	}
	slug := chi.URLParam(r, "slug")
	rec, err := h.App.Recipes.GetBySlug(r.Context(), slug)
	if errors.Is(err, recipes.ErrNotFound) || (err == nil && (rec.Deleted() || rec.Status != recipes.StatusApproved)) {
		h.NotFound(w, r)
		return
	}
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	if err := fn(prof, rec); err != nil {
		h.Fail(w, r, err)
		return
	}
	if r.Header.Get("HX-Request") == "true" {
		h.renderDetail(w, r, slug, "recipe_actions", flash)
		return
	}
	web.Redirect(w, r, "/recipes/"+slug+"#actions", flash)
}

// detailExtra adds the user's history with this recipe.
func (h *Handler) detailExtra(r *http.Request, it *pool.Item) map[string]any {
	prof := web.Profile(r.Context())
	if prof == nil || prof.ID == 0 {
		return map[string]any{"CanTrack": false}
	}
	cooked, err := h.App.Users.CookedCounts(r.Context(), prof.ID)
	if err != nil {
		h.Log.Error("cooked counts", "err", err)
	}
	learned, err := h.App.Users.Learned(r.Context(), prof.ID)
	if err != nil {
		h.Log.Error("learned", "err", err)
	}
	_, isLearned := learned[it.Base.ID]
	return map[string]any{"CanTrack": true, "Cooked": cooked[it.Base.ID], "Learned": isLearned}
}

// homeExtra adds learning progress to the home page.
func (h *Handler) homeExtra(r *http.Request) map[string]any {
	levels, _, _, err := h.progress(r)
	if err != nil {
		h.Log.Error("progress", "err", err)
		return nil
	}
	done, goal := learning.Total(levels)
	return map[string]any{"Levels": levels, "Current": learning.Current(levels), "Done": done, "Goal": goal}
}
