package app

import (
	"context"
	"fmt"

	"github.com/abdulovia/cookcompass/internal/recipes"
	"github.com/abdulovia/cookcompass/internal/translate"
)

// Translator returns the configured machine-translation client, or nil.
func (a *App) Translator() *translate.Client { return translate.New(a.Cfg.Translate.URL) }

// TranslateRecipeDraft machine-translates a recipe's base content into
// locale and stores it as that locale's translation, flagging the recipe for
// review. The draft is a starting point for a human, never a final state.
func (a *App) TranslateRecipeDraft(ctx context.Context, rec *recipes.Recipe, locale string) error {
	if locale == "" || locale == recipes.BaseLocale {
		return fmt.Errorf("bad locale %q", locale)
	}
	cl := a.Translator()
	if cl == nil {
		return fmt.Errorf("translator is not configured (set COOK_TRANSLATE_URL)")
	}
	// One call per field group keeps the payload small and ordered.
	name, err := cl.Translate(ctx, recipes.BaseLocale, locale, rec.Name)
	if err != nil {
		return err
	}
	desc, err := cl.Translate(ctx, recipes.BaseLocale, locale, rec.Description)
	if err != nil {
		return err
	}
	steps, err := cl.Translate(ctx, recipes.BaseLocale, locale, rec.Steps...)
	if err != nil {
		return err
	}
	lines := make([]string, len(rec.Ingredients))
	for i, in := range rec.Ingredients {
		lines[i] = in.Raw
	}
	lines, err = cl.Translate(ctx, recipes.BaseLocale, locale, lines...)
	if err != nil {
		return err
	}
	if rec.Translations == nil {
		rec.Translations = map[string]recipes.Translation{}
	}
	rec.Translations[locale] = recipes.Translation{Name: name[0], Description: desc[0], Steps: steps, Ingredients: lines}
	// The revision note (not the recipe flag: needs_review tracks parsed
	// ingredients) records that this locale is an unreviewed machine draft.
	return a.Recipes.Update(ctx, rec, recipes.Change{Source: recipes.SourceUser, Note: "machine translation draft (" + locale + ")"})
}
