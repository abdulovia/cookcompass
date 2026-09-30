package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"strings"

	"github.com/abdulovia/cookcompass/internal/fetch"
	"github.com/abdulovia/cookcompass/internal/recipes"
)

// MealDBSource imports recipes from TheMealDB (https://www.themealdb.com),
// a free recipe API with photos and YouTube videos. It walks the
// "search by first letter" endpoint through the polite fetch client, so a
// full pass is 26 cached requests at the per-site rate limit.
//
// The public test key "1" is meant for development and personal/educational
// use; supporters get a production key (COOK_MEALDB_KEY). Everything lands in
// the review queue like any scraped recipe, without nutrition data.
type MealDBSource struct {
	Fetcher *fetch.Client
	BaseURL string // default https://www.themealdb.com/api/json/v1
	Key     string // default "1"
	Letters string // default a–z
	Log     *slog.Logger
}

// Name implements recipes.RecipeSource.
func (s *MealDBSource) Name() string { return "themealdb" }

// LongRunning tells callers (the admin queue) to run this source in the
// background: a full pass takes ~30 s at the politeness delay.
func (s *MealDBSource) LongRunning() bool { return true }

const mealDBDefaultBase = "https://www.themealdb.com/api/json/v1"

// Fetch implements recipes.RecipeSource. opts.URLs, when set, are fetched
// as-is (search.php / lookup.php URLs); opts.Limit caps the result.
func (s *MealDBSource) Fetch(ctx context.Context, opts recipes.FetchOpts) ([]recipes.RawRecipe, error) {
	targets := opts.URLs
	if len(targets) == 0 {
		base := strings.TrimRight(firstNonEmpty(s.BaseURL, mealDBDefaultBase), "/")
		key := url.PathEscape(firstNonEmpty(s.Key, "1"))
		for _, l := range firstNonEmpty(s.Letters, "abcdefghijklmnopqrstuvwxyz") {
			targets = append(targets, fmt.Sprintf("%s/%s/search.php?f=%c", base, key, l))
		}
	}
	var out []recipes.RawRecipe
	var errs []error
	for _, u := range targets {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		body, err := s.Fetcher.Get(ctx, u)
		if err != nil {
			errs = append(errs, err)
			if s.Log != nil {
				s.Log.Warn("themealdb", "url", u, "err", err)
			}
			continue
		}
		meals, err := ParseMealDB(body)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", u, err))
			continue
		}
		for i := range meals {
			meals[i].URL = u
		}
		out = append(out, meals...)
		if opts.Limit > 0 && len(out) >= opts.Limit {
			return out[:opts.Limit], nil
		}
	}
	if len(out) == 0 && len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return out, nil
}

// ParseMealDB maps a TheMealDB response ({"meals": [...] | null}) to raw
// recipes. Ingredient lines are "measure ingredient"; instructions are split
// into steps; area and category feed the cuisine and meal-time guesses.
func ParseMealDB(body []byte) ([]recipes.RawRecipe, error) {
	var resp struct {
		Meals []map[string]any `json:"meals"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("themealdb: %w", err)
	}
	out := make([]recipes.RawRecipe, 0, len(resp.Meals))
	for _, m := range resp.Meals {
		get := func(k string) string { s, _ := m[k].(string); return strings.TrimSpace(s) }
		raw := recipes.RawRecipe{
			Source:   "themealdb",
			Name:     get("strMeal"),
			VideoURL: get("strYoutube"),
			ImageURL: get("strMealThumb"),
			Steps:    splitMealDBSteps(get("strInstructions")),
			Language: "en",
		}
		if id := get("idMeal"); id != "" {
			raw.CanonicalURL = "https://www.themealdb.com/meal/" + url.PathEscape(id)
		}
		if a := get("strArea"); a != "" {
			raw.Cuisines = []string{a}
		}
		if c := get("strCategory"); c != "" {
			raw.Categories = []string{mealDBCategory(c)}
		}
		for _, tag := range strings.Split(get("strTags"), ",") {
			if tag = strings.TrimSpace(tag); tag != "" {
				raw.Keywords = append(raw.Keywords, tag)
			}
		}
		for i := 1; i <= 20; i++ {
			ing := get(fmt.Sprintf("strIngredient%d", i))
			if ing == "" {
				continue
			}
			line := strings.Join(strings.Fields(get(fmt.Sprintf("strMeasure%d", i))+" "+strings.ToLower(ing)), " ")
			raw.Ingredients = append(raw.Ingredients, line)
		}
		if raw.Name != "" {
			out = append(out, raw)
		}
	}
	return out, nil
}

// mealDBCategory turns TheMealDB categories into words the meal-time
// heuristic understands.
func mealDBCategory(c string) string {
	switch strings.ToLower(c) {
	case "breakfast":
		return "breakfast"
	case "starter", "side", "dessert":
		return "snack"
	default: // Beef, Chicken, Pasta, Seafood, Vegetarian, …
		return "main course " + strings.ToLower(c)
	}
}

var (
	reStepLabel = regexp.MustCompile(`(?i)^\s*(step\s*\d+[:.)]?|\d+[.)])\s*`)
	reSentence  = regexp.MustCompile(`[^.!?]+[.!?]+(\s+|$)`)
)

// splitMealDBSteps breaks TheMealDB's free text into steps: one per line,
// dropping "STEP 3" labels; a single long paragraph is cut into chunks of
// about two sentences so the step checklist stays usable.
func splitMealDBSteps(text string) []string {
	var steps []string
	for _, line := range strings.FieldsFunc(text, func(r rune) bool { return r == '\n' || r == '\r' }) {
		line = strings.TrimSpace(reStepLabel.ReplaceAllString(line, ""))
		if len([]rune(line)) >= 3 {
			steps = append(steps, line)
		}
	}
	if len(steps) == 1 && len(steps[0]) > 400 {
		sentences := reSentence.FindAllString(steps[0], -1)
		if len(sentences) > 2 {
			steps = steps[:0]
			for i := 0; i < len(sentences); i += 2 {
				end := min(i+2, len(sentences))
				steps = append(steps, strings.TrimSpace(strings.Join(sentences[i:end], "")))
			}
		}
	}
	return steps
}
