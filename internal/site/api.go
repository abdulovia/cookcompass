package site

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/abdulovia/cookcompass/internal/pool"
	"github.com/abdulovia/cookcompass/internal/recipes"
	"github.com/abdulovia/cookcompass/internal/web"
)

// APIRecipe is the public JSON shape of a recipe.
type APIRecipe struct {
	Slug           string            `json:"slug"`
	Name           string            `json:"name"`
	Description    string            `json:"description,omitempty"`
	Cuisine        string            `json:"cuisine"`
	MealTimes      []string          `json:"meal_times"`
	Difficulty     string            `json:"difficulty"`
	PrepMinutes    int               `json:"prep_minutes"`
	CookMinutes    int               `json:"cook_minutes"`
	Servings       int               `json:"servings"`
	VideoURL       string            `json:"video_url,omitempty"`
	Nutrition      recipes.Nutrition `json:"nutrition_per_serving"`
	Available      bool              `json:"available"`
	CookableAtHome bool              `json:"cookable_at_home"`
	Ingredients    []APIIngredient   `json:"ingredients,omitempty"`
	Steps          []string          `json:"steps,omitempty"`
}

// APIIngredient is one ingredient line in the API.
type APIIngredient struct {
	Text      string   `json:"text"`
	Name      string   `json:"name"`
	Quantity  *float64 `json:"quantity,omitempty"`
	Unit      string   `json:"unit,omitempty"`
	Essential bool     `json:"essential"`
	Status    string   `json:"status,omitempty"`
}

func toAPI(it pool.Item) APIRecipe {
	r := it.Recipe
	return APIRecipe{
		Slug: r.Slug, Name: r.Name, Description: r.Description, Cuisine: r.Cuisine, MealTimes: r.MealTimes,
		Difficulty: r.Difficulty, PrepMinutes: r.PrepMinutes, CookMinutes: r.CookMinutes, Servings: r.Servings,
		VideoURL: r.VideoURL, Nutrition: r.Nutrition, Available: it.Market.Available, CookableAtHome: it.Home.Available,
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

// apiList: GET /api/recipes?cuisine=&meal=&difficulty=&q=&cookable=1
func (h *Handler) apiList(w http.ResponseWriter, r *http.Request) {
	items, err := h.App.Pool.Browse(r.Context(), h.viewer(r), browseQuery(r))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	out := make([]APIRecipe, len(items))
	for i, it := range items {
		out[i] = toAPI(it)
	}
	writeJSON(w, http.StatusOK, map[string]any{"count": len(out), "recipes": out})
}

// apiDetail: GET /api/recipes/{slug}
func (h *Handler) apiDetail(w http.ResponseWriter, r *http.Request) {
	it, err := h.App.Pool.One(r.Context(), h.viewer(r), chi.URLParam(r, "slug"))
	if errors.Is(err, recipes.ErrNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	byID, err := h.App.Ings.ByID(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	p := web.NewPage(r, "")
	out := toAPI(*it)
	out.Steps = it.Recipe.Steps
	for i, in := range it.Base.Ingredients {
		ai := APIIngredient{Text: DisplayLine(it.Base, i, p.Lang, h.units(r), byID, p.T), Name: in.Name,
			Quantity: in.Quantity, Unit: in.Unit, Essential: in.Essential}
		if i < len(it.Market.Lines) {
			ai.Status = string(it.Market.Lines[i])
		}
		out.Ingredients = append(out.Ingredients, ai)
	}
	writeJSON(w, http.StatusOK, out)
}
