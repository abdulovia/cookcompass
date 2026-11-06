// Package site serves the public pages: home, browse, recipe detail,
// settings, goals, learning path and the read-only JSON API.
package site

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/abdulovia/cookcompass/internal/app"
	"github.com/abdulovia/cookcompass/internal/availability"
	"github.com/abdulovia/cookcompass/internal/content"
	"github.com/abdulovia/cookcompass/internal/i18n"
	"github.com/abdulovia/cookcompass/internal/ingredients"
	"github.com/abdulovia/cookcompass/internal/pool"
	"github.com/abdulovia/cookcompass/internal/recipes"
	"github.com/abdulovia/cookcompass/internal/shops"
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

// Routes registers public routes. Areas switched off in site.yaml are not
// mounted at all (404), matching the navigation in web.PublicNav.
func (h *Handler) Routes(r chi.Router) {
	on := h.App.Site.On
	r.Get("/", h.home)
	r.Get("/recipes", h.browse)
	r.Get("/recipes/{slug}", h.detail)
	r.Post("/recipes/{slug}/cooked", h.cooked)
	if on(content.FeatureLearning) {
		r.Post("/recipes/{slug}/learned", h.learnedToggle)
		r.Get("/learn", h.learn)
	}
	if on(content.FeatureFavorites) {
		r.Post("/recipes/{slug}/favorite", h.favoriteToggle)
	}
	if on(content.FeatureShoppingList) {
		r.Post("/recipes/{slug}/shopping", h.addShopping)
		r.Get("/list", h.list)
		r.Post("/list/clear", h.shoppingClear)
		r.Post("/list/{id}/bought", h.shoppingBought)
		r.Post("/list/{id}/delete", h.shoppingDelete)
		r.Post("/list/{id}/pantry", h.shoppingPantry)
	}
	if on(content.FeatureGoals) {
		r.Get("/goals", h.goals)
		r.Post("/goals", h.saveGoals)
		if on(content.FeatureShoppingList) {
			r.Post("/goals/shopping", h.planShopping)
		}
	}
	r.Get("/settings", h.settings)
	r.Get("/settings/places", h.places)
	r.Post("/settings", h.saveSettings)
	r.Post("/settings/preferences", h.savePreferences)
	r.Post("/settings/pantry", h.savePantry)
	if on(content.FeatureGeolocation) {
		r.Post("/settings/locate", h.locate)
	}
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
		v.ShopID = p.ShopID
		v.StrictShop = p.StrictShop
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

// favoritesOnly reports whether the browse view is the favorites tab.
func favoritesOnly(r *http.Request) bool { return r.URL.Query().Get("fav") == "1" }

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
	cuisines, err := h.cuisinesIn(r.Context(), items, "")
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

func (h *Handler) browse(w http.ResponseWriter, r *http.Request) {
	v := h.viewer(r)
	q := browseQuery(r)
	items, err := h.App.Pool.Browse(r.Context(), v, q)
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	favoritesTab := favoritesOnly(r)
	if favoritesTab {
		prof := web.Profile(r.Context())
		favs, err := h.App.Users.Favorites(r.Context(), prof.ID)
		if err != nil {
			h.Fail(w, r, err)
			return
		}
		filtered := make([]pool.Item, 0, len(items))
		for _, it := range items {
			if favs[it.Base.ID] {
				filtered = append(filtered, it)
			}
		}
		items = filtered
	}
	visible, err := h.App.Pool.Visible(r.Context(), v)
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	cuisines, err := h.cuisinesIn(r.Context(), visible, q.Cuisine)
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	p := web.NewPage(r, "browse.title")
	prof := web.Profile(r.Context())
	p.Data = map[string]any{
		"Items": items, "Q": q, "Cuisines": cuisines,
		"HasPantry": prof != nil && len(prof.Pantry) > 0, "HasDislikes": len(v.Dislikes) > 0, "CountryAware": v.Country != "",
		"FavoritesTab": favoritesTab, "CanTrack": prof != nil && prof.ID != 0,
	}
	if r.Header.Get("HX-Request") == "true" && r.Header.Get("HX-Target") == "results" {
		h.R.Partial(w, http.StatusOK, "pages/browse", "results", p)
		return
	}
	h.R.Page(w, http.StatusOK, "pages/browse", p)
}

// cuisinesIn lists the cuisines that have at least one of items (plus keep,
// the currently selected filter), so imports that create many cuisines
// don't flood the filters with empty choices.
func (h *Handler) cuisinesIn(ctx context.Context, items []pool.Item, keep string) ([]recipes.Cuisine, error) {
	all, err := h.App.Recipes.Cuisines(ctx)
	if err != nil {
		return nil, err
	}
	used := map[string]bool{keep: keep != ""}
	for _, it := range items {
		used[it.Base.Cuisine] = true
	}
	out := make([]recipes.Cuisine, 0, len(all))
	for _, c := range all {
		if used[c.Slug] {
			out = append(out, c)
		}
	}
	return out, nil
}

// IngredientView is one rendered ingredient line on the detail page.
type IngredientView struct {
	Text     string
	Status   availability.Status
	AtHome   bool
	Optional bool
	SubNote  string
	BuyURL   string // direct product page or shop search
	BuyExact bool   // true = exact product page
	BuyShop  string // shop name for labels
	// Serving-scaler hints for composed lines (empty for hand-written ones):
	// base quantity, text after the unit, unit label per plural sample.
	Qty   string
	Rest  string
	Units string
}

func (h *Handler) detail(w http.ResponseWriter, r *http.Request) {
	h.renderDetail(w, r, chi.URLParam(r, "slug"), "", "")
}

// renderDetail renders the recipe page, or one block of it for HTMX swaps.
func (h *Handler) renderDetail(w http.ResponseWriter, r *http.Request, slug, block, flash string) {
	v := h.viewer(r)
	it, err := h.App.Pool.One(r.Context(), v, slug)
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

	lines := h.ingredientViews(r.Context(), p, it, byID, h.units(r))
	embed, _ := recipes.EmbedURL(it.Base.VideoURL)
	data := map[string]any{"Item": it, "Lines": lines, "Embed": embed, "CountryAware": v.Country != ""}
	if flash != "" {
		// Only HTMX swaps of the actions card pass a flash; full page loads
		// show ?flash= once, in the layout.
		data["ActionFlash"] = "flash." + flash
	}
	for k, val := range h.detailExtra(r, it) {
		data[k] = val
	}
	p.Data = data
	if block != "" {
		h.R.Partial(w, http.StatusOK, "pages/recipe", block, p)
		return
	}
	h.R.Page(w, http.StatusOK, "pages/recipe", p)
}

func (h *Handler) ingredientViews(ctx context.Context, p *web.Page, it *pool.Item, byID map[int64]ingredients.Ingredient, units string) []IngredientView {
	subs := map[int]availability.Substitution{}
	for _, s := range it.Market.Substitutions {
		subs[s.Line] = s
	}
	prof := web.Profile(ctx)
	res := &shops.Resolver{Shops: h.App.Shops, ProductURLs: h.App.Catalog}
	country, shopID := "", int64(0)
	if prof != nil {
		country, shopID = prof.CountryCode, prof.ShopID
	}
	out := make([]IngredientView, len(it.Base.Ingredients))
	for i, in := range it.Base.Ingredients {
		lp := composeLine(it.Base, i, p.Lang, units, byID, p.T)
		iv := IngredientView{
			Text:     lp.Text,
			Optional: !in.Essential,
			AtHome:   in.IngredientID != 0 && (it.Home.Lines[i] == availability.Have || it.Home.Lines[i] == availability.Staple),
		}
		if lp.Qty != nil {
			iv.Qty = strconv.FormatFloat(*lp.Qty, 'f', -1, 64)
			iv.Rest, iv.Units = lp.Rest, unitForms(lp.UnitKey, p.T)
		}
		if i < len(it.Market.Lines) {
			iv.Status = it.Market.Lines[i]
		}
		if s, ok := subs[i]; ok {
			iv.SubNote = p.T("availability.sub_note", "Missing", byID[s.Missing].LocalName(p.Lang),
				"Sub", byID[s.Substitute].LocalName(p.Lang), "Ratio", s.Ratio)
		}
		// Buy link: exact product page when the catalog has one, otherwise a
		// search in the user's shop. The query is the localized ingredient
		// name, falling back to the line's own name for unlinked ingredients.
		query := in.Name
		if in.IngredientID != 0 {
			if ing, ok := byID[in.IngredientID]; ok {
				query = ing.LocalName(p.Lang)
			}
		}
		if link, ok := res.Link(ctx, country, shopID, in.IngredientID, query); ok {
			iv.BuyURL, iv.BuyExact, iv.BuyShop = link.URL, link.Exact, link.Shop
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
	return composeLine(r, i, locale, units, byID, t).Text
}

// lineParts is a rendered ingredient line plus what the serving scaler
// needs to recompute it: quantity, unit message id and the text after the
// unit. Qty is nil for hand-written translations and unquantified lines.
type lineParts struct {
	Text    string
	Qty     *float64
	UnitKey string // "unit.clove", or "" when the line has no unit label
	Rest    string // name and note, as rendered after the unit
}

func composeLine(r *recipes.Recipe, i int, locale, units string, byID map[int64]ingredients.Ingredient,
	t func(string, ...any) string) lineParts {
	if locale != recipes.BaseLocale {
		if line := r.IngredientLine(locale, i); line != "" {
			return lineParts{Text: line}
		}
	}
	in := r.Ingredients[i]
	rest := in.Name
	if locale != recipes.BaseLocale && in.IngredientID != 0 {
		if ing, ok := byID[in.IngredientID]; ok {
			rest = ing.LocalName(locale)
		}
	}
	if in.Unit == "to taste" {
		rest += ", " + t("unit.to_taste")
	} else if in.Note != "" && locale == recipes.BaseLocale {
		rest += ", " + in.Note
	}
	if in.Quantity == nil {
		return lineParts{Text: rest, Rest: rest}
	}
	q, unit := recipes.ConvertUnits(*in.Quantity, in.Unit, units)
	lp := lineParts{Qty: &q, Rest: rest}
	parts := []string{recipes.FormatQuantity(q)}
	if unit != "" && unit != "to taste" {
		lp.UnitKey = "unit." + strings.ReplaceAll(unit, " ", "_")
		parts = append(parts, t(lp.UnitKey, "Count", q))
	}
	lp.Text = strings.Join(append(parts, rest), " ")
	return lp
}

// pluralSamples cover every CLDR category of the shipped locales (one, few,
// many, other — including fractions and French/Spanish "many"); the client
// picks the sample whose Intl.PluralRules category matches the scaled count.
var pluralSamples = []float64{1, 2, 5, 21, 1.5, 0.5, 1000000}

// unitForms renders a unit label for each plural sample, keyed by sample.
func unitForms(key string, t func(string, ...any) string) string {
	if key == "" {
		return ""
	}
	forms := make(map[string]string, len(pluralSamples))
	for _, n := range pluralSamples {
		forms[strconv.FormatFloat(n, 'f', -1, 64)] = t(key, "Count", n)
	}
	b, _ := json.Marshal(forms)
	return string(b)
}
