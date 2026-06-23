package admin

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/abdulovia/cookcompass/internal/recipes"
	"github.com/abdulovia/cookcompass/internal/web"
)

// NutritionFields are the per-serving nutrition inputs, in display order.
var NutritionFields = []string{"calories", "protein", "fat", "carbs", "fiber", "sugar", "sodium", "saturated_fat"}

type translationForm struct {
	Locale      string
	Name        string
	Description string
	Steps       string
	Ingredients string
}

type recipeForm struct {
	ID           int64
	Slug         string
	Name         string
	Description  string
	Cuisine      string
	MealTimes    []string
	Difficulty   string
	Prep         string
	Cook         string
	Servings     string
	VideoURL     string
	ImageURL     string
	Nutrition    map[string]string
	Ingredients  string
	Steps        string
	Translations []translationForm
	Source       string
	Status       string
	Deleted      bool
}

// cacheImage mirrors one recipe's remote image into storage and points the
// recipe at the local /media proxy.
func (h *Handler) cacheImage(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	rec, err := h.App.Recipes.Get(r.Context(), id)
	if errors.Is(err, recipes.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		h.fail(w, r, err)
		return
	}
	if err := h.App.Media.CacheRecipeImage(r.Context(), rec); err != nil {
		h.Log.Warn("cache image", "recipe", rec.Slug, "err", err)
		web.Redirect(w, r, fmt.Sprintf("/admin/recipes/%d/edit", id), "media_failed")
		return
	}
	web.Redirect(w, r, fmt.Sprintf("/admin/recipes/%d/edit", id), "media_cached")
}

// translateRecipe drafts a machine translation for one locale and stores it
// flagged for review.
func (h *Handler) translateRecipe(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	locale := r.FormValue("locale")
	rec, err := h.App.Recipes.Get(r.Context(), id)
	if errors.Is(err, recipes.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		h.fail(w, r, err)
		return
	}
	if err := h.App.TranslateRecipeDraft(r.Context(), rec, locale); err != nil {
		h.Log.Warn("translate draft", "recipe", rec.Slug, "locale", locale, "err", err)
		web.Redirect(w, r, fmt.Sprintf("/admin/recipes/%d/edit", id), "translate_failed")
		return
	}
	web.Redirect(w, r, fmt.Sprintf("/admin/recipes/%d/edit", id), "translated")
}

// cacheAllImages mirrors every remote recipe image into storage (queue page
// bulk action).
func (h *Handler) cacheAllImages(w http.ResponseWriter, r *http.Request) {
	n, err := h.App.Media.CacheImages(r.Context())
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.Log.Info("cached recipe images", "count", n)
	web.Redirect(w, r, "/admin/queue", "media_cached")
}

func (h *Handler) recipeList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := recipes.Filter{Query: q.Get("q"), Cuisine: q.Get("cuisine")}
	switch q.Get("status") {
	case "", "all":
		f.Statuses = []string{recipes.StatusApproved, recipes.StatusPending, recipes.StatusRejected}
	default:
		f.Statuses = []string{q.Get("status")}
	}
	switch q.Get("deleted") {
	case "only":
		f.Deleted = recipes.DeletedOnly
	case "include":
		f.Deleted = recipes.DeletedInclude
	}
	if q.Get("review") == "1" {
		f.NeedsReview = true
	}
	list, err := h.App.Recipes.List(r.Context(), f)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	p := h.page(r, "admin.recipes")
	p.Data = map[string]any{"Recipes": list}
	h.R.Page(w, http.StatusOK, "admin/recipes", p)
}

func (h *Handler) recipeNew(w http.ResponseWriter, r *http.Request) {
	form := recipeForm{Servings: "2", Difficulty: recipes.Easy, Nutrition: map[string]string{}}
	for _, loc := range h.ContentLocales {
		form.Translations = append(form.Translations, translationForm{Locale: loc})
	}
	h.renderForm(w, r, http.StatusOK, form, nil)
}

func (h *Handler) recipeEdit(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	rec, err := h.App.Recipes.Get(r.Context(), id)
	if errors.Is(err, recipes.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.renderForm(w, r, http.StatusOK, h.formFromRecipe(rec), nil)
}

func (h *Handler) renderForm(w http.ResponseWriter, r *http.Request, status int, form recipeForm, errs map[string]string) {
	cuisines, err := h.App.Recipes.Cuisines(r.Context())
	if err != nil {
		h.fail(w, r, err)
		return
	}
	title := "admin.recipe_new"
	if form.ID != 0 {
		title = "admin.recipe_edit"
	}
	p := h.page(r, title)
	p.Errors = errs
	p.Data = map[string]any{
		"Form": form, "Cuisines": cuisines, "MealTimes": recipes.MealTimes,
		"Difficulties": recipes.Difficulties, "NutritionFields": NutritionFields,
		"StorageEnabled":    h.App.Media.Enabled(),
		"TranslatorEnabled": h.App.Translator() != nil,
	}
	h.R.Page(w, status, "admin/recipe_form", p)
}

func (h *Handler) recipeCreate(w http.ResponseWriter, r *http.Request) {
	form, rec, errs := h.parseForm(r)
	if len(errs) == 0 {
		rec.Source = recipes.SourceUser
		err := h.App.Recipes.Create(r.Context(), rec, recipes.Change{Source: recipes.SourceUser, Note: "created in admin"})
		if ok := h.collect(w, r, err, errs); !ok {
			return
		}
		if len(errs) == 0 {
			web.Redirect(w, r, fmt.Sprintf("/admin/recipes/%d/edit", rec.ID), "created")
			return
		}
	}
	h.renderForm(w, r, http.StatusUnprocessableEntity, form, errs)
}

func (h *Handler) recipeUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	existing, err := h.App.Recipes.Get(r.Context(), id)
	if errors.Is(err, recipes.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		h.fail(w, r, err)
		return
	}
	form, rec, errs := h.parseForm(r)
	form.ID, form.Slug = existing.ID, existing.Slug
	if len(errs) == 0 {
		rec.ID = existing.ID
		rec.Slug = existing.Slug
		rec.Status = existing.Status
		rec.SourceURL = existing.SourceURL
		rec.CanonicalURL = existing.CanonicalURL
		// User edits are authoritative from now on: re-scrapes won't overwrite them.
		rec.Source = recipes.SourceUser
		keepIngredientLinks(rec, existing)
		mergeTranslations(rec, existing)
		err := h.App.Recipes.Update(r.Context(), rec, recipes.Change{Source: recipes.SourceUser, Note: "edited in admin"})
		if ok := h.collect(w, r, err, errs); !ok {
			return
		}
		if len(errs) == 0 {
			web.Redirect(w, r, fmt.Sprintf("/admin/recipes/%d/edit", rec.ID), "saved")
			return
		}
	}
	h.renderForm(w, r, http.StatusUnprocessableEntity, form, errs)
}

// collect merges a store error into errs; returns false if it already
// responded (unexpected error).
func (h *Handler) collect(w http.ResponseWriter, r *http.Request, err error, errs map[string]string) bool {
	if err == nil {
		return true
	}
	var ve recipes.ValidationError
	if errors.As(err, &ve) {
		for k, v := range ve {
			errs[k] = v
		}
		return true
	}
	h.fail(w, r, err)
	return false
}

func (h *Handler) recipeDelete(w http.ResponseWriter, r *http.Request) {
	h.lifecycle(w, r, h.App.Recipes.SoftDelete, "deleted")
}

func (h *Handler) recipeRestore(w http.ResponseWriter, r *http.Request) {
	h.lifecycle(w, r, h.App.Recipes.Restore, "restored")
}

func (h *Handler) lifecycle(w http.ResponseWriter, r *http.Request, fn func(ctx context.Context, id int64, ch recipes.Change) error, flash string) {
	id, err := idParam(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	err = fn(r.Context(), id, recipes.Change{Source: recipes.SourceUser})
	if errors.Is(err, recipes.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		h.fail(w, r, err)
		return
	}
	back := r.FormValue("back")
	if !strings.HasPrefix(back, "/admin/") {
		back = "/admin/recipes"
	}
	web.Redirect(w, r, back, flash)
}

func (h *Handler) recipeRevisions(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	rec, err := h.App.Recipes.Get(r.Context(), id)
	if errors.Is(err, recipes.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		h.fail(w, r, err)
		return
	}
	revs, err := h.App.Recipes.Revisions(r.Context(), id)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	byRev := map[int]recipes.Revision{}
	for _, rv := range revs {
		byRev[rv.Rev] = rv
	}
	// Default: newest revision against the one before it.
	var a, b recipes.Revision
	if len(revs) > 0 {
		b = revs[0]
		if len(revs) > 1 {
			a = revs[1]
		}
	}
	if n, err := strconv.Atoi(r.URL.Query().Get("b")); err == nil {
		if rv, ok := byRev[n]; ok {
			b = rv
		}
	}
	if n, err := strconv.Atoi(r.URL.Query().Get("a")); err == nil {
		if rv, ok := byRev[n]; ok {
			a = rv
		}
	}
	p := h.page(r, "admin.revisions")
	p.Data = map[string]any{"Recipe": rec, "Revisions": revs, "A": a, "B": b, "Diff": recipes.Diff(a.Snapshot, b.Snapshot)}
	h.R.Page(w, http.StatusOK, "admin/revisions", p)
}

func (h *Handler) formFromRecipe(rec *recipes.Recipe) recipeForm {
	f := recipeForm{
		ID: rec.ID, Slug: rec.Slug, Name: rec.Name, Description: rec.Description, Cuisine: rec.Cuisine,
		MealTimes: rec.MealTimes, Difficulty: rec.Difficulty,
		Prep: strconv.Itoa(rec.PrepMinutes), Cook: strconv.Itoa(rec.CookMinutes), Servings: strconv.Itoa(rec.Servings),
		VideoURL: rec.VideoURL, ImageURL: rec.ImageURL, Steps: strings.Join(rec.Steps, "\n"),
		Source: rec.Source, Status: rec.Status, Deleted: rec.Deleted(),
	}
	n := rec.Nutrition
	f.Nutrition = map[string]string{}
	for k, v := range map[string]float64{"calories": n.Calories, "protein": n.Protein, "fat": n.Fat, "carbs": n.Carbs,
		"fiber": n.Fiber, "sugar": n.Sugar, "sodium": n.Sodium, "saturated_fat": n.SaturatedFat} {
		f.Nutrition[k] = strconv.FormatFloat(v, 'f', -1, 64)
	}
	lines := make([]string, len(rec.Ingredients))
	for i, in := range rec.Ingredients {
		lines[i] = IngredientLine(in)
	}
	f.Ingredients = strings.Join(lines, "\n")
	locales := map[string]bool{}
	for _, l := range h.ContentLocales {
		locales[l] = true
	}
	for l := range rec.Translations {
		if l != recipes.BaseLocale {
			locales[l] = true
		}
	}
	keys := make([]string, 0, len(locales))
	for l := range locales {
		keys = append(keys, l)
	}
	sort.Strings(keys)
	for _, l := range keys {
		t := rec.Translations[l]
		f.Translations = append(f.Translations, translationForm{Locale: l, Name: t.Name, Description: t.Description,
			Steps: strings.Join(t.Steps, "\n"), Ingredients: strings.Join(t.Ingredients, "\n")})
	}
	return f
}

// IngredientLine renders an ingredient back to its editable text form.
// Non-essential ingredients carry an "(optional)" marker.
func IngredientLine(in recipes.Ingredient) string {
	line := in.Raw
	if line == "" {
		parts := []string{}
		if in.Quantity != nil {
			parts = append(parts, strconv.FormatFloat(*in.Quantity, 'f', -1, 64))
		}
		if in.Unit != "" && in.Unit != "to taste" {
			parts = append(parts, in.Unit)
		}
		parts = append(parts, in.Name)
		line = strings.Join(parts, " ")
		if in.Note != "" {
			line += ", " + in.Note
		}
	}
	if !in.Essential && !strings.Contains(strings.ToLower(line), "optional") {
		line += " (optional)"
	}
	return line
}

// ParseIngredientLines turns textarea lines into ingredients. A line
// containing "(optional)" is marked non-essential.
func ParseIngredientLines(text string) []recipes.Ingredient {
	var out []recipes.Ingredient
	for _, line := range splitLines(text) {
		essential := true
		clean := line
		if i := strings.Index(strings.ToLower(clean), "(optional)"); i >= 0 {
			essential = false
			clean = strings.TrimSpace(clean[:i] + clean[i+len("(optional)"):])
		}
		p := recipes.ParseIngredient(clean)
		out = append(out, recipes.Ingredient{Position: len(out), Raw: clean, Quantity: p.Quantity, Unit: p.Unit,
			Name: p.Name, Note: p.Note, Essential: essential, NeedsReview: p.NeedsReview})
	}
	return out
}

func splitLines(s string) []string {
	var out []string
	for _, l := range strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

// parseForm reads the posted form into both the redisplay form and a recipe.
func (h *Handler) parseForm(r *http.Request) (recipeForm, *recipes.Recipe, map[string]string) {
	errs := map[string]string{}
	if err := r.ParseForm(); err != nil {
		errs["form"] = "validation.bad_form"
		return recipeForm{Nutrition: map[string]string{}}, nil, errs
	}
	v := r.PostForm
	f := recipeForm{
		Name: v.Get("name"), Description: v.Get("description"), Cuisine: v.Get("cuisine"),
		MealTimes: v["meal_times"], Difficulty: v.Get("difficulty"),
		Prep: v.Get("prep_minutes"), Cook: v.Get("cook_minutes"), Servings: v.Get("servings"),
		VideoURL: v.Get("video_url"), ImageURL: v.Get("image_url"),
		Ingredients: v.Get("ingredients"), Steps: v.Get("steps"), Nutrition: map[string]string{},
	}
	rec := &recipes.Recipe{
		Name: f.Name, Description: f.Description, Cuisine: f.Cuisine, MealTimes: f.MealTimes, Difficulty: f.Difficulty,
		VideoURL: f.VideoURL, ImageURL: f.ImageURL, Steps: splitLines(f.Steps),
		Ingredients: ParseIngredientLines(f.Ingredients),
	}
	atoi := func(field, s string, dst *int) {
		s = strings.TrimSpace(s)
		if s == "" {
			*dst = 0
			return
		}
		n, err := strconv.Atoi(s)
		if err != nil {
			errs[field] = "validation.number"
			return
		}
		*dst = n
	}
	atoi("prep_minutes", f.Prep, &rec.PrepMinutes)
	atoi("cook_minutes", f.Cook, &rec.CookMinutes)
	atoi("servings", f.Servings, &rec.Servings)
	nut := map[string]*float64{"calories": &rec.Nutrition.Calories, "protein": &rec.Nutrition.Protein,
		"fat": &rec.Nutrition.Fat, "carbs": &rec.Nutrition.Carbs, "fiber": &rec.Nutrition.Fiber,
		"sugar": &rec.Nutrition.Sugar, "sodium": &rec.Nutrition.Sodium, "saturated_fat": &rec.Nutrition.SaturatedFat}
	for _, field := range NutritionFields {
		s := strings.TrimSpace(v.Get(field))
		f.Nutrition[field] = s
		if s == "" {
			continue
		}
		x, err := strconv.ParseFloat(strings.ReplaceAll(s, ",", "."), 64)
		if err != nil {
			errs[field] = "validation.number"
			continue
		}
		*nut[field] = x
	}
	rec.Translations = map[string]recipes.Translation{}
	for _, loc := range v["translation_locales"] {
		tf := translationForm{Locale: loc, Name: v.Get("t_" + loc + "_name"), Description: v.Get("t_" + loc + "_description"),
			Steps: v.Get("t_" + loc + "_steps"), Ingredients: v.Get("t_" + loc + "_ingredients")}
		f.Translations = append(f.Translations, tf)
		rec.Translations[loc] = recipes.Translation{Name: strings.TrimSpace(tf.Name), Description: strings.TrimSpace(tf.Description),
			Steps: splitLines(tf.Steps), Ingredients: splitLines(tf.Ingredients)}
	}
	if err := rec.Validate(); err != nil {
		var ve recipes.ValidationError
		if errors.As(err, &ve) {
			for k, m := range ve {
				if _, seen := errs[k]; !seen {
					errs[k] = m
				}
			}
		}
	}
	return f, rec, errs
}

// keepIngredientLinks preserves explicit ingredient links (e.g. from seed
// data) for lines whose text did not change.
func keepIngredientLinks(rec, existing *recipes.Recipe) {
	links := map[string]int64{}
	for _, in := range existing.Ingredients {
		if in.IngredientID != 0 {
			links[in.Raw] = in.IngredientID
		}
	}
	for i := range rec.Ingredients {
		if id, ok := links[rec.Ingredients[i].Raw]; ok {
			rec.Ingredients[i].IngredientID = id
		}
	}
}

// mergeTranslations keeps translations for locales the form didn't include.
func mergeTranslations(rec, existing *recipes.Recipe) {
	for loc, t := range existing.Translations {
		if _, ok := rec.Translations[loc]; !ok {
			rec.Translations[loc] = t
		}
	}
}
