// Package pool assembles the recipes a particular viewer should see: it
// removes allergens, computes feasibility against the viewer's country and
// pantry, and applies browse filters.
package pool

import (
	"context"
	"strings"

	"github.com/abdulovia/cookcompass/internal/availability"
	"github.com/abdulovia/cookcompass/internal/catalog"
	"github.com/abdulovia/cookcompass/internal/ingredients"
	"github.com/abdulovia/cookcompass/internal/recipes"
)

// Viewer describes who is looking.
type Viewer struct {
	Locale          string
	Country         string // "" disables country availability
	ShowUnavailable bool
	ShopID          int64 // selected shop; 0 = any
	StrictShop      bool  // availability only from ShopID + generic products
	Allergies       []int64
	Dislikes        []int64
	Pantry          map[int64]bool
}

// Query holds browse filters.
type Query struct {
	Cuisine      string
	MealTime     string
	Difficulty   string
	Text         string
	OnlyCookable bool // everything essential is in the pantry (or substitutable)
	HideDisliked bool
}

// Item is a recipe with viewer-specific facts.
type Item struct {
	Recipe   *recipes.Recipe // localized for display
	Base     *recipes.Recipe // as stored
	Market   availability.Result
	Home     availability.Result
	Disliked bool
}

// Pool loads and filters recipes.
type Pool struct {
	Recipes   *recipes.Store
	Ings      *ingredients.Store
	Catalog   *catalog.Store
	Threshold float64
}

// context data shared by all items of one request.
type env struct {
	staples map[int64]bool
	subs    map[int64][]ingredients.Substitute
	market  map[int64]bool
}

func (p *Pool) env(ctx context.Context, v Viewer) (*env, error) {
	list, err := p.Ings.List(ctx)
	if err != nil {
		return nil, err
	}
	e := &env{staples: map[int64]bool{}}
	for _, in := range list {
		if in.Staple {
			e.staples[in.ID] = true
		}
	}
	if e.subs, err = p.Ings.Substitutes(ctx); err != nil {
		return nil, err
	}
	if v.Country != "" {
		if e.market, err = p.Catalog.IngredientSet(ctx, v.Country, v.ShopID, v.StrictShop); err != nil {
			return nil, err
		}
	}
	return e, nil
}

func (p *Pool) item(e *env, v Viewer, r *recipes.Recipe) Item {
	it := Item{Recipe: r.Localized(v.Locale), Base: r}
	if v.Country != "" {
		it.Market = availability.Checker{Have: e.market, Staples: e.staples, Subs: e.subs, Threshold: p.Threshold}.Check(r)
	} else {
		it.Market = availability.Result{Available: true, Ratio: 1}
	}
	it.Home = availability.Checker{Have: v.Pantry, Staples: e.staples, Subs: e.subs, Threshold: 1}.Check(r)
	dis := map[int64]bool{}
	for _, id := range v.Dislikes {
		dis[id] = true
	}
	for _, in := range r.Ingredients {
		if dis[in.IngredientID] {
			it.Disliked = true
			break
		}
	}
	return it
}

// Items returns every published recipe the viewer may see (allergens
// removed), with availability computed but no other filtering.
func (p *Pool) Items(ctx context.Context, v Viewer) ([]Item, error) {
	list, err := p.Recipes.List(ctx, recipes.Filter{ExcludeIngredientIDs: v.Allergies})
	if err != nil {
		return nil, err
	}
	e, err := p.env(ctx, v)
	if err != nil {
		return nil, err
	}
	out := make([]Item, len(list))
	for i, r := range list {
		out[i] = p.item(e, v, r)
	}
	return out, nil
}

// Browse returns the filtered list for the browse page.
func (p *Pool) Browse(ctx context.Context, v Viewer, q Query) ([]Item, error) {
	items, err := p.Items(ctx, v)
	if err != nil {
		return nil, err
	}
	return Filter(items, v, q), nil
}

// Visible is the pool used for suggestions: available where the viewer
// lives (unless they opted to see everything) and free of dislikes.
func (p *Pool) Visible(ctx context.Context, v Viewer) ([]Item, error) {
	return p.Browse(ctx, v, Query{HideDisliked: true})
}

// One returns a single published recipe by slug with availability. It is
// returned even when unavailable so the detail page can explain why.
func (p *Pool) One(ctx context.Context, v Viewer, slug string) (*Item, error) {
	r, err := p.Recipes.GetBySlug(ctx, slug)
	if err != nil {
		return nil, err
	}
	if r.Deleted() || r.Status != recipes.StatusApproved {
		return nil, recipes.ErrNotFound
	}
	e, err := p.env(ctx, v)
	if err != nil {
		return nil, err
	}
	it := p.item(e, v, r)
	return &it, nil
}

// Filter applies browse filters to items. It is pure so it can be tested
// without a database.
func Filter(items []Item, v Viewer, q Query) []Item {
	text := ingredients.Normalize(q.Text)
	out := make([]Item, 0, len(items))
	for _, it := range items {
		r := it.Base
		switch {
		case q.Cuisine != "" && r.Cuisine != q.Cuisine,
			q.MealTime != "" && !r.HasMealTime(q.MealTime),
			q.Difficulty != "" && r.Difficulty != q.Difficulty,
			q.OnlyCookable && !it.Home.Available,
			q.HideDisliked && it.Disliked,
			v.Country != "" && !v.ShowUnavailable && !it.Market.Available:
			continue
		}
		if text != "" && !matchesText(it, text) {
			continue
		}
		out = append(out, it)
	}
	return out
}

func matchesText(it Item, text string) bool {
	for _, h := range []string{it.Recipe.Name, it.Base.Name, it.Base.Cuisine} {
		if strings.Contains(ingredients.Normalize(h), text) {
			return true
		}
	}
	return false
}
