package site

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/abdulovia/cookcompass/internal/ingredients"
	"github.com/abdulovia/cookcompass/internal/recipes"
	"github.com/abdulovia/cookcompass/internal/shops"
	"github.com/abdulovia/cookcompass/internal/users"
	"github.com/abdulovia/cookcompass/internal/web"
)

// favoriteToggle adds or removes the recipe from the user's favorites.
func (h *Handler) favoriteToggle(w http.ResponseWriter, r *http.Request) {
	on := r.FormValue("favorite") == "1"
	flash := "unfavorited"
	if on {
		flash = "favorited"
	}
	h.recipeAction(w, r, flash, func(prof *users.Profile, rec *recipes.Recipe) error {
		return h.App.Users.SetFavorite(r.Context(), prof.ID, rec.ID, on)
	})
}

// shoppingView is one rendered line of the shopping list.
type shoppingView struct {
	users.ShoppingItem
	RecipeName string
	RecipeSlug string
	BuyURL     string
	BuyExact   bool
}

// shoppingData assembles the rendered list.
func (h *Handler) shoppingData(r *http.Request, prof *users.Profile) ([]shoppingView, int, error) {
	ctx := r.Context()
	items, err := h.App.Users.ShoppingList(ctx, prof.ID)
	if err != nil {
		return nil, 0, err
	}
	byID, err := h.App.Ings.ByID(ctx)
	if err != nil {
		return nil, 0, err
	}
	lang := web.NewPage(r, "").Lang
	res := &shops.Resolver{Shops: h.App.Shops, ProductURLs: h.App.Catalog}
	recipesByID := map[int64]*recipes.Recipe{}
	views := make([]shoppingView, 0, len(items))
	for _, it := range items {
		v := shoppingView{ShoppingItem: it}
		if it.RecipeID != 0 {
			rec, ok := recipesByID[it.RecipeID]
			if !ok {
				if rec, err = h.App.Recipes.Get(ctx, it.RecipeID); err != nil {
					rec = nil // deleted recipe: the line stays useful without a name
				} else {
					recipesByID[it.RecipeID] = rec
				}
			}
			if rec != nil {
				v.RecipeName, v.RecipeSlug = rec.Name, rec.Slug
			}
		}
		query := it.Text
		if it.IngredientID != 0 {
			if ing, ok := byID[it.IngredientID]; ok {
				query = ing.LocalName(lang)
			}
		}
		if link, ok := res.Link(ctx, prof.CountryCode, prof.ShopID, it.IngredientID, query); ok {
			v.BuyURL, v.BuyExact = link.URL, link.Exact
		}
		views = append(views, v)
	}
	open := 0
	for _, v := range views {
		if !v.Bought {
			open++
		}
	}
	return views, open, nil
}

// list renders the shopping list page.
func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	prof := h.requireUser(w, r)
	if prof == nil {
		return
	}
	if r.Header.Get("HX-Request") == "true" && r.Header.Get("HX-Target") == "list-items" {
		h.listItems(w, r, prof, "")
		return
	}
	views, open, err := h.shoppingData(r, prof)
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	p := web.NewPage(r, "list.title")
	p.Data = map[string]any{"Items": views, "Open": open}
	h.R.Page(w, http.StatusOK, "pages/list", p)
}

// listItems renders just the #list-items block (HTMX swaps after an action).
func (h *Handler) listItems(w http.ResponseWriter, r *http.Request, prof *users.Profile, flash string) {
	views, open, err := h.shoppingData(r, prof)
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	p := web.NewPage(r, "list.title")
	data := map[string]any{"Items": views, "Open": open}
	if flash != "" {
		data["ActionFlash"] = "flash." + flash
	}
	p.Data = data
	h.R.Partial(w, http.StatusOK, "pages/list", "list_items", p)
}

// listDone finishes a list action: the refreshed block for HTMX, a redirect
// back to the page otherwise.
func (h *Handler) listDone(w http.ResponseWriter, r *http.Request, prof *users.Profile, flash string) {
	if r.Header.Get("HX-Request") == "true" {
		h.listItems(w, r, prof, flash)
		return
	}
	web.Redirect(w, r, "/list", flash)
}

// shoppingLines builds list rows from a recipe's ingredient lines, skipping
// what the user already has at home (pantry and staples).
func (h *Handler) shoppingLines(r *http.Request, rec *recipes.Recipe, prof *users.Profile) []users.ShoppingItem {
	byID, err := h.App.Ings.ByID(r.Context())
	if err != nil {
		byID = map[int64]ingredients.Ingredient{}
	}
	p := web.NewPage(r, "")
	var out []users.ShoppingItem
	for i, in := range rec.Ingredients {
		if in.IngredientID != 0 && (prof.Pantry[in.IngredientID] || byID[in.IngredientID].Staple) {
			continue
		}
		out = append(out, users.ShoppingItem{RecipeID: rec.ID, IngredientID: in.IngredientID,
			Text: DisplayLine(rec, i, p.Lang, h.units(r), byID, p.T)})
	}
	return out
}

// addShopping queues a recipe's missing ingredients.
func (h *Handler) addShopping(w http.ResponseWriter, r *http.Request) {
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
	if _, err := h.App.Users.AddShoppingItems(r.Context(), prof.ID, h.shoppingLines(r, rec, prof)); err != nil {
		h.Fail(w, r, err)
		return
	}
	web.Redirect(w, r, "/recipes/"+slug, "added_to_list")
}

// planShopping queues every ingredient of the current meal plan.
func (h *Handler) planShopping(w http.ResponseWriter, r *http.Request) {
	prof := h.requireUser(w, r)
	if prof == nil {
		return
	}
	plan, err := h.planFor(r, 0)
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	if plan == nil {
		web.Redirect(w, r, "/goals", "need_goal")
		return
	}
	var lines []users.ShoppingItem
	for _, item := range plan.Items {
		rec, err := h.App.Recipes.GetBySlug(r.Context(), item.Slug)
		if err != nil {
			continue
		}
		lines = append(lines, h.shoppingLines(r, rec, prof)...)
	}
	if _, err := h.App.Users.AddShoppingItems(r.Context(), prof.ID, lines); err != nil {
		h.Fail(w, r, err)
		return
	}
	web.Redirect(w, r, "/list", "added_to_list")
}

func (h *Handler) shoppingAction(w http.ResponseWriter, r *http.Request, flash string, fn func(userID, id int64) error) {
	prof := h.requireUser(w, r)
	if prof == nil {
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}
	if err := fn(prof.ID, id); errors.Is(err, users.ErrNotFound) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		h.Fail(w, r, err)
		return
	}
	h.listDone(w, r, prof, flash)
}

func (h *Handler) shoppingBought(w http.ResponseWriter, r *http.Request) {
	bought := r.FormValue("bought") != "0"
	h.shoppingAction(w, r, "", func(userID, id int64) error {
		return h.App.Users.SetShoppingBought(r.Context(), userID, id, bought)
	})
}

func (h *Handler) shoppingDelete(w http.ResponseWriter, r *http.Request) {
	h.shoppingAction(w, r, "", func(userID, id int64) error {
		return h.App.Users.DeleteShoppingItem(r.Context(), userID, id)
	})
}

// shoppingPantry marks the line's ingredient as "already at home" and drops
// the line.
func (h *Handler) shoppingPantry(w http.ResponseWriter, r *http.Request) {
	prof := h.requireUser(w, r)
	if prof == nil {
		return
	}
	ctx := r.Context()
	items, err := h.App.Users.ShoppingList(ctx, prof.ID)
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	var target *users.ShoppingItem
	for i := range items {
		if items[i].ID == id {
			target = &items[i]
			break
		}
	}
	if target == nil {
		http.NotFound(w, r)
		return
	}
	if target.IngredientID != 0 {
		pantry := prof.Pantry
		pantry[target.IngredientID] = true
		ids := make([]int64, 0, len(pantry))
		for ing := range pantry {
			ids = append(ids, ing)
		}
		if err := h.App.Users.SetPantry(ctx, prof.ID, ids); err != nil {
			h.Fail(w, r, err)
			return
		}
	}
	if err := h.App.Users.DeleteShoppingItem(ctx, prof.ID, id); err != nil {
		h.Fail(w, r, err)
		return
	}
	h.listDone(w, r, prof, "pantry_added")
}

func (h *Handler) shoppingClear(w http.ResponseWriter, r *http.Request) {
	prof := h.requireUser(w, r)
	if prof == nil {
		return
	}
	if err := h.App.Users.ClearBought(r.Context(), prof.ID); err != nil {
		h.Fail(w, r, err)
		return
	}
	h.listDone(w, r, prof, "cleared_bought")
}
