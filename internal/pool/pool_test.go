package pool

import (
	"context"
	"strings"
	"testing"

	"github.com/abdulovia/cookcompass/internal/availability"
	"github.com/abdulovia/cookcompass/internal/catalog"
	"github.com/abdulovia/cookcompass/internal/ingredients"
	"github.com/abdulovia/cookcompass/internal/recipes"
	"github.com/abdulovia/cookcompass/internal/seed"
	"github.com/abdulovia/cookcompass/internal/testutil"
	seedfs "github.com/abdulovia/cookcompass/seed"
)

func it(name, cuisine, diff string, meals []string, home, market, disliked bool) Item {
	r := &recipes.Recipe{Name: name, Cuisine: cuisine, Difficulty: diff, MealTimes: meals,
		Translations: map[string]recipes.Translation{"ru": {Name: "Рецепт " + name}}}
	return Item{Recipe: r.Localized("ru"), Base: r, Home: availability.Result{Available: home},
		Market: availability.Result{Available: market}, Disliked: disliked}
}

func names(items []Item) string {
	var n []string
	for _, i := range items {
		n = append(n, i.Base.Name)
	}
	return strings.Join(n, ",")
}

func TestFilter(t *testing.T) {
	items := []Item{
		it("Pancakes", "american", "easy", []string{"breakfast"}, true, true, false),
		it("Pad Thai", "thai", "medium", []string{"lunch", "dinner"}, false, false, false),
		it("Gyoza", "japanese", "hard", []string{"dinner", "snack"}, false, true, true),
		it("Syrniki", "russian", "easy", []string{"breakfast", "snack"}, true, true, true),
	}
	all := Viewer{}
	local := Viewer{Country: "RU"}
	tests := []struct {
		name string
		v    Viewer
		q    Query
		want string
	}{
		{"no filters", all, Query{}, "Pancakes,Pad Thai,Gyoza,Syrniki"},
		{"cuisine", all, Query{Cuisine: "thai"}, "Pad Thai"},
		{"meal", all, Query{MealTime: "snack"}, "Gyoza,Syrniki"},
		{"difficulty", all, Query{Difficulty: "easy"}, "Pancakes,Syrniki"},
		{"only cookable", all, Query{OnlyCookable: true}, "Pancakes,Syrniki"},
		{"hide disliked", all, Query{HideDisliked: true}, "Pancakes,Pad Thai"},
		{"text in base name", all, Query{Text: "gyo"}, "Gyoza"},
		{"text in translation", all, Query{Text: "рецепт pad"}, "Pad Thai"},
		{"text by cuisine", all, Query{Text: "RUSS"}, "Syrniki"},
		{"country hides unavailable", local, Query{}, "Pancakes,Gyoza,Syrniki"},
		{"show unavailable", Viewer{Country: "RU", ShowUnavailable: true}, Query{}, "Pancakes,Pad Thai,Gyoza,Syrniki"},
		{"combined", local, Query{MealTime: "breakfast", HideDisliked: true, Difficulty: "easy"}, "Pancakes"},
		{"nothing matches", all, Query{Cuisine: "french"}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := names(Filter(items, tc.v, tc.q)); got != tc.want {
				t.Errorf("got %q want %q", got, tc.want)
			}
		})
	}
}

func newPool(t *testing.T) *Pool {
	t.Helper()
	d := testutil.DB(t)
	ings := ingredients.NewStore(d)
	rs := recipes.NewStore(d, ings, 20)
	cat := catalog.NewStore(d, ings)
	if _, err := (&seed.Seeder{FS: seedfs.FS, Ings: ings, Recipes: rs, Catalog: cat}).Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	return &Pool{Recipes: rs, Ings: ings, Catalog: cat, Threshold: 1}
}

func TestPoolWithSeedData(t *testing.T) {
	ctx := context.Background()
	p := newPool(t)
	peanut, _ := p.Ings.IDBySlug(ctx, "peanuts")
	tvorog, _ := p.Ings.IDBySlug(ctx, "tvorog")
	egg, _ := p.Ings.IDBySlug(ctx, "egg")

	all, err := p.Items(ctx, Viewer{Locale: "en"})
	if err != nil || len(all) != 28 {
		t.Fatalf("items %d %v", len(all), err)
	}
	noPeanuts, _ := p.Items(ctx, Viewer{Allergies: []int64{peanut}})
	if len(noPeanuts) >= len(all) {
		t.Fatal("allergen not excluded")
	}
	for _, it := range noPeanuts {
		if it.Base.Slug == "kung-pao-chicken" {
			t.Fatal("kung pao contains peanuts")
		}
	}

	// Country availability: RU lacks gyoza wrappers, so gyoza is hidden there.
	ru, err := p.Browse(ctx, Viewer{Country: "RU", Locale: "ru"}, Query{})
	if err != nil {
		t.Fatal(err)
	}
	ruNames := map[string]bool{}
	for _, it := range ru {
		ruNames[it.Base.Slug] = true
	}
	if ruNames["pork-gyoza"] || ruNames["thai-green-curry"] || ruNames["bibimbap"] {
		t.Fatalf("unavailable recipes visible in RU: %v", ruNames)
	}
	if !ruNames["syrniki"] || !ruNames["pad-thai"] {
		t.Fatal("syrniki and pad thai (fish sauce → soy sauce) should be available in RU")
	}
	pad, err := p.One(ctx, Viewer{Country: "RU"}, "pad-thai")
	if err != nil || len(pad.Market.Substitutions) == 0 {
		t.Fatalf("pad thai in RU should use a substitution: %+v %v", pad, err)
	}
	if !strings.HasPrefix(pad.Recipe.Name, "Pad") {
		t.Fatalf("default locale name %q", pad.Recipe.Name)
	}

	// Pantry: carbonara needs spaghetti, pancetta (or bacon), eggs and parmesan (or pecorino).
	id := func(slug string) int64 { v, _ := p.Ings.IDBySlug(ctx, slug); return v }
	cookable := func(pantry ...string) bool {
		m := map[int64]bool{}
		for _, s := range pantry {
			m[id(s)] = true
		}
		list, _ := p.Browse(ctx, Viewer{Pantry: m}, Query{OnlyCookable: true})
		for _, it := range list {
			if it.Base.Slug == "spaghetti-carbonara" {
				return true
			}
		}
		return false
	}
	if cookable("egg", "spaghetti", "parmesan") {
		t.Fatal("carbonara needs pork")
	}
	if !cookable("egg", "spaghetti", "parmesan", "bacon") {
		t.Fatal("bacon should substitute for pancetta")
	}
	_ = egg

	vis, _ := p.Visible(ctx, Viewer{Dislikes: []int64{tvorog}})
	for _, it := range vis {
		if it.Base.Slug == "syrniki" {
			t.Fatal("disliked recipe in visible pool")
		}
	}

	if _, err := p.One(ctx, Viewer{}, "nope"); err == nil {
		t.Fatal("missing slug")
	}
	r, _ := p.Recipes.GetBySlug(ctx, "syrniki")
	if err := p.Recipes.SoftDelete(ctx, r.ID, recipes.Change{}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.One(ctx, Viewer{}, "syrniki"); err == nil {
		t.Fatal("deleted recipe returned")
	}
}
