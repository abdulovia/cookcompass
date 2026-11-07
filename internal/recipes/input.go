package recipes

import (
	"context"
	"fmt"
	"strings"
)

// Input is the JSON document format used by seed files and
// `cookcompass recipe create --file`. Base text comes from the "en" entry
// of I18n unless Name/Steps are given directly.
type Input struct {
	Slug        string               `json:"slug,omitempty"`
	Name        string               `json:"name,omitempty"`
	Description string               `json:"description,omitempty"`
	Cuisine     string               `json:"cuisine"`
	MealTimes   []string             `json:"meal_times"`
	Difficulty  string               `json:"difficulty"`
	PrepMinutes int                  `json:"prep_minutes"`
	CookMinutes int                  `json:"cook_minutes"`
	Servings    int                  `json:"servings"`
	VideoURL    string               `json:"video_url,omitempty"`
	ImageURL    string               `json:"image_url,omitempty"`
	Nutrition   Nutrition            `json:"nutrition"`
	Steps       []string             `json:"steps,omitempty"`
	Ingredients []InputIngredient    `json:"ingredients"`
	I18n        map[string]InputText `json:"i18n,omitempty"`
}

// InputIngredient is one ingredient line, optionally pinned to a canonical
// ingredient slug.
type InputIngredient struct {
	Text       string `json:"text"`
	Ingredient string `json:"ingredient,omitempty"`
	Essential  *bool  `json:"essential,omitempty"`
}

// InputText is per-locale text.
type InputText struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Steps       []string `json:"steps,omitempty"`
	Ingredients []string `json:"ingredients,omitempty"`
}

// BaseLocale is the language of the recipes table's own text columns.
const BaseLocale = "en"

// FromInput converts an Input to a Recipe, parsing ingredient lines and
// resolving ingredient slugs.
func (s *Store) FromInput(ctx context.Context, in Input) (*Recipe, error) {
	base := in.I18n[BaseLocale]
	r := &Recipe{
		Slug:        in.Slug,
		Name:        firstNonEmpty(in.Name, base.Name),
		Description: firstNonEmpty(in.Description, base.Description),
		Cuisine:     in.Cuisine,
		MealTimes:   in.MealTimes,
		Difficulty:  in.Difficulty,
		PrepMinutes: in.PrepMinutes,
		CookMinutes: in.CookMinutes,
		Servings:    in.Servings,
		VideoURL:    in.VideoURL,
		ImageURL:    in.ImageURL,
		Nutrition:   in.Nutrition,
		Steps:       in.Steps,
	}
	if len(r.Steps) == 0 {
		r.Steps = base.Steps
	}
	for i, ii := range in.Ingredients {
		p := ParseIngredient(ii.Text)
		ing := Ingredient{Position: i, Raw: strings.TrimSpace(ii.Text), Quantity: p.Quantity, Unit: p.Unit,
			Name: p.Name, Note: p.Note, Essential: true, NeedsReview: p.NeedsReview}
		if ii.Essential != nil {
			ing.Essential = *ii.Essential
		}
		if ii.Ingredient != "" {
			if s.ings == nil {
				return nil, fmt.Errorf("ingredient %q: no ingredient store", ii.Ingredient)
			}
			id, err := s.ings.IDBySlug(ctx, ii.Ingredient)
			if err != nil {
				return nil, fmt.Errorf("recipe %q: %w", r.Name, err)
			}
			ing.IngredientID = id
		}
		r.Ingredients = append(r.Ingredients, ing)
	}
	if len(in.I18n) > 0 {
		r.Translations = map[string]Translation{}
		for loc, t := range in.I18n {
			r.Translations[loc] = Translation{Name: t.Name, Description: t.Description, Steps: t.Steps, Ingredients: t.Ingredients}
		}
	}
	return r, nil
}
