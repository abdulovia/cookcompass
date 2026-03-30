// Package site serves the public pages: home, browse, recipe detail,
// settings, goals, learning path and the read-only JSON API.
package site

import (
	"errors"
	"log/slog"
	"net/http"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/abdulovia/cookcompass/internal/app"
	"github.com/abdulovia/cookcompass/internal/availability"
	"github.com/abdulovia/cookcompass/internal/i18n"
	"github.com/abdulovia/cookcompass/internal/ingredients"
	"github.com/abdulovia/cookcompass/internal/pool"
	"github.com/abdulovia/cookcompass/internal/recipes"
	"github.com/abdulovia/cookcompass/internal/users"
	"github.com/abdulovia/cookcompass/internal/web"
)

// Handler serves public routes.
type Handler struct {
	App    *app.App
	R      *web.Renderer
	Log    *slog.Logger
	Bundle *i18n.Bundle
	// CountryAware enables hiding recipes whose ingredients aren't sold in
	// the viewer's country.
	CountryAware bool
}

// Routes registers public routes.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/", h.home)
	r.Get("/recipes", h.browse)
	r.Get("/recipes/{slug}", h.detail)
	r.Get("/goals", h.goals)
	r.Post("/goals", h.saveGoals)
	r.Get("/settings", h.settings)
	r.Post("/settings", h.saveSettings)
	r.Post("/settings/preferences", h.savePreferences)
	r.Post("/settings/pantry", h.savePantry)
	r.Post("/settings/locate", h.locate)
	r.Post("/settings/rescan", h.rescan)
	r.Get("/api/recipes", h.apiList)
	r.Get("/api/recipes/{slug}", h.apiDetail)
	r.Get("/login", h.loginForm)
	r.Post("/login", h.login)
	r.Post("/register", h.register)
	r.Post("/logout", h.logout)
}

// NotFound renders the 404 page.
func (h *Handler) NotFound(w http.ResponseWriter, r *http.Request) {
	p := web.NewPage(r, "error.not_found_title")
	h.R.Page(w, http.StatusNotFound, "pages/404", p)
}

// Fail logs err and renders the 500 page.
func (h *Handler) Fail(w http.ResponseWriter, r *http.Request, err error) {
	h.Log.Error("request failed", "path", r.URL.Path, "err", err)
	p := web.NewPage(r, "error.server_title")
	h.R.Page(w, http.StatusInternalServerError, "pages/500", p)
}

// viewer builds the pool viewer for the current request.
func (h *Handler) viewer(r *http.Request) pool.Viewer {
	p := web.Profile(r.Context())
	v := pool.Viewer{Locale: web.NewPage(r, "").Lang, Pantry: map[int64]bool{}}
	if p == nil {
		return v
	}
	if h.CountryAware {
		v.Country = p.CountryCode
		v.ShowUnavailable = p.ShowUnavailable
	}
	v.Pantry = p.Pantry
	for id, kind := range p.Prefs {
		if kind == users.Allergy {
			v.Allergies = append(v.Allergies, id)
		} else {
			v.Dislikes = append(v.Dislikes, id)
		}
	}
	sort.Slice(v.Allergies, func(i, j int) bool { return v.Allergies[i] < v.Allergies[j] })
	return v
}

func (h *Handler) units(r *http.Request) string {
	if p := web.Profile(r.Context()); p != nil && p.Units == recipes.Imperial {
		return recipes.Imperial
	}
	return recipes.Metric
}

// browseQuery reads browse filters. "hide disliked" defaults to on until
// the filter form has been submitted once (f=1).
func browseQuery(r *http.Request) pool.Query {
	q := r.URL.Query()
	submitted := q.Get("f") == "1"
	return pool.Query{
		Cuisine:      q.Get("cuisine"),
		MealTime:     oneOf(q.Get("meal"), recipes.MealTimes),
		Difficulty:   oneOf(q.Get("difficulty"), recipes.Difficulties),
		Text:         strings.TrimSpace(q.Get("q")),
		OnlyCookable: q.Get("cookable") == "1",
		HideDisliked: q.Get("hide_disliked") == "1" || !submitted,
	}
}

func oneOf(v string, set []string) string {
	for _, s := range set {
		if s == v {
			return v
		}
	}
	return ""
}

func (h *Handler) home(w http.ResponseWriter, r *http.Request) {
	items, err := h.App.Pool.Visible(r.Context(), h.viewer(r))
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	// "Start here": the quickest easy recipes.
	var easy []pool.Item
	for _, it := range items {
		if it.Base.Difficulty == recipes.Easy {
			easy = append(easy, it)
		}
	}
	sort.SliceStable(easy, func(i, j int) bool { return easy[i].Base.TotalMinutes() < easy[j].Base.TotalMinutes() })
	if len(easy) > 3 {
		easy = easy[:3]
	}
	cuisines, err := h.App.Recipes.Cuisines(r.Context())
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	plan, err := h.planFor(r, 0)
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	p := web.NewPage(r, "")
	data := map[string]any{"StartHere": easy, "Cuisines": cuisines, "Count": len(items), "Plan": plan}
	for k, v := range h.homeExtra(r) {
		data[k] = v
	}
	p.Data = data
	h.R.Page(w, http.StatusOK, "pages/home", p)
}

// homeExtra is extended by later features (learning progress).
func (h *Handler) homeExtra(_ *http.Request) map[string]any { return nil }

func (h *Handler) browse(w http.ResponseWriter, r *http.Request) {
	v := h.viewer(r)
	q := browseQuery(r)
	items, err := h.App.Pool.Browse(r.Context(), v, q)
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	cuisines, err := h.App.Recipes.Cuisines(r.Context())
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	p := web.NewPage(r, "browse.title")
	prof := web.Profile(r.Context())
	p.Data = map[string]any{
		"Items": items, "Q": q, "Cuisines": cuisines, "MealTimes": recipes.MealTimes, "Difficulties": recipes.Difficulties,
		"HasPantry": prof != nil && len(prof.Pantry) > 0, "HasDislikes": len(v.Dislikes) > 0, "CountryAware": v.Country != "",
	}
	if r.Header.Get("HX-Request") == "true" && r.Header.Get("HX-Target") == "results" {
		h.R.Partial(w, http.StatusOK, "pages/browse", "results", p)
		return
	}
	h.R.Page(w, http.StatusOK, "pages/browse", p)
}

// IngredientView is one rendered ingredient line on the detail page.
type IngredientView struct {
	Text     string
	Status   availability.Status
	AtHome   bool
	Optional bool
	SubNote  string
}

func (h *Handler) detail(w http.ResponseWriter, r *http.Request) {
	v := h.viewer(r)
	it, err := h.App.Pool.One(r.Context(), v, chi.URLParam(r, "slug"))
	if errors.Is(err, recipes.ErrNotFound) {
		h.NotFound(w, r)
		return
	}
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	byID, err := h.App.Ings.ByID(r.Context())
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	p := web.NewPage(r, "")
	p.Title = it.Recipe.Name
	lines := h.ingredientViews(p, it, byID, h.units(r))
	embed, _ := recipes.EmbedURL(it.Base.VideoURL)
	data := map[string]any{"Item": it, "Lines": lines, "Embed": embed, "CountryAware": v.Country != ""}
	for k, val := range h.detailExtra(r, it) {
		data[k] = val
	}
	p.Data = data
	h.R.Page(w, http.StatusOK, "pages/recipe", p)
}

// detailExtra is extended by later features (cooked/learned state).
func (h *Handler) detailExtra(_ *http.Request, _ *pool.Item) map[string]any { return nil }

func (h *Handler) ingredientViews(p *web.Page, it *pool.Item, byID map[int64]ingredients.Ingredient, units string) []IngredientView {
	subs := map[int]availability.Substitution{}
	for _, s := range it.Market.Substitutions {
		subs[s.Line] = s
	}
	out := make([]IngredientView, len(it.Base.Ingredients))
	for i, in := range it.Base.Ingredients {
		iv := IngredientView{
			Text:     DisplayLine(it.Base, i, p.Lang, units, byID, p.T),
			Optional: !in.Essential,
			AtHome:   in.IngredientID != 0 && (it.Home.Lines[i] == availability.Have || it.Home.Lines[i] == availability.Staple),
		}
		if i < len(it.Market.Lines) {
			iv.Status = it.Market.Lines[i]
		}
		if s, ok := subs[i]; ok {
			iv.SubNote = p.T("availability.sub_note", "Missing", byID[s.Missing].LocalName(p.Lang),
				"Sub", byID[s.Substitute].LocalName(p.Lang), "Ratio", s.Ratio)
		}
		out[i] = iv
	}
	return out
}

// DisplayLine renders ingredient i of r for a locale and unit system. A
// hand-written translation line wins; otherwise the line is composed from
// the parsed quantity, converted unit and localized ingredient name.
func DisplayLine(r *recipes.Recipe, i int, locale, units string, byID map[int64]ingredients.Ingredient,
	t func(string, ...any) string) string {
	if locale != recipes.BaseLocale {
		if line := r.IngredientLine(locale, i); line != "" {
			return line
		}
	}
	in := r.Ingredients[i]
	name := in.Name
	if locale != recipes.BaseLocale && in.IngredientID != 0 {
		if ing, ok := byID[in.IngredientID]; ok {
			name = ing.LocalName(locale)
		}
	}
	var parts []string
	if in.Quantity != nil {
		q, unit := recipes.ConvertUnits(*in.Quantity, in.Unit, units)
		parts = append(parts, recipes.FormatQuantity(q))
		if unit != "" && unit != "to taste" {
			parts = append(parts, t("unit."+strings.ReplaceAll(unit, " ", "_"), "Count", q))
		}
	}
	parts = append(parts, name)
	line := strings.Join(parts, " ")
	if in.Unit == "to taste" {
		line += ", " + t("unit.to_taste")
	} else if in.Note != "" && locale == recipes.BaseLocale {
		line += ", " + in.Note
	}
	return line
}
