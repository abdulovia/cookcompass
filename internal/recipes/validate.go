package recipes

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
)

// ValidationError maps field names to i18n message ids.
type ValidationError map[string]string

func (v ValidationError) Error() string {
	keys := make([]string, 0, len(v))
	for k := range v {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + ": " + v[k]
	}
	return "invalid recipe: " + strings.Join(parts, "; ")
}

// Validate checks a recipe before it is persisted and normalizes harmless
// formatting (trimmed strings, deduplicated meal times, empty steps dropped).
func (r *Recipe) Validate() error {
	errs := ValidationError{}
	r.Name = strings.TrimSpace(r.Name)
	r.Description = strings.TrimSpace(r.Description)
	r.VideoURL = strings.TrimSpace(r.VideoURL)
	r.ImageURL = strings.TrimSpace(r.ImageURL)

	switch {
	case r.Name == "":
		errs["name"] = "validation.required"
	case len([]rune(r.Name)) > 200:
		errs["name"] = "validation.too_long"
	}
	if !oneOf(r.Difficulty, Difficulties) {
		errs["difficulty"] = "validation.invalid_choice"
	}
	seen := map[string]bool{}
	var meals []string
	for _, m := range r.MealTimes {
		m = strings.TrimSpace(strings.ToLower(m))
		if !oneOf(m, MealTimes) {
			errs["meal_times"] = "validation.invalid_choice"
			continue
		}
		if !seen[m] {
			seen[m] = true
			meals = append(meals, m)
		}
	}
	sort.SliceStable(meals, func(i, j int) bool { return indexOf(meals[i], MealTimes) < indexOf(meals[j], MealTimes) })
	r.MealTimes = meals
	if len(meals) == 0 && errs["meal_times"] == "" {
		errs["meal_times"] = "validation.required"
	}
	if r.Servings < 1 || r.Servings > 100 {
		errs["servings"] = "validation.out_of_range"
	}
	if r.PrepMinutes < 0 || r.PrepMinutes > 24*60 {
		errs["prep_minutes"] = "validation.out_of_range"
	}
	if r.CookMinutes < 0 || r.CookMinutes > 3*24*60 {
		errs["cook_minutes"] = "validation.out_of_range"
	}
	n := r.Nutrition
	for field, v := range map[string]float64{"calories": n.Calories, "protein": n.Protein, "fat": n.Fat, "carbs": n.Carbs,
		"fiber": n.Fiber, "sugar": n.Sugar, "sodium": n.Sodium, "saturated_fat": n.SaturatedFat} {
		if v < 0 || v > 100000 {
			errs[field] = "validation.out_of_range"
		}
	}
	if r.VideoURL != "" && !IsYouTubeURL(r.VideoURL) {
		errs["video_url"] = "validation.youtube_only"
	}
	if r.ImageURL != "" && !isHTTPURL(r.ImageURL) {
		errs["image_url"] = "validation.url"
	}
	var steps []string
	for _, s := range r.Steps {
		if s = strings.TrimSpace(s); s != "" {
			steps = append(steps, s)
		}
	}
	r.Steps = steps
	if len(steps) == 0 {
		errs["steps"] = "validation.required"
	}
	var ings []Ingredient
	for _, in := range r.Ingredients {
		in.Name = strings.TrimSpace(in.Name)
		if in.Name == "" {
			continue
		}
		in.Position = len(ings)
		ings = append(ings, in)
	}
	r.Ingredients = ings
	if len(ings) == 0 {
		errs["ingredients"] = "validation.required"
	}
	for loc, t := range r.Translations {
		if len(loc) < 2 || len(loc) > 5 {
			errs["translations"] = "validation.invalid_choice"
		}
		t.Name = strings.TrimSpace(t.Name)
		if t.Name == "" && len(t.Steps) == 0 && t.Description == "" && len(t.Ingredients) == 0 {
			delete(r.Translations, loc)
			continue
		}
		r.Translations[loc] = t
	}
	if len(errs) > 0 {
		return errs
	}
	return nil
}

func isHTTPURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

func oneOf(v string, set []string) bool { return indexOf(v, set) >= 0 }

func indexOf(v string, set []string) int {
	for i, s := range set {
		if s == v {
			return i
		}
	}
	return -1
}

// ParseMealTimes splits a comma list.
func ParseMealTimes(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// FormatQuantity renders a quantity with common fractions (1.5 → "1 ½").
func FormatQuantity(q float64) string {
	if q <= 0 {
		return ""
	}
	whole := int(q)
	frac := q - float64(whole)
	fracs := []struct {
		v float64
		s string
	}{{0.125, "⅛"}, {0.25, "¼"}, {1.0 / 3, "⅓"}, {0.5, "½"}, {2.0 / 3, "⅔"}, {0.75, "¾"}}
	for _, f := range fracs {
		if abs(frac-f.v) < 0.02 {
			if whole == 0 {
				return f.s
			}
			return fmt.Sprintf("%d %s", whole, f.s)
		}
	}
	if frac < 0.02 {
		return fmt.Sprintf("%d", whole)
	}
	if frac > 0.98 {
		return fmt.Sprintf("%d", whole+1)
	}
	s := fmt.Sprintf("%.2f", q)
	s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	return s
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}
